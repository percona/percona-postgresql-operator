package pgc

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/percona/percona-postgresql-operator/cmd/pgo-scheduler/scheduler"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
	"github.com/pkg/errors"

	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Controller) handleScheduleBackup(newCluster, oldCluster *crv1.PerconaPGCluster) error {
	ctx := context.TODO()
	if newCluster == nil {
		return nil
	}
	equal := true
	if oldCluster != nil && !reflect.DeepEqual(newCluster.Spec.Backup.Schedule, oldCluster.Spec.Backup.Schedule) {
		equal = false
	}
	if equal {
		return nil
	}
	for _, schedule := range oldCluster.Spec.Backup.Schedule {
		cmName := newCluster.Name + "-" + schedule.Name
		err := c.Client.CoreV1().ConfigMaps(newCluster.Namespace).Delete(ctx, cmName, metav1.DeleteOptions{})
		if err != nil {
			return errors.Wrapf(err, "delete config map %s", cmName)
		}
	}

	for _, schedule := range newCluster.Spec.Backup.Schedule {
		storage, ok := newCluster.Spec.Backup.Storages[schedule.Storage]
		if !ok {
			return errors.Errorf("invalid storage name: %s", schedule.Storage)
		}
		schedule := scheduler.ScheduleTemplate{
			Name:      schedule.Name,
			Schedule:  schedule.Schedule,
			Type:      "pgbackrest",
			Cluster:   newCluster.Name,
			Namespace: newCluster.Namespace,
			PGBackRest: scheduler.PGBackRest{
				Type:        schedule.Type,
				StorageType: string(storage.Type),
				Deployment:  newCluster.Name,
				Container:   "database",
			},
		}
		cmSchedule, err := json.Marshal(schedule)
		if err != nil {
			return errors.Wrap(err, "masrshal schedule template")
		}
		data := map[string]string{
			"schedule": string(cmSchedule),
		}
		scheduleConfigMap := v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: newCluster.Name + "-" + schedule.Name,
				Labels: map[string]string{
					"crunchy-scheduler": "true",
					"pg-cluster":        newCluster.Name,
				},
				Namespace: newCluster.Namespace,
			},
			Data: data,
		}
		_, err = c.Client.CoreV1().ConfigMaps(newCluster.Namespace).Create(ctx, &scheduleConfigMap, metav1.CreateOptions{})
		if err != nil && !kerrors.IsAlreadyExists(err) {
			return errors.Wrap(err, "create config map")
		}
	}

	return nil
}
