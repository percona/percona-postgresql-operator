package pgc

import (
	"context"
	"sync/atomic"

	"github.com/pkg/errors"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/percona/percona-postgresql-operator/internal/config"
	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
	"github.com/percona/percona-postgresql-operator/percona/controllers/deployment"
	"github.com/percona/percona-postgresql-operator/percona/controllers/pgcluster"
	"github.com/percona/percona-postgresql-operator/percona/controllers/pgreplica"
	"github.com/percona/percona-postgresql-operator/percona/controllers/version"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
)

func (c *Controller) updateVersion(oldCluster, newCluster *crv1.PerconaPGCluster) error {
	if oldCluster.Spec.Backup.Image == newCluster.Spec.Backup.Image &&
		oldCluster.Spec.Backup.BackrestRepoImage == newCluster.Spec.Backup.BackrestRepoImage &&
		oldCluster.Spec.PGBadger.Image == newCluster.Spec.PGBadger.Image &&
		oldCluster.Spec.PGBouncer.Image == newCluster.Spec.PGBouncer.Image &&
		oldCluster.Spec.PGPrimary.Image == newCluster.Spec.PGPrimary.Image {
		return nil
	}

	return smartUpdateCluster(c.Client, newCluster, oldCluster)
}

func (c *Controller) deleteEnsureVersion(jobName string) {
	c.crons.crons.Remove(cron.EntryID(c.crons.ensureVersionJobs[jobName].ID))
	delete(c.crons.ensureVersionJobs, jobName)
}

func (c *Controller) scheduleUpdate(newCluster *crv1.PerconaPGCluster) error {
	jn := newCluster.Name + "/ensureVersion"
	schedule, ok := c.crons.ensureVersionJobs[jn]
	if newCluster.Spec.UpgradeOptions == nil {
		if ok {
			c.deleteEnsureVersion(jn)
		}
		return nil
	}
	if newCluster.Spec.UpgradeOptions.Apply.Lower() == crv1.UpgradeStrategyNever ||
		newCluster.Spec.UpgradeOptions.Apply.Lower() == crv1.UpgradeStrategyDisabled {
		if ok {
			c.deleteEnsureVersion(jn)
		}
		return nil
	}
	if ok && schedule.CronSchedule == newCluster.Spec.UpgradeOptions.Schedule {
		return nil
	}
	if ok {
		c.deleteEnsureVersion(jn)
	}

	nn := types.NamespacedName{
		Name:      newCluster.Name,
		Namespace: newCluster.Namespace,
	}
	l := c.lockers.LoadOrCreate(nn.String())

	id, err := c.crons.crons.AddFunc(newCluster.Spec.UpgradeOptions.Schedule, func() {
		l.statusMutex.Lock()
		defer l.statusMutex.Unlock()

		if !atomic.CompareAndSwapInt32(l.updateSync, updateDone, updateWait) {
			return
		}

		err := version.EnsureVersion(c.Client, newCluster, version.VersionServiceClient{
			OpVersion: newCluster.ObjectMeta.Labels["pgo-version"],
		})
		if err != nil {
			log.Errorf("schedule update: ensure version %s", err)
		}
		oldCluster, err := c.Client.CrunchydataV1().PerconaPGClusters(newCluster.Namespace).Get(context.TODO(), newCluster.Name, metav1.GetOptions{})
		if err != nil {
			log.Errorf("get perconapgcluster: %s", err)
		}
		err = smartUpdateCluster(c.Client, newCluster, oldCluster)
		if err != nil {
			log.Errorf("schedule update: smart update cluster: %s", err)
		}

		newCluster.ResourceVersion = oldCluster.ResourceVersion
		_, err = c.Client.CrunchydataV1().PerconaPGClusters(newCluster.Namespace).Update(context.TODO(), newCluster, metav1.UpdateOptions{})
		if err != nil {
			log.Errorf("update perconapgcluster: %s", err)
		}
	})
	if err != nil {
		return errors.Wrap(err, "add func to cron")
	}

	c.crons.ensureVersionJobs[jn] = Schedule{
		ID:           int(id),
		CronSchedule: newCluster.Spec.UpgradeOptions.Schedule,
	}

	return nil
}

