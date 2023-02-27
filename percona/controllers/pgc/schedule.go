package pgc

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"

	"github.com/percona/percona-postgresql-operator/cmd/pgo-scheduler/scheduler"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type actionType string
type s struct {
	job    crv1.CronJob
	action actionType
}

const (
	keep         actionType = "keep"
	del          actionType = "delete"
	update       actionType = "update"
	create       actionType = "create"
	storageLocal            = "local"
)

func (c *Controller) handleScheduleBackup(newCluster, oldCluster *crv1.PerconaPGCluster) error {
	ctx := context.TODO()
	if newCluster == nil {
		return nil
	}
	if oldCluster != nil && reflect.DeepEqual(newCluster.Spec.Backup.Schedule, oldCluster.Spec.Backup.Schedule) {
		return nil
	}
	if oldCluster == nil {
		oldCluster = &crv1.PerconaPGCluster{}
	}

	sm := make(map[string]s)
	for _, schedule := range oldCluster.Spec.Backup.Schedule {
		cmName := newCluster.Name + "-" + schedule.Name
		sm[cmName] = s{schedule, del}
	}

	for _, scheduleJob := range newCluster.Spec.Backup.Schedule {
		cmName := newCluster.Name + "-" + scheduleJob.Name
		oldSchedule, ok := sm[cmName]
		sm[cmName] = s{scheduleJob, create}
		if ok && reflect.DeepEqual(scheduleJob, oldSchedule.job) {
			sm[cmName] = s{oldSchedule.job, keep}
		} else if ok {
			sm[cmName] = s{scheduleJob, update}
		}
	}

	for name, s := range sm {
		switch s.action {
		case create:
			scheduleConfigMap, err := getScheduleConfigMap(name, s, newCluster)
			if err != nil {
				return errors.Wrap(err, "get schedule config map")
			}
			_, err = c.Client.CoreV1().ConfigMaps(newCluster.Namespace).Create(ctx, scheduleConfigMap, metav1.CreateOptions{})
			if err != nil {
				return errors.Wrap(err, "create config map")
			}
		case del:
			err := c.Client.CoreV1().ConfigMaps(newCluster.Namespace).Delete(ctx, name, metav1.DeleteOptions{})
			if err != nil && !kerrors.IsNotFound(err) {
				return errors.Wrapf(err, "delete config map %s", name)
			}
		case update:
			scheduleConfigMap, err := getScheduleConfigMap(name, s, newCluster)
			if err != nil {
				return errors.Wrap(err, "get schedule config map")
			}
			_, err = c.Client.CoreV1().ConfigMaps(newCluster.Namespace).Update(ctx, scheduleConfigMap, metav1.UpdateOptions{})
			if err != nil {
				return errors.Wrap(err, "update config map")
			}
		}
	}

	return nil
}
func getScheduleConfigMap(name string, schedule s, newCluster *crv1.PerconaPGCluster) (*v1.ConfigMap, error) {
	storage, ok := newCluster.Spec.Backup.Storages[schedule.job.Storage]
	if !ok && schedule.job.Storage != storageLocal {
		return nil, errors.Errorf("invalid storage name '%s' in schedule '%s'", schedule.job.Storage, schedule.job.Name)
	}
	storageType := storageLocal
	if ok {
		storageType = string(storage.Type)
	}
	scheduleTemp := scheduler.ScheduleTemplate{
		Name:      schedule.job.Name,
		Schedule:  schedule.job.Schedule,
		Type:      "pgbackrest",
		Cluster:   newCluster.Name,
		Namespace: newCluster.Namespace,
		PGBackRest: scheduler.PGBackRest{
			Type:        schedule.job.Type,
			StorageType: storageType,
			Deployment:  newCluster.Name,
			Container:   "database",
			Options:     schedule.job.PGBackrestOpts,
		},
	}
	if schedule.job.Keep > 0 {
		keepStr := strconv.FormatInt(schedule.job.Keep, 10)
		switch schedule.job.Type {
		case "full":
			scheduleTemp.PGBackRest.Options = scheduleTemp.PGBackRest.Options + " --repo1-retention-full=" + keepStr
		case "diff":
			scheduleTemp.PGBackRest.Options = scheduleTemp.PGBackRest.Options + " --repo1-retention-diff=" + keepStr
		}
	}
	cmSchedule, err := json.Marshal(scheduleTemp)
	if err != nil {
		return nil, errors.Wrap(err, "marshal schedule template")
	}
	data := map[string]string{
		"schedule": string(cmSchedule),
	}
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"crunchy-scheduler": "true",
				"pg-cluster":        newCluster.Name,
			},
			Namespace: newCluster.Namespace,
		},
		Data: data,
	}, nil
}
