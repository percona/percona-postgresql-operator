package version

import (
	"context"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
	api "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
)

func EnsureVersion(clientset kubeapi.Interface, cr *api.PerconaPGCluster, vs VersionService) error {
	if cr.Spec.UpgradeOptions == nil ||
		cr.Spec.UpgradeOptions.Apply.Lower() == api.UpgradeStrategyNever ||
		cr.Spec.UpgradeOptions.Apply.Lower() == api.UpgradeStrategyDisabled {
		return nil
	}

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
	applySp := strings.Split(string(cr.Spec.UpgradeOptions.Apply), "-")
	pVerSp := strings.Split(string(pVer), ".")
	if len(applySp) > 1 && len(pVerSp) > 1 && applySp[0] != pVerSp[0] {
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
	newVersion, err := vs.GetExactVersion(cr, cr.Spec.UpgradeOptions.VersionServiceEndpoint, verMeta)
	if err != nil {
		return errors.Wrap(err, "failed to check version")
	}
	log.Printf(`ensured version images:
	%s
	%s
	%s
	%s
	%s
	%s
	`, newVersion.PostgresImage, newVersion.PGBadgerImage, newVersion.PGBouncerImage, newVersion.PGBackrestImage, newVersion.PGBackrestRepoImage, newVersion.PMMImage)

	cr.Spec.PGPrimary.Image = newVersion.PostgresImage
	cr.Spec.PGBadger.Image = newVersion.PGBadgerImage
	cr.Spec.PGBouncer.Image = newVersion.PGBouncerImage
	cr.Spec.Backup.Image = newVersion.PGBackrestImage
	cr.Spec.Backup.BackrestRepoImage = newVersion.PGBackrestRepoImage
	cr.Spec.PMM.Image = newVersion.PMMImage

	return nil
}
