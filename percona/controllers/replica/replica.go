package replica

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"strconv"

	"github.com/percona/percona-postgresql-operator/internal/config"
	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func Create(clientset kubeapi.Interface, cluster *crv1.PerconaPGCluster) error {
	ctx := context.TODO()

	for i := 1; i <= cluster.Spec.PGReplicas.HotStandby.Size; i++ {
		replica := getNewReplicaObject(cluster, i)
		_, err := clientset.CrunchydataV1().Pgreplicas(cluster.Namespace).Create(ctx, &replica, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrapf(err, "create replica %s", replica.Name)
		}
	}
	log.Println("Handle service update")
	err := createOrUpdateReplicaService(clientset, cluster)
	if err != nil {
		return errors.Wrap(err, "handle replica service")
	}
	return nil
}
func Update(clientset kubeapi.Interface, newCluster, oldCluster *crv1.PerconaPGCluster) error {
	ctx := context.TODO()
	if reflect.DeepEqual(oldCluster.Spec.PGReplicas, newCluster.Spec.PGReplicas) {
		return nil
	}

	oldReplicaCount := 0
	newReplicaCount := 0
	var err error

	if newCluster.Spec.PGReplicas != nil {
		newReplicaCount = newCluster.Spec.PGReplicas.HotStandby.Size
	}
	if oldCluster.Spec.PGReplicas != nil {
		oldReplicaCount = oldCluster.Spec.PGReplicas.HotStandby.Size
	}

	if newReplicaCount == 0 {
		for i := 1; i <= oldReplicaCount; i++ {
			replicaName := getReplicaName(oldCluster, i)
			err = clientset.CrunchydataV1().Pgreplicas(newCluster.Namespace).Delete(ctx, replicaName, metav1.DeleteOptions{})
			if err != nil {
				return errors.Wrapf(err, "delete replica %s", replicaName)
			}
		}
		return nil
	}
	if newReplicaCount < oldReplicaCount {
		for i := oldReplicaCount; i > newReplicaCount; i-- {
			replicaName := getReplicaName(oldCluster, i)
			err = clientset.CrunchydataV1().Pgreplicas(newCluster.Namespace).Delete(ctx, replicaName, metav1.DeleteOptions{})
			if err != nil {
				return errors.Wrapf(err, "delete replica %s", replicaName)
			}
		}
	}

	for i := 1; i <= newReplicaCount; i++ {
		replica := getNewReplicaObject(newCluster, i)
		oldReplica, err := clientset.CrunchydataV1().Pgreplicas(newCluster.Namespace).Get(ctx, replica.Name, metav1.GetOptions{})
		if err != nil {

			_, err = clientset.CrunchydataV1().Pgreplicas(newCluster.Namespace).Create(ctx, &replica, metav1.CreateOptions{})
			if err != nil {
				return errors.Wrapf(err, "create replica %s", replica.Name)
			}
			continue
		}
		if reflect.DeepEqual(oldReplica.Spec, replica.Spec) {
			continue
		}
		replica.ResourceVersion = oldReplica.ResourceVersion
		_, err = clientset.CrunchydataV1().Pgreplicas(newCluster.Namespace).Update(ctx, &replica, metav1.UpdateOptions{})
		if err != nil {
			return errors.Wrapf(err, "update replica %s", replica.Name)
		}

	}
	log.Println("Handle service update")
	err = createOrUpdateReplicaService(clientset, newCluster)
	if err != nil {
		return errors.Wrap(err, "handle replica service")
	}
	return nil
}

func getReplicaName(cluster *crv1.PerconaPGCluster, index int) string {
	return cluster.Name + "-repl" + strconv.Itoa(index)
}

