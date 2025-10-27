package pgcluster

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"time"

	gover "github.com/hashicorp/go-version"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/percona/percona-postgresql-operator/v2/internal/initialize"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/percona/clientcmd"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

var errPatroniVersionCheckWait = errors.New("waiting for pod to initialize")

func (r *PGClusterReconciler) reconcilePatroniVersion(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if cr.Annotations == nil {
		cr.Annotations = make(map[string]string)
	}

	if patroniVersion, ok := cr.Annotations[pNaming.AnnotationCustomPatroniVersion]; ok {
		patroniVersionUpdateFunc := func() error {
			cluster := &v2.PerconaPGCluster{}
			if err := r.Client.Get(ctx, types.NamespacedName{
				Name:      cr.Name,
				Namespace: cr.Namespace,
			}, cluster); err != nil {
				return errors.Wrap(err, "get PerconaPGCluster")
			}

			orig := cluster.DeepCopy()

			cluster.Status.Patroni.Version = patroniVersion
			cluster.Status.PatroniVersion = patroniVersion

			if err := r.Client.Status().Patch(ctx, cluster.DeepCopy(), client.MergeFrom(orig)); err != nil {
				return errors.Wrap(err, "failed to patch patroni version")
			}

			err := r.patchPatroniVersionAnnotation(ctx, cr, patroniVersion)
			if err != nil {
				return errors.Wrap(err, "failed to patch patroni version annotation")
			}

			return nil
		}

		// To ensure that the update was done given that conflicts can be caused by
		// other code making unrelated updates to the same resource at the same time.
		if err := retry.RetryOnConflict(retry.DefaultRetry, patroniVersionUpdateFunc); err != nil {
			return errors.Wrap(err, "failed to patch patroni version")
		}
		return nil
	}

	// Starting from version 2.8.0, the patroni version check pod should not be executed.
	if cr.CompareVersion("2.8.0") >= 0 {

		pods, err := r.getInstancePods(ctx, cr)
		if err != nil {
			return errors.Wrap(err, "failed to get instance pods")
		}
		if len(pods.Items) == 0 {
			return errors.Wrap(err, "instance pods not available")
		}

		p := pods.Items[0]

		if p.Status.Phase != corev1.PodRunning {
			return errPatroniVersionCheckWait
		}

		patroniVersion, err := r.getPatroniVersion(ctx, &p, naming.ContainerDatabase)
		if err != nil {
			return errors.Wrap(err, "failed to get patroni version")
		}

		orig := cr.DeepCopy()

		cr.Status.Patroni.Version = patroniVersion
		cr.Status.PatroniVersion = patroniVersion
		cr.Status.Postgres.Version = cr.Spec.PostgresVersion
		cr.Status.Postgres.ImageID = getImageIDFromPod(&p, naming.ContainerDatabase)

		if err := r.Client.Status().Patch(ctx, cr.DeepCopy(), client.MergeFrom(orig)); err != nil {
			return errors.Wrap(err, "failed to patch patroni version")
		}

		err = r.patchPatroniVersionAnnotation(ctx, cr, patroniVersion)
		if err != nil {
			return errors.Wrap(err, "failed to patch patroni version annotation")
		}

		return nil
	}

	imageIDs, err := r.instanceImageIDs(ctx, cr)
	if err != nil {
		return errors.Wrap(err, "get image IDs")
	}

	// If the imageIDs slice contains the imageID from the status, we skip checking the Patroni version.
	// This ensures that the Patroni version is only checked after all pods have been updated.
	if cr.CompareVersion("2.8.0") >= 0 {
		if (len(imageIDs) == 0 || slices.Contains(imageIDs, cr.Status.Postgres.ImageID)) && cr.Status.Patroni.Version != "" {
			err = r.patchPatroniVersionAnnotation(ctx, cr, cr.Status.Patroni.Version)
			if err != nil {
				return errors.Wrap(err, "failed to patch patroni version annotation")
			}
			return nil
		}
	} else {
		if (len(imageIDs) == 0 || slices.Contains(imageIDs, cr.Status.Postgres.ImageID)) && cr.Status.PatroniVersion != "" {
			err = r.patchPatroniVersionAnnotation(ctx, cr, cr.Status.PatroniVersion)
			if err != nil {
				return errors.Wrap(err, "failed to patch patroni version annotation")
			}
			return nil
		}
	}

	meta := metav1.ObjectMeta{
		Name:      cr.Name + "-patroni-version-check",
		Namespace: cr.Namespace,
	}

	p := &corev1.Pod{
		ObjectMeta: meta,
	}

	err = r.Client.Get(ctx, client.ObjectKeyFromObject(p), p)
	if client.IgnoreNotFound(err) != nil {
		return errors.Wrap(err, "failed to get patroni version check pod")
	}
	if k8serrors.IsNotFound(err) {
		if len(cr.Spec.InstanceSets) == 0 {
			return errors.New(".spec.instances is a required value") // shouldn't happen as the value is required in the crd.yaml
		}

		// Using minimal resources since the patroni version check pod is performing a very simple
		// operation i.e. "patronictl version"
		resources := corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
		}

		p = &corev1.Pod{
			ObjectMeta: meta,
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  pNaming.ContainerPatroniVersionCheck,
						Image: cr.PostgresImage(),
						Command: []string{
							"bash",
						},
						Args: []string{
							"-c", "sleep 60",
						},
						Resources:       resources,
						SecurityContext: initialize.RestrictedSecurityContext(cr.CompareVersion("2.8.0") >= 0),
					},
				},
				SecurityContext:               cr.Spec.InstanceSets[0].SecurityContext,
				Affinity:                      cr.Spec.InstanceSets[0].Affinity,
				TerminationGracePeriodSeconds: ptr.To(int64(5)),
				ImagePullSecrets:              cr.Spec.ImagePullSecrets,
				Resources:                     &resources,
			},
		}

		if err := controllerutil.SetControllerReference(cr, p, r.Client.Scheme()); err != nil {
			return errors.Wrap(err, "set controller reference")
		}
		if err := r.Client.Create(ctx, p); client.IgnoreAlreadyExists(err) != nil {
			return errors.Wrap(err, "failed to create pod to check patroni version")
		}

		return errPatroniVersionCheckWait
	}

	if p.Status.Phase != corev1.PodRunning {
		return errPatroniVersionCheckWait
	}

	patroniVersion, err := r.getPatroniVersion(ctx, p, pNaming.ContainerPatroniVersionCheck)
	if err != nil {
		return errors.Wrap(err, "failed to get patroni version")
	}

	orig := cr.DeepCopy()

	cr.Status.Patroni.Version = patroniVersion
	cr.Status.PatroniVersion = patroniVersion
	cr.Status.Postgres.Version = cr.Spec.PostgresVersion
	cr.Status.Postgres.ImageID = getImageIDFromPod(p, pNaming.ContainerPatroniVersionCheck)

	if err := r.Client.Status().Patch(ctx, cr.DeepCopy(), client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "failed to patch patroni version")
	}

	err = r.patchPatroniVersionAnnotation(ctx, cr, patroniVersion)
	if err != nil {
		return errors.Wrap(err, "failed to patch patroni version annotation")
	}

	if err := r.Client.Delete(ctx, p); err != nil {
		return errors.Wrap(err, "failed to delete patroni version check pod")
	}

	return nil
}

