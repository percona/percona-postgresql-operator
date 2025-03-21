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

func InitContainer(component, image string,
	pullPolicy corev1.PullPolicy,
	secCtx *corev1.SecurityContext,
	resources corev1.ResourceRequirements,
) corev1.Container {
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      naming.CrunchyBinVolumeName,
			MountPath: naming.CrunchyBinVolumePath,
		},
	}

	return corev1.Container{
		Name:                     component + "-init",
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

func InitImage(ctx context.Context, cl client.Reader) (string, error) {
	// TODO:
	//if image := cr.Spec.InitImage; len(image) > 0 {
	//	return image, nil
	//}
	return OperatorImage(ctx, cl)
}

func OperatorImage(ctx context.Context, cl client.Reader) (string, error) {
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