func getNewReplicaObject(cluster *crv1.PerconaPGCluster, index int) crv1.Pgreplica {
	labels := make(map[string]string)
	labels[config.LABEL_PG_CLUSTER] = cluster.Name
	labels[config.LABEL_NAME] = cluster.Name + "-repl" + strconv.Itoa(index)
	if cluster.Spec.PGReplicas.HotStandby.Expose.Labels != nil {
		for k, v := range cluster.Spec.PGReplicas.HotStandby.Expose.Labels {
			labels[k] = v
		}
	}
	annotations := make(map[string]string)
	if cluster.Spec.PGReplicas.HotStandby.Expose.Annotations != nil {
		for k, v := range cluster.Spec.PGReplicas.HotStandby.Expose.Annotations {
			annotations[k] = v
		}
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
	spec := crv1.PgreplicaSpec{
		Name:           labels[config.LABEL_NAME],
		ReplicaStorage: storage,
		UserLabels:     cluster.Spec.UserLabels,
		ClusterName:    cluster.Name,
	}
	if cluster.Spec.PGPrimary.Affinity.NodeAffinity != nil {
		spec.NodeAffinity = cluster.Spec.PGPrimary.Affinity.NodeAffinity
	}
	newReplica := crv1.Pgreplica{
		ObjectMeta: metav1.ObjectMeta{
			Name:        labels[config.LABEL_NAME],
			Namespace:   cluster.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: spec,
		Status: crv1.PgreplicaStatus{
			State:   crv1.PgreplicaStateCreated,
			Message: "Created, not processed yet",
		},
	}
	if cluster.Spec.PGReplicas != nil {
		if len(cluster.Spec.PGReplicas.HotStandby.Expose.ServiceType) > 0 {
			newReplica.Spec.ServiceType = cluster.Spec.PGReplicas.HotStandby.Expose.ServiceType
		}
		if cluster.Spec.PGReplicas.HotStandby.VolumeSpec != nil {
			newReplica.Spec.ReplicaStorage = *cluster.Spec.PGReplicas.HotStandby.VolumeSpec
		}
		if len(cluster.Spec.PGReplicas.HotStandby.Expose.Labels) > 0 {
			for k, v := range cluster.Spec.PGReplicas.HotStandby.Expose.Labels {
				if k == config.LABEL_PG_CLUSTER {
					continue
				}
				newReplica.Labels[k] = v
			}
		}
		if len(cluster.Spec.PGReplicas.HotStandby.Expose.Annotations) > 0 {
			if len(newReplica.Annotations) == 0 {
				newReplica.Annotations = make(map[string]string)
			}
			for k, v := range cluster.Spec.PGReplicas.HotStandby.Expose.Annotations {
				newReplica.Annotations[k] = v
			}
		}
		if len(cluster.Spec.PGReplicas.HotStandby.Labels) > 0 {
			for k, v := range cluster.Spec.PGReplicas.HotStandby.Labels {
				if k == config.LABEL_PG_CLUSTER {
					continue
				}
				newReplica.Labels[k] = v
			}
		}
		if len(cluster.Spec.PGReplicas.HotStandby.Annotations) > 0 {
			if len(newReplica.Annotations) == 0 {
				newReplica.Annotations = make(map[string]string)
			}
			for k, v := range cluster.Spec.PGReplicas.HotStandby.Annotations {
				newReplica.Annotations[k] = v
			}
		}

	}

	return newReplica
}

func createOrUpdateReplicaService(clientset kubeapi.Interface, cluster *crv1.PerconaPGCluster) error {
	ctx := context.TODO()
	service, err := getReplicaServiceObject(cluster)
	if err != nil {
		return errors.Wrap(err, "get replica service object")
	}

	oldSvc, err := clientset.CoreV1().Services(cluster.Namespace).Get(ctx, getReplicaServiceName(cluster.Name), metav1.GetOptions{})
	if err != nil {
		_, err = clientset.CoreV1().Services(cluster.Namespace).Create(ctx, &service, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrap(err, "create replica service")
		}
		return nil
	}

	service.ResourceVersion = oldSvc.ResourceVersion
	service.Spec.ClusterIP = oldSvc.Spec.ClusterIP
	_, err = clientset.CoreV1().Services(cluster.Namespace).Update(ctx, &service, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrap(err, "update replica service")
	}

	return nil
}

func getReplicaServiceObject(cluster *crv1.PerconaPGCluster) (corev1.Service, error) {
	replicaName := getReplicaServiceName(cluster.Name)
	labels := map[string]string{
		"name":       replicaName,
		"pg-cluster": cluster.Name,
	}
	if cluster.Spec.PGReplicas.HotStandby.Expose.Labels != nil {
		for k, v := range cluster.Spec.PGReplicas.HotStandby.Expose.Labels {
			labels[k] = v
		}
	}
	annotations := make(map[string]string)
	if cluster.Spec.PGReplicas.HotStandby.Expose.Annotations != nil {
		for k, v := range cluster.Spec.PGReplicas.HotStandby.Expose.Annotations {
			annotations[k] = v
		}
	}
	port, err := strconv.Atoi(cluster.Spec.Port)
	if err != nil {
		return corev1.Service{}, errors.Wrap(err, "parse port")
	}
	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        replicaName,
			Namespace:   cluster.Namespace,
			Labels:      labels,
			Annotations: annotations,
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

func UpdateResources(cl *crv1.PerconaPGCluster, cluster *crv1.Pgcluster, deployment *appsv1.Deployment) error {
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
