package replica

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/percona/percona-postgresql-operator/internal/config"
	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
	"github.com/percona/percona-postgresql-operator/internal/operator/pvc"
	"github.com/percona/percona-postgresql-operator/percona/controllers/pmm"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func Create(clientset kubeapi.Interface, cluster *crv1.PerconaPGCluster) error {
	ctx := context.TODO()
	if cluster.Spec.PGReplicas.HotStandby.Size == 0 {
		return nil
	}
	err := createOrUpdateReplicaService(clientset, cluster)
	if err != nil {
		return errors.Wrap(err, "handle replica service")
	}
	for i := 1; i <= cluster.Spec.PGReplicas.HotStandby.Size; i++ {
		replica := getNewReplicaObject(cluster, &crv1.Pgreplica{}, i)
		_, err := clientset.CrunchydataV1().Pgreplicas(cluster.Namespace).Create(ctx, replica, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrapf(err, "create replica %s", replica.Name)
		}
		for i := 0; i <= 30; i++ {
			time.Sleep(5 * time.Second)
			dep, err := clientset.AppsV1().Deployments(cluster.Namespace).Get(ctx,
				replica.Name, metav1.GetOptions{})

			if err != nil {
				log.Info(errors.Wrapf(err, "get deployment %s", replica.Name))
			}
			if dep.Status.UnavailableReplicas == 0 {
				fmt.Println(replica.Name, "ready")
				break
			}
		}
	}

	return nil
}

func Update(clientset kubeapi.Interface, newCluster, oldCluster *crv1.PerconaPGCluster) error {
	ctx := context.TODO()

	oldReplicaCount := 0
	newReplicaCount := 0
	var err error

	if newCluster.Spec.PGReplicas != nil {
		newReplicaCount = newCluster.Spec.PGReplicas.HotStandby.Size
	}
	if oldCluster.Spec.PGReplicas != nil {
		oldReplicaCount = oldCluster.Spec.PGReplicas.HotStandby.Size
	}

	if newReplicaCount > 0 {
		err = createOrUpdateReplicaService(clientset, newCluster)
		if err != nil {
			return errors.Wrap(err, "handle replica service")
		}
	}

	if newReplicaCount == 0 {
		for i := oldReplicaCount; i >= 1; i-- {
			err = deleteReplica(clientset, oldCluster, newCluster, i)
			if err != nil {
				return errors.Wrapf(err, "delete replica %s", getReplicaName(oldCluster, i))
			}
		}
		err = clientset.CoreV1().Services(newCluster.Namespace).Delete(ctx, getReplicaServiceName(newCluster.Name), metav1.DeleteOptions{})
		if err != nil {
			return errors.Wrapf(err, "delete replicas service")
		}
		return nil
	}
	if newReplicaCount < oldReplicaCount {
		for i := oldReplicaCount; i > newReplicaCount; i-- {
			err = deleteReplica(clientset, oldCluster, newCluster, i)
			if err != nil {
				return errors.Wrapf(err, "delete replica %s", getReplicaName(oldCluster, i))
			}
		}
	}

	for i := newReplicaCount; i >= 1; i-- {
		replicaName := getReplicaName(oldCluster, i)
		oldReplica, err := clientset.CrunchydataV1().Pgreplicas(newCluster.Namespace).Get(ctx, replicaName, metav1.GetOptions{})
		if err != nil {
			replica := getNewReplicaObject(newCluster, &crv1.Pgreplica{}, i)
			_, err = clientset.CrunchydataV1().Pgreplicas(newCluster.Namespace).Create(ctx, replica, metav1.CreateOptions{})
			if err != nil {
				return errors.Wrapf(err, "create replica %s", replica.Name)
			}
			continue
		}
		replica := getNewReplicaObject(newCluster, oldReplica, i)

		replica.ResourceVersion = oldReplica.ResourceVersion
		replica.Status = oldReplica.Status

		err = updateDeployment(clientset, replica)
		if err != nil {
			return errors.Wrapf(err, "update replica deployment%s", replica.Name)
		}
		if !reflect.DeepEqual(newCluster.Spec.PGReplicas.HotStandby, oldCluster.Spec.PGReplicas.HotStandby) {
			_, err = clientset.CrunchydataV1().Pgreplicas(newCluster.Namespace).Update(ctx, replica, metav1.UpdateOptions{})
			if err != nil {
				return errors.Wrapf(err, "update replica %s", replica.Name)
			}
		}

		for i := 0; i <= 30; i++ {
			time.Sleep(5 * time.Second)
			dep, err := clientset.AppsV1().Deployments(newCluster.Namespace).Get(ctx,
				replicaName, metav1.GetOptions{})
			if err != nil {
				log.Info(errors.Wrapf(err, "get deployment %s", replica.Name))
			}
			if dep.Status.UnavailableReplicas == 0 {
				break
			}
		}
	}

	return nil
}

