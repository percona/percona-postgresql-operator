package k8s

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-postgresql-operator/percona/naming"
	"github.com/percona/percona-postgresql-operator/percona/version"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

const WatchNamespaceEnvVar = "WATCH_NAMESPACE"

// GetWatchNamespace returns the namespace the operator should be watching for changes
func GetWatchNamespace() (string, error) {
	// This is needed in order to preserve backwards compatibility with the
	// users that are using the PGO_TARGET_NAMESPACE env var.
	ns, found := os.LookupEnv("PGO_TARGET_NAMESPACE")
	if found {
		return ns, nil
	}

	ns, found = os.LookupEnv(WatchNamespaceEnvVar)
	if !found {
		return "", fmt.Errorf("%s must be set", WatchNamespaceEnvVar)
	}

	return ns, nil
}

func InitContainer(cluster *v1beta1.PostgresCluster, componentName, image string,
	pullPolicy corev1.PullPolicy,
	secCtx *corev1.SecurityContext,
	resources corev1.ResourceRequirements,
	component ComponentWithInit,
) corev1.Container {
	if component != nil && component.GetInitContainer() != nil && component.GetInitContainer().Resources != nil {
		resources = *component.GetInitContainer().Resources
	} else if ic := cluster.Spec.InitContainer; ic != nil && ic.Resources != nil {
		resources = *ic.Resources
	}
	if component != nil && component.GetInitContainer() != nil && component.GetInitContainer().ContainerSecurityContext != nil {
		secCtx = component.GetInitContainer().ContainerSecurityContext
	} else if ic := cluster.Spec.InitContainer; ic != nil && ic.ContainerSecurityContext != nil {
		secCtx = ic.ContainerSecurityContext
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      naming.CrunchyBinVolumeName,
			MountPath: naming.CrunchyBinVolumePath,
		},
	}

	return corev1.Container{
		Name:                     componentName + "-init",
		Image:                    image,
		ImagePullPolicy:          pullPolicy,
		VolumeMounts:             volumeMounts,
		Command:                  []string{"/usr/local/bin/init-entrypoint.sh"},
		TerminationMessagePath:   "/dev/termination-log",
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		SecurityContext:          secCtx,
		Resources:                resources,
	}
}

type ComponentWithInit interface {
	GetInitContainer() *v1beta1.InitContainerSpec
}

func InitImage(ctx context.Context, cl client.Reader, cluster *v1beta1.PostgresCluster, componentWithInit ComponentWithInit) (string, error) {
	if componentWithInit != nil && componentWithInit.GetInitContainer() != nil && componentWithInit.GetInitContainer().Image != "" {
		return componentWithInit.GetInitContainer().Image, nil
	}
	if cluster != nil && cluster.Spec.InitContainer != nil && len(cluster.Spec.InitContainer.Image) > 0 {
		return cluster.Spec.InitContainer.Image, nil
	}

	operatorImage, err := operatorImage(ctx, cl)
	if err != nil {
		return "", errors.Wrap(err, "get operator image")
	}

	imageName := operatorImage

	if cluster == nil {
		return imageName, nil
	}

	crVersion, ok := cluster.Labels[v1beta1.LabelVersion]
	if !ok || crVersion == "" {
		return imageName, nil
	}

	if cluster.CompareVersion(version.Version()) != 0 {
		imageName = strings.Split(operatorImage, ":")[0] + ":" + crVersion
	}

	return imageName, nil
}

func operatorImage(ctx context.Context, cl client.Reader) (string, error) {
	pod, err := operatorPod(ctx, cl)
	if err != nil {
		return "", errors.Wrap(err, "get operator pod")
	}

	for _, container := range pod.Spec.Containers {
		if container.Name == "operator" {
			return container.Image, nil
		}
	}

	return "", errors.New("manager container not found")
}

func operatorPod(ctx context.Context, cl client.Reader) (*corev1.Pod, error) {
	ns, err := GetOperatorNamespace()
	if err != nil {
		return nil, errors.Wrap(err, "get namespace")
	}

	pod := new(corev1.Pod)
	nn := types.NamespacedName{
		Namespace: ns,
		Name:      os.Getenv("HOSTNAME"),
	}
	if err := cl.Get(ctx, nn, pod); err != nil {
		return nil, err
	}

	return pod, nil
}

// GetOperatorNamespace returns the namespace of the operator pod
func GetOperatorNamespace() (string, error) {
	ns, found := os.LookupEnv("OPERATOR_NAMESPACE")
	if found {
		return ns, nil
	}
	nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(nsBytes)), nil
}

func ObjectHash(obj runtime.Object) (string, error) {
	var dataToMarshal interface{}

	switch object := obj.(type) {
	case *appsv1.StatefulSet:
		dataToMarshal = object.Spec
	case *appsv1.Deployment:
		dataToMarshal = object.Spec
	case *corev1.Service:
		dataToMarshal = object.Spec
	case *corev1.Secret:
		dataToMarshal = object.Data
	default:
		dataToMarshal = obj
	}

	data, err := json.Marshal(dataToMarshal)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}
