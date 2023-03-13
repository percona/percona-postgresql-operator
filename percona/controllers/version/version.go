package version

import (
	"context"
	"os"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
	api "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
)

func TelemetryEnabled() bool {
	return os.Getenv("DISABLE_TELEMETRY") == "false"
}

func VersionUpgradeEnabled(cr *api.PerconaPGCluster) bool {
	return cr.Spec.UpgradeOptions.Apply.Lower() != api.UpgradeStrategyNever &&
		cr.Spec.UpgradeOptions.Apply.Lower() != api.UpgradeStrategyDisabled
}

func isMajorVersionUpgrade(cr *api.PerconaPGCluster, pVer string) bool {
	applySp := strings.Split(string(cr.Spec.UpgradeOptions.Apply), "-")
	pVerSp := strings.Split(string(pVer), ".")

	return len(applySp) > 1 && len(pVerSp) > 1 && applySp[0] != pVerSp[0]
}

func EnsureVersion(clientset kubeapi.Interface, cr *api.PerconaPGCluster, vs VersionService) error {
	var pVer string
	pgCluster, err := clientset.CrunchydataV1().Pgclusters(cr.Namespace).Get(context.TODO(), cr.Name, metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.Wrap(err, "get pgcluster resource")
	}

	if pgCluster != nil && err == nil {
		pVer, err = GetPostgresqlVersion(clientset, pgCluster)
		if err != nil {
			return errors.Wrap(err, "get postgrsql version")
		}
	}

	if isMajorVersionUpgrade(cr, pVer) {
		log.Errorf("%s value for spec.upgradeOptions.apply option is not supported", cr.Spec.UpgradeOptions.Apply)
		return nil
	}

	verMeta := versionMeta{
		Apply: string(cr.Spec.UpgradeOptions.Apply),
		CRUID: string(cr.GetUID()),
	}
	if len(pVer) > 0 {
		verMeta.PGVersion = strings.TrimSuffix(pVer, "\n")
	}

	var newVersion DepVersion
	if VersionUpgradeEnabled(cr) || (!VersionUpgradeEnabled(cr) && TelemetryEnabled()) {
		log.Infof("Fetching versions from %s", cr.Spec.UpgradeOptions.VersionServiceEndpoint)

		newVersion, err = vs.GetExactVersion(cr, cr.Spec.UpgradeOptions.VersionServiceEndpoint, verMeta)
		if err != nil {
			return errors.Wrap(err, "failed to check version")
		}

		if TelemetryEnabled() && cr.Spec.UpgradeOptions.VersionServiceEndpoint != api.GetDefaultVersionServiceEndpoint() {
			_, err = vs.GetExactVersion(cr, api.GetDefaultVersionServiceEndpoint(), verMeta)
			if err != nil {
				// we don't return here to not block execution just because we can't phone home
				log.WithError(err).Errorf("failed to send telemetry to %s", api.GetDefaultVersionServiceEndpoint())
			}
		}
	}

	if !VersionUpgradeEnabled(cr) {
		return nil
	}

	log.Printf(`ensured version images:
	%s
	%s
	%s
	%s
	%s
	%s
	`,
		newVersion.PostgresImage,
		newVersion.PGBadgerImage,
		newVersion.PGBouncerImage,
		newVersion.PGBackrestImage,
		newVersion.PGBackrestRepoImage,
		newVersion.PMMImage,
	)

	cr.Spec.PGPrimary.Image = newVersion.PostgresImage
	cr.Spec.PGBadger.Image = newVersion.PGBadgerImage
	cr.Spec.PGBouncer.Image = newVersion.PGBouncerImage
	cr.Spec.Backup.Image = newVersion.PGBackrestImage
	cr.Spec.Backup.BackrestRepoImage = newVersion.PGBackrestRepoImage
	cr.Spec.PMM.Image = newVersion.PMMImage

	return nil
}
