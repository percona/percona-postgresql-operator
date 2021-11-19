package pgc

import (
	"context"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
	"github.com/percona/percona-postgresql-operator/percona/controllers/pgcluster"
	"github.com/percona/percona-postgresql-operator/percona/controllers/pgreplica"
	"github.com/percona/percona-postgresql-operator/percona/controllers/version"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
)

const never = "never"
const disabled = "disabled"

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
	if strings.ToLower(newCluster.Spec.UpgradeOptions.Apply) == never ||
		strings.ToLower(newCluster.Spec.UpgradeOptions.Apply) == disabled {
		if ok {
			c.deleteEnsureVersion(jn)
		}
	}
	if ok && schedule.CronSchedule == newCluster.Spec.UpgradeOptions.Schedule {
		return nil
	}
	if ok {
		c.deleteEnsureVersion(jn)
	}
	id, err := c.crons.crons.AddFunc(newCluster.Spec.UpgradeOptions.Schedule, func() {
		for i := 1; i < 30; i++ {
			time.Sleep(5 * time.Second)
			if !c.crons.inProgress {
				break
			}
		}
		c.crons.inProgress = true
		defer func() { c.crons.inProgress = false }()

		err := version.EnsureVersion(newCluster, version.VersionServiceClient{
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
		err := restartCluster(client, oldCluster, newCluster.Spec.PGPrimary.Image)
		if err != nil {
			return errors.Wrap(err, "restart cluster")
		}
	}
	oldCluster.Spec.PGPrimary.Image = newCluster.Spec.PGPrimary.Image

	if newCluster.Spec.PGBadger.Image != oldCluster.Spec.PGBadger.Image {
		err := restartBadger(client, oldCluster, newCluster.Spec.PGBadger.Image)
		if err != nil {
			return errors.Wrap(err, "restart badger")
		}
	}
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

func restartCluster(client *kubeapi.Client, oldCluster *crv1.PerconaPGCluster, newPostgreSQLImage string) error {
	newCluster := *oldCluster
	newCluster.Spec.PGPrimary.Image = newPostgreSQLImage
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
	ctx := context.TODO()
	for i := 0; i <= 30; i++ {
		time.Sleep(5 * time.Second)
		primaryDepl, err := client.AppsV1().Deployments(newCluster.Namespace).Get(ctx,
			newCluster.Name, metav1.GetOptions{})
		if err != nil && !kerrors.IsNotFound(err) {
			return errors.Wrap(err, "get pgprimary deployment")
		}
		if primaryDepl.Status.Replicas == primaryDepl.Status.AvailableReplicas {
			break
		}
	}

	return nil
}

func restartBackrest(client *kubeapi.Client, oldCluster *crv1.PerconaPGCluster, newBackrestImage, newBackrestRepoImage string) error {
	newCluster := *oldCluster
	newCluster.Spec.Backup.Image = newBackrestImage
	newCluster.Spec.Backup.BackrestRepoImage = newBackrestRepoImage

	err := pgcluster.Update(client, &newCluster, oldCluster)
	if err != nil {
		return errors.Wrap(err, "update pgcluster")
	}
	return nil
}

func restartBadger(client *kubeapi.Client, oldCluster *crv1.PerconaPGCluster, newBadgerImage string) error {
	newCluster := *oldCluster
	newCluster.Spec.PGBadger.Image = newBadgerImage

	err := pgcluster.Update(client, &newCluster, oldCluster)
	if err != nil {
		return errors.Wrap(err, "update pgcluster")
	}

	return nil
}

func restartBouncer(client *kubeapi.Client, oldCluster *crv1.PerconaPGCluster, newBouncerImage string) error {
	newCluster := *oldCluster
	newCluster.Spec.PGBouncer.Image = newBouncerImage

	err := pgcluster.Update(client, &newCluster, oldCluster)
	if err != nil {
		return errors.Wrap(err, "update pgcluster")
	}

	return nil
}
