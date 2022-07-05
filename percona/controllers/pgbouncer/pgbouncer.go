package pgbouncer

import (
	"context"
	"reflect"

	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
	dplmnt "github.com/percona/percona-postgresql-operator/percona/controllers/deployment"
	"github.com/percona/percona-postgresql-operator/percona/controllers/service"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"

	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func UpdateDeployment(clientset kubeapi.Interface, newPerconaPGCluster, oldPerconaPGCluster *crv1.PerconaPGCluster) error {
	ctx := context.TODO()
	deployment, err := clientset.AppsV1().Deployments(newPerconaPGCluster.Namespace).Get(ctx,
		newPerconaPGCluster.Name+"-pgbouncer", metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.Wrap(err, "get deployment")
	} else if err != nil {
		return nil
	}
	dplmnt.UpdateSpecTemplateAffinity(deployment, *newPerconaPGCluster.Spec.PGBouncer.Affinity)
	dplmnt.UpdateDeploymentContainer(deployment, dplmnt.ContainerPGBouncer,
		newPerconaPGCluster.Spec.PGBouncer.Image,
		newPerconaPGCluster.Spec.PGBouncer.ImagePullPolicy)

	_, err = clientset.AppsV1().Deployments(deployment.Namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrap(err, "update deployment")
	}

	return nil
}

func Update(clientset kubeapi.Interface, newPerconaPGCluster, oldPerconaPGCluster *crv1.PerconaPGCluster) error {
	if reflect.DeepEqual(oldPerconaPGCluster.Spec.PGBouncer, newPerconaPGCluster.Spec.PGBouncer) {
		return nil
	}
	if newPerconaPGCluster.Spec.PGBouncer.Size == 0 {
		return nil
	}
	err := service.CreateOrUpdate(clientset, newPerconaPGCluster, service.PGBouncerServiceType)
	if err != nil {
		return errors.Wrap(err, "handle bouncer service on update")
	}
	err = UpdateDeployment(clientset, newPerconaPGCluster, oldPerconaPGCluster)
	if err != nil {
		return errors.Wrap(err, "update deployment")
	}

	return nil
}
