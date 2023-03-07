package version

import (
	"net/http"
	"net/url"
	"time"

	"github.com/pkg/errors"

	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
	"github.com/percona/percona-postgresql-operator/internal/util"
	"github.com/percona/percona-postgresql-operator/percona/controllers/db"
	api "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
	"github.com/percona/percona-postgresql-operator/versionserviceclient"
	"github.com/percona/percona-postgresql-operator/versionserviceclient/models"
	"github.com/percona/percona-postgresql-operator/versionserviceclient/version_service"
)

const productName = "pg-operator"

func (vs VersionServiceClient) GetExactVersion(cr *api.PerconaPGCluster, endpoint string, vm versionMeta) (DepVersion, error) {
	requestURL, err := url.Parse(endpoint)
	if err != nil {
		return DepVersion{}, errors.Wrap(err, "parse endpoint")
	}

	srvCl := versionserviceclient.NewHTTPClientWithConfig(nil, &versionserviceclient.TransportConfig{
		Host:     requestURL.Host,
		BasePath: requestURL.Path,
		Schemes:  []string{requestURL.Scheme},
	})

	applyParams := &version_service.VersionServiceApplyParams{
		Apply:             vm.Apply,
		BackupVersion:     &vm.BackupVersion,
		CustomResourceUID: &vm.CRUID,
		DatabaseVersion:   &vm.PGVersion,
		KubeVersion:       &vm.KubeVersion,
		NamespaceUID:      new(string),
		OperatorVersion:   vs.OpVersion,
		Platform:          &vm.Platform,
		PmmVersion:        &vm.PMMVersion,
		Product:           productName,
		Context:           nil,
		HTTPClient:        &http.Client{Timeout: 10 * time.Second},
	}
	applyParams = applyParams.WithTimeout(10 * time.Second)

	resp, err := srvCl.VersionService.VersionServiceApply(applyParams)
	if err != nil {
		return DepVersion{}, errors.Wrap(err, "version service apply")
	}

	if !VersionUpgradeEnabled(cr) {
		return DepVersion{}, nil
	}

	if len(resp.Payload.Versions) == 0 {
		return DepVersion{}, errors.New("empty versions response")
	}

	pgBackrestVersion, err := getVersion(resp.Payload.Versions[0].Matrix.Pgbackrest)
	if err != nil {
		return DepVersion{}, errors.Wrap(err, "get pgBackrest version")
	}
	bouncerVersion, err := getVersion(resp.Payload.Versions[0].Matrix.Pgbouncer)
	if err != nil {
		return DepVersion{}, errors.Wrap(err, "get pgBouncer version")
	}
	pmmVersion, err := getVersion(resp.Payload.Versions[0].Matrix.Pmm)
	if err != nil {
		return DepVersion{}, errors.Wrap(err, "get pmm version")
	}
	pgBackrestRepoVersion, err := getVersion(resp.Payload.Versions[0].Matrix.PgbackrestRepo)
	if err != nil {
		return DepVersion{}, errors.Wrap(err, "get pgBackrestRepo version")
	}
	pgBadgerVersion, err := getVersion(resp.Payload.Versions[0].Matrix.Pgbadger)
	if err != nil {
		return DepVersion{}, errors.Wrap(err, "get pgBadger version")
	}
	postgreSQLVersion, err := getVersion(resp.Payload.Versions[0].Matrix.Postgresql)
	if err != nil {
		return DepVersion{}, errors.Wrap(err, "get postgresql version")
	}
	dv := DepVersion{
		PostgresImage:       resp.Payload.Versions[0].Matrix.Postgresql[postgreSQLVersion].ImagePath,
		PGBackrestImage:     resp.Payload.Versions[0].Matrix.Pgbackrest[pgBackrestVersion].ImagePath,
		PGBouncerImage:      resp.Payload.Versions[0].Matrix.Pgbouncer[bouncerVersion].ImagePath,
		PGBackrestRepoImage: resp.Payload.Versions[0].Matrix.PgbackrestRepo[pgBackrestRepoVersion].ImagePath,
		PGBadgerImage:       resp.Payload.Versions[0].Matrix.Pgbadger[pgBadgerVersion].ImagePath,
		PMMImage:            resp.Payload.Versions[0].Matrix.Pmm[pmmVersion].ImagePath,
	}

	return dv, nil
}

func getVersion(versions map[string]models.VersionVersion) (string, error) {
	if len(versions) != 1 {
		return "", errors.New("response has multiple or zero versions")
	}

	for k := range versions {
		return k, nil
	}
	return "", nil
}

func GetPostgresqlVersion(clientset kubeapi.Interface, cluster *crv1.Pgcluster) (string, error) {
	pod, err := util.GetPrimaryPod(clientset, cluster)
	if err != nil {
		return "", errors.Wrap(err, "get primary pod")
	}

	sql := "SHOW server_version"

	result, err := db.ExecuteSQL(clientset, pod, cluster.Spec.Port, sql, []string{})
	if err != nil {
		return "", errors.Wrap(err, "execute sql")
	}

	return result, nil
}

type DepVersion struct {
	PGBackrestImage       string `json:"pgBackrestImage,omitempty"`
	PGBackrestVersion     string `json:"pgBackrestVersion,omitempty"`
	PGBackrestRepoImage   string `json:"pgBackrestRepoImage,omitempty"`
	PGBackrestRepoVersion string `json:"pgBackrestRepoVersion,omitempty"`
	PMMImage              string `json:"pmmImage,omitempty"`
	PMMVersion            string `json:"pmmVersion,omitempty"`
	PostgresImage         string `json:"postgresImage,omitempty"`
	PostgresVersion       string `json:"postgresVersion,omitempty"`
	PGBouncerImage        string `json:"pgBouncerImage,omitempty"`
	PGBouncerVersion      string `json:"pgBouncerVersion,omitempty"`
	PGBadgerImage         string `json:"pgBadgerImage,omitempty"`
	PGBadgerVersion       string `json:"pgBadgerVersion,omitempty"`
}

type VersionService interface {
	GetExactVersion(cr *api.PerconaPGCluster, endpoint string, vm versionMeta) (DepVersion, error)
}

type VersionServiceClient struct {
	OpVersion string
}

type versionMeta struct {
	Apply         string
	PGVersion     string
	KubeVersion   string
	Platform      string
	PMMVersion    string
	BackupVersion string
	CRUID         string
}