func deleteReplica(clientset kubeapi.Interface, oldCluster, newCluster *crv1.PerconaPGCluster, i int) error {
	ctx := context.TODO()
	replicaName := getReplicaName(oldCluster, i)
	err := clientset.CrunchydataV1().Pgreplicas(newCluster.Namespace).Delete(ctx, replicaName, metav1.DeleteOptions{})
	if err != nil {
		return errors.Wrapf(err, "delete replica %s", replicaName)
	}
	if !newCluster.Spec.KeepData {
		err = pvc.DeleteIfExists(clientset, replicaName, oldCluster.ObjectMeta.Namespace)
		if err != nil {
			return errors.Wrapf(err, "delete replica %s pvc", replicaName)
		}
	}
	for i := 0; i <= 30; i++ {
		_, err := clientset.AppsV1().Deployments(newCluster.Namespace).Get(ctx,
			replicaName, metav1.GetOptions{})
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return nil
			}
		}
		time.Sleep(5 * time.Second)
	}

	return errors.Errorf("Can't delete replica %s", replicaName)
}

func getReplicaName(cluster *crv1.PerconaPGCluster, index int) string {
	return cluster.Name + "-repl" + strconv.Itoa(index)
}

func getNewReplicaObject(cluster *crv1.PerconaPGCluster, replica *crv1.Pgreplica, index int) *crv1.Pgreplica {
	if cluster.Spec.PGReplicas == nil {
		return nil
	}
	labels := map[string]string{
		config.LABEL_PG_CLUSTER: cluster.Name,
		config.LABEL_NAME:       cluster.Name + "-repl" + strconv.Itoa(index),
		"pgouser":               "admin",
	}
	for k, v := range cluster.Spec.PGReplicas.HotStandby.Labels {
		labels[k] = v
	}

	storage := crv1.PgStorageSpec{}
	if cluster.Spec.PGReplicas.HotStandby.VolumeSpec != nil {
		storageName := getReplicaName(cluster, index)
		if len(cluster.Spec.PGReplicas.HotStandby.VolumeSpec.Name) > 0 {
			storageName = cluster.Spec.PGReplicas.HotStandby.VolumeSpec.Name
		}
		storage = crv1.PgStorageSpec{
			Name:               storageName,
			StorageClass:       cluster.Spec.PGReplicas.HotStandby.VolumeSpec.StorageClass,
			AccessMode:         cluster.Spec.PGReplicas.HotStandby.VolumeSpec.AccessMode,
			Size:               cluster.Spec.PGReplicas.HotStandby.VolumeSpec.Size,
			StorageType:        cluster.Spec.PGReplicas.HotStandby.VolumeSpec.StorageType,
			SupplementalGroups: cluster.Spec.PGReplicas.HotStandby.VolumeSpec.SupplementalGroups,
			MatchLabels:        cluster.Spec.PGReplicas.HotStandby.VolumeSpec.MatchLabels,
		}
	}

	replica.ObjectMeta.Name = labels[config.LABEL_NAME]
	replica.ObjectMeta.Namespace = cluster.Namespace
	if replica.ObjectMeta.Labels == nil {
		replica.ObjectMeta.Labels = labels
	} else {
		for k, v := range labels {
			replica.ObjectMeta.Labels[k] = v
		}
	}
	if replica.ObjectMeta.Annotations == nil {
		replica.ObjectMeta.Annotations = make(map[string]string)
	}
	for k, v := range cluster.Spec.PGReplicas.HotStandby.Annotations {
		replica.ObjectMeta.Annotations[k] = v
	}

	if cluster.Spec.PGReplicas.HotStandby.Affinity != nil {
		replica.Spec.NodeAffinity = cluster.Spec.PGReplicas.HotStandby.Affinity.NodeAffinity
	}
	replica.Spec.Name = labels[config.LABEL_NAME]
	replica.Spec.ReplicaStorage = storage
	replica.Spec.UserLabels = cluster.Spec.UserLabels
	replica.Spec.ClusterName = cluster.Name
	if len(cluster.Spec.PGReplicas.HotStandby.Expose.ServiceType) > 0 {
		replica.Spec.ServiceType = cluster.Spec.PGReplicas.HotStandby.Expose.ServiceType
	}

	return replica
}

func createOrUpdateReplicaService(clientset kubeapi.Interface, cluster *crv1.PerconaPGCluster) error {
	ctx := context.TODO()
	service, err := getReplicaServiceObject(cluster)
	if err != nil {
		return errors.Wrap(err, "get replica service object")
	}

	oldSvc, err := clientset.CoreV1().Services(cluster.Namespace).Get(ctx, getReplicaServiceName(cluster.Name), metav1.GetOptions{})
	if err != nil {
		_, err = clientset.CoreV1().Services(cluster.Namespace).Create(ctx, service, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrap(err, "create replica service")
		}
		return nil
	}
	if reflect.DeepEqual(service.Spec, oldSvc.Spec) {
		return nil
	}
	service.ResourceVersion = oldSvc.ResourceVersion
	service.Spec.ClusterIP = oldSvc.Spec.ClusterIP
	_, err = clientset.CoreV1().Services(cluster.Namespace).Update(ctx, service, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrap(err, "update replica service")
	}

	return nil
}

