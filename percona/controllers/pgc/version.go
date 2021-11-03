package pgc

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	api "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
	v1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"

	"github.com/pkg/errors"
	"github.com/robfig/cron/v3"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

const never = "never"
const disabled = "disabled"

func (r *Controller) deleteEnsureVersion(jobName string) {
	r.crons.crons.Remove(cron.EntryID(r.crons.ensureVersionJobs[jobName].ID))
	delete(r.crons.ensureVersionJobs, jobName)
}

func (r *Controller) sheduleEnsurePXCVersion(cr *api.PerconaPGCluster, vs VersionService) error {
	jn := jobName(cr)
	schedule, ok := r.crons.ensureVersionJobs[jn]
	if cr.Spec.UpdateStrategy != v1.SmartUpdateStatefulSetStrategyType ||
		cr.Spec.UpgradeOptions.Schedule == "" ||
		strings.ToLower(cr.Spec.UpgradeOptions.Apply) == never ||
		strings.ToLower(cr.Spec.UpgradeOptions.Apply) == disabled {
		if ok {
			r.deleteEnsureVersion(jn)
		}
		return nil
	}

	if ok && schedule.CronSchedule == cr.Spec.UpgradeOptions.Schedule {
		return nil
	}

	//l := r.lockers.LoadOrCreate(nn.String())

	id, err := r.crons.crons.AddFunc(cr.Spec.UpgradeOptions.Schedule, func() {
		l.statusMutex.Lock()
		defer l.statusMutex.Unlock()

		if !atomic.CompareAndSwapInt32(l.updateSync, updateDone, updateWait) {
			return
		}

		localCr := &api.PerconaPGCluster{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, localCr)
		if k8serrors.IsNotFound(err) {
			logger.Info("cluster is not found, deleting the job",
				"job name", jobName, "cluster", cr.Name, "namespace", cr.Namespace)
			r.deleteEnsureVersion(jn)
			return
		}
		if err != nil {
			logger.Error(err, "failed to get CR")
			return
		}

		if localCr.Status.Status != v1.AppStateReady {
			logger.Info("cluster is not ready")
			return
		}

		_, err = localCr.CheckNSetDefaults(r.serverVersion, r.logger(cr.Name, cr.Namespace))
		if err != nil {
			logger.Error(err, "failed to set defaults for CR")
			return
		}

		err = r.ensurePXCVersion(localCr, vs)
		if err != nil {
			logger.Error(err, "failed to ensure version")
		}
	})
	if err != nil {
		return err
	}

	logger.Info("add new job", "name", jn, "schedule", cr.Spec.UpgradeOptions.Schedule)

	r.crons.ensureVersionJobs[jn] = Schedule{
		ID:           int(id),
		CronSchedule: cr.Spec.UpgradeOptions.Schedule,
	}

	return nil
}

func jobName(cr *api.PerconaPGCluster) string {
	jobName := "ensure-version"
	nn := types.NamespacedName{
		Name:      cr.Name,
		Namespace: cr.Namespace,
	}
	return fmt.Sprintf("%s/%s", jobName, nn.String())
}

func (r *Controller) ensurePXCVersion(cr *api.PerconaPGCluster, vs VersionService) error {
	if cr.Spec.UpdateStrategy != v1.SmartUpdateStatefulSetStrategyType ||
		cr.Spec.UpgradeOptions.Schedule == "" ||
		strings.ToLower(cr.Spec.UpgradeOptions.Apply) == never ||
		strings.ToLower(cr.Spec.UpgradeOptions.Apply) == disabled {
		return nil
	}
	/*
		if cr.Status.Status != v1.AppStateReady  {
			return errors.New("cluster is not ready")
		}*/

	err = r.client.Status().Update(context.Background(), cr)
	if err != nil {
		return errors.Wrap(err, "failed to update CR status")
	}

	time.Sleep(1 * time.Second)

	return nil
}
