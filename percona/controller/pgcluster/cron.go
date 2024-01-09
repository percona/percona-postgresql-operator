package pgcluster

import (
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/robfig/cron/v3"
)

type CronRegistry struct {
	crons      *cron.Cron
	backupJobs *sync.Map
}

// AddFuncWithSeconds does the same as cron.AddFunc but changes the schedule so that the function will run the exact second that this method is called.
func (r *CronRegistry) AddFuncWithSeconds(spec string, cmd func()) (cron.EntryID, error) {
	schedule, err := cron.ParseStandard(spec)
	if err != nil {
		return 0, errors.Wrap(err, "failed to parse cron schedule")
	}
	schedule.(*cron.SpecSchedule).Second = uint64(1 << time.Now().Second())
	id := r.crons.Schedule(schedule, cron.FuncJob(cmd))
	return id, nil
}

func NewCronRegistry() CronRegistry {
	c := CronRegistry{
		crons:      cron.New(),
		backupJobs: new(sync.Map),
	}

	c.crons.Start()

	return c
}

func (r *CronRegistry) ApplyBackupJob(name string, schedule string, cmd func()) error {
	schRaw, ok := r.backupJobs.Load(name)
	if ok {
		sch := schRaw.(BackupScheduleJob)
		if sch.schedule == schedule {
			return nil
		}
	}

	r.DeleteBackupJob(name)

	jobID, err := r.AddFuncWithSeconds(schedule, cmd)
	if err != nil {
		return errors.Wrap(err, "failed to add backup job")
	}
	r.backupJobs.Store(name, BackupScheduleJob{
		schedule: schedule,
		id:       jobID,
	})
	return nil
}

func (r *CronRegistry) DeleteBackupJob(name string) {
	if sch, ok := r.backupJobs.Load(name); ok {
		r.crons.Remove(sch.(BackupScheduleJob).id)
	}
}

type BackupScheduleJob struct {
	schedule string
	id       cron.EntryID
}
