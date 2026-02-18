package perconaPG

import (
	"context"

	gover "github.com/hashicorp/go-version"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
)

const (
	patroniVersion4 = "4.0.0"
)

// GetPrimaryPod returns the primary pod.
// K8SPG-882
func GetPrimaryPod(ctx context.Context, cli client.Client, cr *v2.PerconaPGCluster) (*corev1.Pod, error) {
	podList := &corev1.PodList{}
	// K8SPG-648: patroni v4.0.0 deprecated "master" role.
	//            We should use "primary" instead
	role := "primary"

	patroniVer, err := gover.NewVersion(determineVersion(cr))
	if err != nil {
		return nil, errors.Wrap(err, "failed to get patroni version")
	}
	patroniVer4 := patroniVer.Compare(gover.Must(gover.NewVersion("4.0.0"))) >= 0
	if !patroniVer4 {
		role = "master"
	}
	err = cli.List(ctx, podList, &client.ListOptions{
		Namespace: cr.Namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"app.kubernetes.io/instance":             cr.Name,
			"postgres-operator.crunchydata.com/role": role,
		}),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list pods")
	}

	if len(podList.Items) == 0 {
		return nil, errors.New("no primary pod found")
	}

	if len(podList.Items) > 1 {
		return nil, errors.New("multiple primary pods found")
	}

	return &podList.Items[0], nil
}

// GetReplicaPods lists the replica pods for a given cluster.
func GetReplicaPods(ctx context.Context, cli client.Client, cr *v2.PerconaPGCluster) ([]corev1.Pod, error) {
	podList := &corev1.PodList{}

	err := cli.List(ctx, podList, &client.ListOptions{
		Namespace: cr.Namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"app.kubernetes.io/instance":             cr.GetName(),
			"postgres-operator.crunchydata.com/role": naming.RolePatroniReplica,
		}),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list pods")
	}

	return podList.Items, nil
}

func determineVersion(cr *v2.PerconaPGCluster) string {
	if cr.CompareVersion("2.7.0") <= 0 {
		return cr.Status.PatroniVersion
	}
	return patroniVersion4
}

// SuspendInstance suspends an instance by setting the AnnotationInstanceSuspended annotation on the StatefulSet.
// Returns true if the instance was suspended.
// Caller is responsible for waiting for the instance to be suspended.
func SuspendInstance(ctx context.Context, cli client.Client, instanceKey client.ObjectKey) (bool, error) {
	sts := &appsv1.StatefulSet{}
	if err := cli.Get(ctx, instanceKey, sts); err != nil {
		return false, errors.Wrap(err, "failed to get stateful set")
	}

	if _, ok := sts.GetAnnotations()[pNaming.AnnotationInstanceSuspended]; ok {
		return sts.Status.Replicas == 0 && sts.Status.ReadyReplicas == 0, nil
	}

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := cli.Get(ctx, instanceKey, sts); err != nil {
			return errors.Wrap(err, "failed to get stateful set")
		}

		orig := sts.DeepCopy()
		annots := sts.GetAnnotations()
		if annots == nil {
			annots = make(map[string]string)
		}
		annots[pNaming.AnnotationInstanceSuspended] = ""
		sts.SetAnnotations(annots)
		return cli.Patch(ctx, sts, client.MergeFrom(orig))
	}); err != nil {
		return false, errors.Wrap(err, "failed to update stateful set annotations")
	}
	return false, nil
}

// UnsuspendInstance unsuspends an instance by removing the AnnotationInstanceSuspended annotation on the StatefulSet.
// Returns true if the instance was unsuspended.
// Caller is responsible for waiting for the instance to be unsuspended.
func UnsuspendInstance(ctx context.Context, cli client.Client, instanceKey client.ObjectKey) (bool, error) {
	sts := &appsv1.StatefulSet{}
	if err := cli.Get(ctx, instanceKey, sts); err != nil {
		return false, errors.Wrap(err, "failed to get stateful set")
	}

	if _, ok := sts.GetAnnotations()[pNaming.AnnotationInstanceSuspended]; !ok {
		return sts.Status.Replicas > 0 && sts.Status.ReadyReplicas > 0, nil
	}

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := cli.Get(ctx, instanceKey, sts); err != nil {
			return errors.Wrap(err, "failed to get stateful set")
		}

		orig := sts.DeepCopy()
		annots := sts.GetAnnotations()
		delete(annots, pNaming.AnnotationInstanceSuspended)
		sts.SetAnnotations(annots)
		return cli.Patch(ctx, sts, client.MergeFrom(orig))
	}); err != nil {
		return false, errors.Wrap(err, "failed to update stateful set annotations")
	}
	return false, nil
}
