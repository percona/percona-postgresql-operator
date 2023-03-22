package pgc

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"time"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/percona/percona-postgresql-operator/cmd/pgo-scheduler/scheduler"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
)

type actionType string
type schedule struct {
	job       crv1.CronJob
	action    actionType
	configMap *v1.ConfigMap
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
	if oldCluster == nil {
		oldCluster = &crv1.PerconaPGCluster{}
	}

	scheduleMap := make(map[string]schedule)
	for _, job := range oldCluster.Spec.Backup.Schedule {
		cmName := newCluster.Name + "-" + job.Name
		cm, err := getScheduleConfigMap(cmName, job, oldCluster)
		if err != nil {
			return errors.Wrapf(err, "get schedule configmap for %s", job.Name)
		}

		scheduleMap[cmName] = schedule{job, del, cm}
	}

	for _, job := range newCluster.Spec.Backup.Schedule {
		cmName := newCluster.Name + "-" + job.Name
		cm, err := getScheduleConfigMap(cmName, job, newCluster)
		if err != nil {
			return errors.Wrapf(err, "get schedule configmap for %s", job.Name)
		}

		oldSchedule, ok := scheduleMap[cmName]
		scheduleMap[cmName] = schedule{job, create, cm}

		if ok {
			if reflect.DeepEqual(cm, oldSchedule.configMap) {
				scheduleMap[cmName] = schedule{job, keep, cm}
			} else {
				scheduleMap[cmName] = schedule{job, update, cm}
			}
		}
	}

	for name, s := range scheduleMap {
		switch s.action {
		case create:
			_, err := c.Client.CoreV1().ConfigMaps(newCluster.Namespace).Create(ctx, s.configMap, metav1.CreateOptions{})
			if err != nil {
				return errors.Wrap(err, "create config map")
			}
		case del:
			err := c.Client.CoreV1().ConfigMaps(newCluster.Namespace).Delete(ctx, name, metav1.DeleteOptions{})
			if err != nil && !kerrors.IsNotFound(err) {
				return errors.Wrapf(err, "delete config map %s", name)
			}
		case update:
			_, err := c.Client.CoreV1().ConfigMaps(newCluster.Namespace).Update(ctx, s.configMap, metav1.UpdateOptions{})
			if err != nil {
				return errors.Wrap(err, "update config map")
			}
		}
	}

	return nil
}

func getScheduleConfigMap(name string, job crv1.CronJob, newCluster *crv1.PerconaPGCluster) (*v1.ConfigMap, error) {
	storage, ok := newCluster.Spec.Backup.Storages[job.Storage]
	if !ok && job.Storage != storageLocal {
		return nil, errors.Errorf("invalid storage name '%s' in schedule '%s'", job.Storage, job.Name)
	}
	storageType := storageLocal
	if ok {
		storageType = string(storage.Type)
	}

	affinity, err := json.Marshal(newCluster.Spec.Backup.Affinity)
	if err != nil {
		return nil, errors.Wrap(err, "marshal affinity")
	}

	scheduleTemp := scheduler.ScheduleTemplate{
		Name:      job.Name,
		Created:   time.Now(),
		Schedule:  job.Schedule,
		Type:      "pgbackrest",
		Cluster:   newCluster.Name,
		Namespace: newCluster.Namespace,
		PGBackRest: scheduler.PGBackRest{
			Type:        job.Type,
			StorageType: storageType,
			Deployment:  newCluster.Name,
			Container:   "database",
			Options:     job.PGBackrestOpts,
		},
		Affinity: string(affinity),
	}
	if job.Keep > 0 {
		keepStr := strconv.FormatInt(job.Keep, 10)
		switch job.Type {
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
