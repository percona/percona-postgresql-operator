package service

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type ServiceType string

const (
	PGPrimaryServiceType = ServiceType("primary")
	PGBouncerServiceType = ServiceType("bouncer")
	PGReplicaServiceType = ServiceType("replica")
)

func CreateOrUpdate(clientset kubeapi.Interface, cluster *crv1.PerconaPGCluster, svcType ServiceType) error {
	ctx := context.TODO()
	var service *corev1.Service
	switch svcType {
	case PGPrimaryServiceType:
		svc, err := getPrimaryServiceObject(cluster)
		if err != nil {
			return errors.Wrap(err, "get primary service object")
		}
		service = svc
	case PGBouncerServiceType:
		svc, err := getPGBouncerServiceObject(cluster)
		if err != nil {
			return errors.Wrap(err, "get pgBouncer service object")
		}
		service = svc
	case PGReplicaServiceType:
		svc, err := getReplicaServiceObject(cluster)
		if err != nil {
			return errors.Wrap(err, "get replica service object")
		}
		service = svc
	}
	oldSvc, err := clientset.CoreV1().Services(cluster.Namespace).Get(ctx, service.Name, metav1.GetOptions{})
	if err != nil {
		_, err = clientset.CoreV1().Services(cluster.Namespace).Create(ctx, service, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrapf(err, "create service %s", service.Name)
		}
		return nil
	}
	service.Spec.ClusterIP = oldSvc.Spec.ClusterIP
	if reflect.DeepEqual(service.Spec, oldSvc.Spec) {
		return nil
	}
	service.ResourceVersion = oldSvc.ResourceVersion
	_, err = clientset.CoreV1().Services(cluster.Namespace).Update(ctx, service, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrapf(err, "update service %s", svcType)
	}

	return nil
}

func Delete(clientset kubeapi.Interface, namespace, serviceName string) error {
	ctx := context.TODO()
	err := clientset.CoreV1().Services(namespace).Delete(ctx, serviceName, metav1.DeleteOptions{})
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return errors.Wrapf(err, "delete replicas service")
	}

	return nil
}

func getPrimaryServiceObject(cluster *crv1.PerconaPGCluster) (*corev1.Service, error) {
	labels := map[string]string{
		"name":       cluster.Name,
		"pg-cluster": cluster.Name,
	}

	for k, v := range cluster.Spec.PGPrimary.Expose.Labels {
		labels[k] = v
	}

	port, err := strconv.Atoi(cluster.Spec.Port)
	if err != nil {
		return &corev1.Service{}, errors.Wrap(err, "parse port")
	}
	svcType := corev1.ServiceTypeClusterIP
	if len(cluster.Spec.PGPrimary.Expose.ServiceType) > 0 {
		svcType = cluster.Spec.PGPrimary.Expose.ServiceType
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cluster.Name,
			Namespace:   cluster.Namespace,
			Labels:      labels,
			Annotations: cluster.Spec.PGPrimary.Expose.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Type: svcType,
			Ports: []corev1.ServicePort{
				{
					Name:       "sshd",
					Protocol:   corev1.ProtocolTCP,
					Port:       2022,
					TargetPort: intstr.FromInt(2022),
				},
				{
					Name:       "postgres",
					Protocol:   corev1.ProtocolTCP,
					Port:       int32(port),
					TargetPort: intstr.FromInt(port),
				},
			},
			Selector: map[string]string{
				"pg-cluster": cluster.Name,
				"role":       "master",
			},
			SessionAffinity:          corev1.ServiceAffinityNone,
			LoadBalancerSourceRanges: cluster.Spec.PGPrimary.Expose.LoadBalancerSourceRanges,
		},
	}, nil
}

func getPGBouncerServiceObject(cluster *crv1.PerconaPGCluster) (*corev1.Service, error) {
	svcName := cluster.Name + "-pgbouncer"
	labels := map[string]string{
		"name":       svcName,
		"pg-cluster": cluster.Name,
	}

	for k, v := range cluster.Spec.PGBouncer.Expose.Labels {
		labels[k] = v
	}

	port, err := strconv.Atoi(cluster.Spec.Port)
	if err != nil {
		return &corev1.Service{}, errors.Wrap(err, "parse port")
	}
	svcType := corev1.ServiceTypeClusterIP
	if len(cluster.Spec.PGBouncer.Expose.ServiceType) > 0 {
		svcType = cluster.Spec.PGBouncer.Expose.ServiceType
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        svcName,
			Namespace:   cluster.Namespace,
			Labels:      labels,
			Annotations: cluster.Spec.PGBouncer.Expose.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Type: svcType,
			Ports: []corev1.ServicePort{
				{
					Name:       "postgres",
					Protocol:   corev1.ProtocolTCP,
					Port:       int32(port),
					TargetPort: intstr.FromInt(port),
				},
			},
			Selector: map[string]string{
				"service-name": svcName,
			},
			SessionAffinity:          corev1.ServiceAffinityNone,
			LoadBalancerSourceRanges: cluster.Spec.PGBouncer.Expose.LoadBalancerSourceRanges,
		},
	}, nil
}

func getReplicaServiceObject(cluster *crv1.PerconaPGCluster) (*corev1.Service, error) {
	replicaName := GetReplicaServiceName(cluster.Name)
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

func GetReplicaServiceName(clusterName string) string {
	return fmt.Sprintf("%s-replica", clusterName)
}
