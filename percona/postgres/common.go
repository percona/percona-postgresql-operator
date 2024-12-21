package postgres

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
)

func GetPrimaryPod(ctx context.Context, cli client.Client, cr *v2.PerconaPGCluster) (*corev1.Pod, error) {
	podList := &corev1.PodList{}
	err := cli.List(ctx, podList, &client.ListOptions{
		Namespace: cr.Namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"app.kubernetes.io/instance":             cr.Name,
			"postgres-operator.crunchydata.com/role": "master",
		}),
	})
	if err != nil {
		return nil, err
	}

	if len(podList.Items) == 0 {
		return nil, errors.New("no primary pod found")
	}

	if len(podList.Items) > 1 {
		return nil, errors.New("multiple primary pods found")
	}

	return &podList.Items[0], nil
}
