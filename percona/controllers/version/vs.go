package version

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	api "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
	"github.com/percona/percona-postgresql-operator/versionserviceclient"
	"github.com/percona/percona-postgresql-operator/versionserviceclient/models"
	"github.com/percona/percona-postgresql-operator/versionserviceclient/version_service"
)

const productName = "postgresql-operator"

func (vs VersionServiceClient) GetExactVersion(cr *api.PerconaPGCluster, endpoint string, vm versionMeta) (DepVersion, error) {
	if len(endpoint) == 0 {
		endpoint = "https://check.percona.com"
	}
	requestURL, err := url.Parse(endpoint)
	if err != nil {
		return DepVersion{}, err
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
		return DepVersion{}, err
	}

	if len(resp.Payload.Versions) == 0 {
		return DepVersion{}, fmt.Errorf("empty versions response")
	}
	b, err := resp.Payload.Versions[0].Matrix.MarshalBinary()
	fmt.Println(string(b))
	fmt.Println("get backrest version")
	pgBackrestVersion, err := getVersion(resp.Payload.Versions[0].Matrix.Pgbackrest)
	if err != nil {
		return DepVersion{}, err
	}
	fmt.Println("get bouncer version")
	bouncerVersion, err := getVersion(resp.Payload.Versions[0].Matrix.Pgbouncer)
	if err != nil {
		return DepVersion{}, err
	}
	fmt.Println("get pmm version")
	pmmVersion, err := getVersion(resp.Payload.Versions[0].Matrix.Pmm)
	if err != nil {
		return DepVersion{}, err
	}
	fmt.Println("get backrestrepo version")
	pgBackrestRepoVersion, err := getVersion(resp.Payload.Versions[0].Matrix.PgbackrestRepo)
	if err != nil {
		return DepVersion{}, err
	}
	fmt.Println("get badger version")
	pgBadgerVersion, err := getVersion(resp.Payload.Versions[0].Matrix.Pgbadger)
	if err != nil {
		return DepVersion{}, err
	}
	fmt.Println("get psql version")
	postgreSQLVersion, err := getVersion(resp.Payload.Versions[0].Matrix.Postgresql)
	if err != nil {
		return DepVersion{}, err
	}
	dv := DepVersion{
		PostgresImage:       resp.Payload.Versions[0].Matrix.Postgresql[postgreSQLVersion].ImagePath,
		PGBackrestImage:     resp.Payload.Versions[0].Matrix.Pgbackrest[pgBackrestVersion].ImagePath,
		PGBouncerImage:      resp.Payload.Versions[0].Matrix.Pgbouncer[bouncerVersion].ImagePath,
		PGBackrestRepoImage: resp.Payload.Versions[0].Matrix.PgbackrestRepo[pgBackrestRepoVersion].ImagePath,
		PGBadgerImage:       resp.Payload.Versions[0].Matrix.Pgbadger[pgBadgerVersion].ImagePath,
		PMMImage:            resp.Payload.Versions[0].Matrix.Pmm[pmmVersion].ImagePath,
		PMMVersion:          pmmVersion,
	}

	return dv, nil
}

func getVersion(versions map[string]models.VersionVersion) (string, error) {
	//if len(versions) != 1 {
	if len(versions) == 0 {
		return "", fmt.Errorf("response has multiple or zero versions")
	}

	for k := range versions {
		return k, nil
	}
	return "", nil
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
	PGBouncerVersion      string `json:"pgBouncerImage,omitempty"`
	PGBadgerImage         string `json:"pgBadgerImage,omitempty"`
	PGBadgerVersion       string `json:"pgBadgerImage,omitempty"`
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