func (r *PGClusterReconciler) getPatroniVersion(ctx context.Context, pod *corev1.Pod, containerName string) (string, error) {
	var stdout, stderr bytes.Buffer
	execCli, err := clientcmd.NewClient()
	if err != nil {
		return "", errors.Wrap(err, "failed to create exec client")
	}
	b := wait.Backoff{
		Duration: 5 * time.Second,
		Factor:   1.0,
		Steps:    12,
		Cap:      time.Minute,
	}
	if err := retry.OnError(b, func(err error) bool { return err != nil && strings.Contains(err.Error(), "container not found") }, func() error {
		return execCli.Exec(ctx, pod, containerName, nil, &stdout, &stderr, "patronictl", "version")
	}); err != nil {
		return "", errors.Wrap(err, "exec")
	}

	patroniVersion := strings.TrimSpace(strings.TrimPrefix(stdout.String(), "patronictl version "))

	if _, err := gover.NewVersion(patroniVersion); err != nil {
		return "", errors.Wrap(err, "failed to validate patroni version")
	}

	return patroniVersion, nil
}

func (r *PGClusterReconciler) patchPatroniVersionAnnotation(ctx context.Context, cr *v2.PerconaPGCluster, patroniVersion string) error {
	orig := cr.DeepCopy()
	cr.Annotations[pNaming.AnnotationPatroniVersion] = patroniVersion
	if err := r.Client.Patch(ctx, cr.DeepCopy(), client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "failed to patch the pg cluster")
	}
	return nil
}

func (r *PGClusterReconciler) instanceImageIDs(ctx context.Context, cr *v2.PerconaPGCluster) ([]string, error) {
	pods, err := r.getInstancePods(ctx, cr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get instance pods")
	}

	// Collecting all image IDs from instance pods. Under normal conditions, this slice will contain a single image ID, as all pods typically use the same image.
	// During an image update, it may contain multiple different image IDs as the update progresses.
	var imageIDs []string
	for _, pod := range pods.Items {
		imageID := getImageIDFromPod(&pod, naming.ContainerDatabase)
		if imageID != "" && !slices.Contains(imageIDs, imageID) {
			imageIDs = append(imageIDs, imageID)
		}
	}

	return imageIDs, nil
}

func (r *PGClusterReconciler) getInstancePods(ctx context.Context, cr *v2.PerconaPGCluster) (*corev1.PodList, error) {
	pods := new(corev1.PodList)
	instances, err := naming.AsSelector(naming.ClusterInstances(cr.Name))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create a selector for instance pods")
	}
	if err = r.Client.List(ctx, pods, client.InNamespace(cr.Namespace), client.MatchingLabelsSelector{Selector: instances}); err != nil {
		return nil, errors.Wrap(err, "failed to list instances")
	}
	return pods, nil
}

func getImageIDFromPod(pod *corev1.Pod, containerName string) string {
	idx := slices.IndexFunc(pod.Status.ContainerStatuses, func(s corev1.ContainerStatus) bool {
		return s.Name == containerName
	})
	if idx == -1 {
		return ""
	}
	return pod.Status.ContainerStatuses[idx].ImageID
}
