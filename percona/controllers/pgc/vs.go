package pgc

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	api "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
	"github.com/percona/percona-postgresql-operator/versionserviceclient"
	"github.com/percona/percona-postgresql-operator/versionserviceclient/models"
	"github.com/percona/percona-postgresql-operator/versionserviceclient/version_service"
)

const productName = "pg-operator"

func (vs VersionServiceClient) GetExactVersion(cr *api.PerconaPGCluster, endpoint string, vm versionMeta) (DepVersion, error) {
	if strings.Contains(endpoint, "https://check.percona.com/versions") {
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
		Apply:               vm.Apply,
		BackupVersion:       &vm.BackupVersion,
		CustomResourceUID:   &vm.CRUID,
		DatabaseVersion:     &vm.PGVersion,
		HaproxyVersion:      &vm.HAProxyVersion,
		KubeVersion:         &vm.KubeVersion,
		LogCollectorVersion: &vm.LogCollectorVersion,
		NamespaceUID:        new(string),
		OperatorVersion:     vs.OpVersion,
		Platform:            &vm.Platform,
		PmmVersion:          &vm.PMMVersion,
		Product:             productName,
		ProxysqlVersion:     &vm.ProxySQLVersion,
		Context:             nil,
		HTTPClient:          &http.Client{Timeout: 10 * time.Second},
	}
	applyParams = applyParams.WithTimeout(10 * time.Second)

	resp, err := srvCl.VersionService.VersionServiceApply(applyParams)

	if err != nil {
		return DepVersion{}, err
	}

	if len(resp.Payload.Versions) == 0 {
		return DepVersion{}, fmt.Errorf("empty versions response")
	}

	backupVersion, err := getVersion(resp.Payload.Versions[0].Matrix.Backup)
	if err != nil {
		return DepVersion{}, err
	}

	pmmVersion, err := getVersion(resp.Payload.Versions[0].Matrix.Pmm)
	if err != nil {
		return DepVersion{}, err
	}

	dv := DepVersion{
		BackupImage:   resp.Payload.Versions[0].Matrix.Backup[backupVersion].ImagePath,
		BackupVersion: backupVersion,
		PMMImage:      resp.Payload.Versions[0].Matrix.Pmm[pmmVersion].ImagePath,
		PMMVersion:    pmmVersion,
	}

	return dv, nil
}

func getVersion(versions map[string]models.VersionVersion) (string, error) {
	if len(versions) != 1 {
		return "", fmt.Errorf("response has multiple or zero versions")
	}

	for k := range versions {
		return k, nil
	}
	return "", nil
}

type DepVersion struct {
	BackupImage   string `json:"backupImage,omitempty"`
	BackupVersion string `json:"backupVersion,omitempty"`
	PMMImage      string `json:"pmmImage,omitempty"`
	PMMVersion    string `json:"pmmVersion,omitempty"`
}

type VersionService interface {
	GetExactVersion(cr *api.PerconaPGCluster, endpoint string, vm versionMeta) (DepVersion, error)
}

type VersionServiceClient struct {
	OpVersion string
}

type versionMeta struct {
	Apply               string
	PGVersion           string
	KubeVersion         string
	Platform            string
	PMMVersion          string
	BackupVersion       string
	ProxySQLVersion     string
	HAProxyVersion      string
	LogCollectorVersion string
	CRUID               string
}