func getReplicaServiceObject(cluster *crv1.PerconaPGCluster) (*corev1.Service, error) {
	replicaName := getReplicaServiceName(cluster.Name)
	labels := map[string]string{
		"name":       replicaName,
		"pg-cluster": cluster.Name,
	}

	for k, v := range cluster.Spec.PGReplicas.HotStandby.Expose.Labels {
		labels[k] = v
	}

	port, err := strconv.Atoi(cluster.Spec.Port)
	if err != nil {
		return &corev1.Service{}, errors.Wrap(err, "parse port")
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        replicaName,
			Namespace:   cluster.Namespace,
			Labels:      labels,
			Annotations: cluster.Spec.PGReplicas.HotStandby.Expose.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Type: cluster.Spec.PGReplicas.HotStandby.Expose.ServiceType,
			Ports: []corev1.ServicePort{
				{
					Name:       "sshd",
					Protocol:   corev1.ProtocolTCP,
					Port:       2022,
					TargetPort: intstr.FromInt(2022),
					NodePort:   0,
				},
				{
					Name:       "postgres",
					Protocol:   corev1.ProtocolTCP,
					Port:       int32(port),
					TargetPort: intstr.FromInt(port),
					NodePort:   0,
				},
			},
			Selector: map[string]string{
				"pg-cluster": cluster.Name,
				"role":       "replica",
			},
			SessionAffinity:          corev1.ServiceAffinityNone,
			LoadBalancerSourceRanges: cluster.Spec.PGReplicas.HotStandby.Expose.LoadBalancerSourceRanges,
		},
	}, nil
}

func getReplicaServiceName(clusterName string) string {
	return fmt.Sprintf("%s-replica", clusterName)
}

func updateResources(cl *crv1.PerconaPGCluster, deployment *appsv1.Deployment) error {
	if cl.Spec.PGReplicas == nil {
		return nil
	}
	if cl.Spec.PGReplicas.HotStandby.Size == 0 {
		return nil
	}
	if cl.Spec.PGReplicas.HotStandby.Resources == nil {
		return nil
	}
	for k := range deployment.Spec.Template.Spec.Containers {
		if deployment.Spec.Template.Spec.Containers[k].Name == "database" {
			deployment.Spec.Template.Spec.Containers[k].Resources.Limits = cl.Spec.PGReplicas.HotStandby.Resources.Limits
			deployment.Spec.Template.Spec.Containers[k].Resources.Requests = cl.Spec.PGReplicas.HotStandby.Resources.Requests
		}
	}

	return nil
}

func updateAnnotations(cl *crv1.PerconaPGCluster, deployment *appsv1.Deployment) {
	if cl.Spec.PGReplicas == nil {
		return
	}
	if cl.Spec.PGReplicas.HotStandby.Size == 0 {
		return
	}
	if cl.Spec.PGReplicas.HotStandby.Annotations == nil {
		return
	}
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	for k, v := range cl.Spec.PGReplicas.HotStandby.Annotations {
		deployment.Spec.Template.Annotations[k] = v
	}

	return
}

func updateLabels(cl *crv1.PerconaPGCluster, deployment *appsv1.Deployment) {
	if cl.Spec.PGReplicas == nil {
		return
	}
	if cl.Spec.PGReplicas.HotStandby.Size == 0 {
		return
	}
	if cl.Spec.PGReplicas.HotStandby.Labels == nil {
		return
	}
	if deployment.Spec.Template.Labels == nil {
		deployment.Spec.Template.Labels = make(map[string]string)
	}
	for k, v := range cl.Spec.PGReplicas.HotStandby.Labels {
		deployment.Spec.Template.Labels[k] = v
	}

	return
}

func updateDeployment(clientset kubeapi.Interface, replica *crv1.Pgreplica) error {
	ctx := context.TODO()
	deployment, err := clientset.AppsV1().Deployments(replica.Namespace).Get(ctx,
		replica.Name, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "could not find deployment for pgreplica %s", replica.Name)

	}
	cl, err := clientset.CrunchydataV1().PerconaPGClusters(replica.Namespace).Get(ctx, replica.Spec.ClusterName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, "get perconapgcluster resource")
	}
	updateAnnotations(cl, deployment)
	updateLabels(cl, deployment)
	err = pmm.AddOrRemovePMMSidecar(cl, replica.Spec.ClusterName, deployment)
	if err != nil {
		return errors.Wrap(err, "add or remove pmm sidecar: %s")
	}
	err = updateResources(cl, deployment)
	if err != nil {
		return errors.Wrap(err, "update replica resources resource: %s")
	}
	if _, err := clientset.AppsV1().Deployments(deployment.Namespace).Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		return errors.Wrapf(err, "could not update deployment for pgreplica: %s", replica.Name)
	}

	return nil
}
