package perconaPG

import (
	"context"

	gover "github.com/hashicorp/go-version"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

func determineVersion(cr *v2.PerconaPGCluster) string {
	if cr.CompareVersion("2.7.0") <= 0 {
		return cr.Status.PatroniVersion
	}
	return patroniVersion4
}