func smartUpdateCluster(client *kubeapi.Client, newCluster, oldCluster *crv1.PerconaPGCluster) error {
	log.Printf("Smart update of cluster %s started", oldCluster.Name)
	log.Printf(`Update with images:
	%s
	%s
	%s
	%s
	%s
	`,
		newCluster.Spec.Backup.Image,
		newCluster.Spec.Backup.BackrestRepoImage,
		newCluster.Spec.PGPrimary.Image,
		newCluster.Spec.PGBadger.Image,
		newCluster.Spec.PGBouncer.Image)

	if newCluster.Spec.Backup.Image != oldCluster.Spec.Backup.Image || newCluster.Spec.Backup.BackrestRepoImage != oldCluster.Spec.Backup.BackrestRepoImage {
		err := restartBackrest(client, oldCluster, newCluster.Spec.Backup.Image, newCluster.Spec.Backup.BackrestRepoImage)
		if err != nil {
			return errors.Wrap(err, "restart backrest")
		}
	}
	oldCluster.Spec.Backup.Image = newCluster.Spec.Backup.Image
	oldCluster.Spec.Backup.BackrestRepoImage = newCluster.Spec.Backup.BackrestRepoImage

	if newCluster.Spec.PGPrimary.Image != oldCluster.Spec.PGPrimary.Image {
		err := restartCluster(client,
			oldCluster,
			newCluster.Spec.PGPrimary.Image,
			newCluster.Spec.PGBadger.Image,
			newCluster.Spec.PMM.Image,
			newCluster.Labels[config.LABEL_PGO_VERSION])
		if err != nil {
			return errors.Wrap(err, "restart cluster")
		}
	}
	oldCluster.Spec.PGPrimary.Image = newCluster.Spec.PGPrimary.Image
	oldCluster.Spec.PGBadger.Image = newCluster.Spec.PGBadger.Image

	if newCluster.Spec.PGBouncer.Image != oldCluster.Spec.PGBouncer.Image {
		err := restartBouncer(client, oldCluster, newCluster.Spec.PGBouncer.Image)
		if err != nil {
			return errors.Wrap(err, "restart bouncer")
		}
	}
	oldCluster.Spec.PGBouncer.Image = newCluster.Spec.PGBouncer.Image
	log.Println("Smart update finished")

	return nil
}

func restartCluster(client *kubeapi.Client, oldCluster *crv1.PerconaPGCluster, newPostgreSQLImage, newBadgerImage, newPMMImage, version string) error {
	newCluster := *oldCluster
	newCluster.Spec.PGPrimary.Image = newPostgreSQLImage
	newCluster.Spec.PGBadger.Image = newBadgerImage
	newCluster.Spec.PMM.Image = newPMMImage
	if newCluster.Labels == nil {
		newCluster.Labels = make(map[string]string)
	}
	newCluster.Labels[config.LABEL_PGO_VERSION] = version
	primary, err := pgcluster.IsPrimary(client, oldCluster)
	if err != nil {
		return errors.Wrap(err, "check is pgcluster primary")
	}
	if primary {
		err = pgreplica.Update(client, &newCluster, oldCluster)
		if err != nil {
			return errors.Wrap(err, "update pgreplica")
		}
		err = pgcluster.Update(client, &newCluster, oldCluster)
		if err != nil {
			return errors.Wrap(err, "update pgcluster")
		}
	} else {
		err = pgcluster.Update(client, &newCluster, oldCluster)
		if err != nil {
			return errors.Wrap(err, "update pgcluster")
		}
		err = pgreplica.Update(client, &newCluster, oldCluster)
		if err != nil {
			return errors.Wrap(err, "update pgreplica")
		}
	}
	err = deployment.Wait(client, newCluster.Name, newCluster.Namespace)
	if err != nil {
		return errors.Wrap(err, "wait deployment")
	}

	return nil
}

func restartBackrest(client *kubeapi.Client, oldCluster *crv1.PerconaPGCluster, newBackrestImage, newBackrestRepoImage string) error {
	newCluster := *oldCluster
	newCluster.Spec.Backup.Image = newBackrestImage
	newCluster.Spec.Backup.BackrestRepoImage = newBackrestRepoImage

	err := pgcluster.UpdateCR(client, &newCluster, oldCluster)
	if err != nil {
		return errors.Wrap(err, "update pgcluster cr")
	}
	err = deployment.Wait(client, newCluster.Name+"-backrest-shared-repo", newCluster.Namespace)
	if err != nil {
		return errors.Wrap(err, "wait deployment")
	}

	return nil
}

func restartBouncer(client *kubeapi.Client, oldCluster *crv1.PerconaPGCluster, newBouncerImage string) error {
	newCluster := *oldCluster
	newCluster.Spec.PGBouncer.Image = newBouncerImage

	err := pgcluster.UpdateCR(client, &newCluster, oldCluster)
	if err != nil {
		return errors.Wrap(err, "update pgcluster cr")
	}
	err = deployment.Wait(client, newCluster.Name+"-pgbouncer", newCluster.Namespace)
	if err != nil {
		return errors.Wrap(err, "wait deployment")
	}

	return nil
}
