package version

import (
	api "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
	"github.com/pkg/errors"
)

func EnsureVersion(cr *api.PerconaPGCluster, vs VersionService) error {
	newVersion, err := vs.GetExactVersion(cr, cr.Spec.UpgradeOptions.VersionServiceEndpoint, versionMeta{
		Apply: cr.Spec.UpgradeOptions.Apply,
		CRUID: string(cr.GetUID()),
	})
	if err != nil {
		return errors.Wrap(err, "failed to check version")
	}
	cr.Spec.PGPrimary.Image = newVersion.PostgresImage
	cr.Spec.PGBadger.Image = newVersion.PGBadgerImage
	cr.Spec.PGBouncer.Image = newVersion.PGBouncerImage
	cr.Spec.Backup.Image = newVersion.PGBackrestImage
	cr.Spec.Backup.BackrestRepoImage = newVersion.PGBackrestRepoImage
	cr.Spec.PMM.Image = newVersion.PMMImage

	return nil
}
