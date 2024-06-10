package controller

import (
	"context"

	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/percona/percona-postgresql-operator/internal/naming"
)

// jobCompleted returns "true" if the Job provided completed successfully.  Otherwise it returns
// "false".
func JobCompleted(job *batchv1.Job) bool {
	conditions := job.Status.Conditions
	for i := range conditions {
		if conditions[i].Type == batchv1.JobComplete {
			return conditions[i].Status == corev1.ConditionTrue
		}
	}
	return false
}

// jobFailed returns "true" if the Job provided has failed.  Otherwise it returns "false".
func JobFailed(job *batchv1.Job) bool {
	conditions := job.Status.Conditions
	for i := range conditions {
		if conditions[i].Type == batchv1.JobFailed {
			return conditions[i].Status == corev1.ConditionTrue
		}
	}
	return false
}

// CustomManager is needed to receive a crunchy controller without modifying the crunchy code.
// It should be used in the `(r *postgrescluster.Reconciler) SetupWithManager(mgr manager.Manager)` method.
// A Crunchy controller is received when `controller.New` is called which uses the `Add` method.
// This manager has a custom `Add` method which additionally sets a controller to the manager if provided.
type CustomManager struct {
	manager.Manager

	ctrl controller.Controller
}

func (m *CustomManager) Controller() controller.Controller {
	return m.ctrl
}

func (m *CustomManager) Add(r manager.Runnable) error {
	if err := m.Manager.Add(r); err != nil {
		return err
	}
	if ctrl, ok := r.(controller.Controller); ok {
		m.ctrl = ctrl
	}
	return nil
}

func GetReadyInstancePod(ctx context.Context, c client.Client, clusterName, namespace string) (*corev1.Pod, error) {
	pods := &corev1.PodList{}
	selector, err := naming.AsSelector(naming.ClusterInstances(clusterName))
	if err != nil {
		return nil, err
	}
	if err := c.List(ctx, pods, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, errors.Wrap(err, "list pods")
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		return &pod, nil
	}
	return nil, errors.New("no running instance found")
}
