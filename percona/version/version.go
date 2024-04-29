package version

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/pkg/errors"

	"github.com/percona/percona-postgresql-operator/percona/version/service/client"
	"github.com/percona/percona-postgresql-operator/percona/version/service/client/version_service"
	v2 "github.com/percona/percona-postgresql-operator/pkg/apis/pgv2.percona.com/v2"
)

type Meta struct {
	Apply              string
	OperatorVersion    string
	PGVersion          string
	KubeVersion        string
	Platform           string
	PMMVersion         string
	BackupVersion      string
	CRUID              string
	PMMEnabled         bool
	HelmDeployCR       bool
	HelmDeployOperator bool
	SidecarsUsed       bool
	Extensions         string
}

func EnsureVersion(ctx context.Context, meta Meta) error {
	err := fetchVersions(ctx, v2.GetDefaultVersionServiceEndpoint(), meta)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to send telemetry to %s", v2.GetDefaultVersionServiceEndpoint()))
	}

	return nil
}

func fetchVersions(ctx context.Context, endpoint string, vm Meta) error {
	requestURL, err := url.Parse(endpoint)
	if err != nil {
		return errors.Wrap(err, "parse endpoint")
	}

	srvCl := client.NewHTTPClientWithConfig(nil, &client.TransportConfig{
		Host:     requestURL.Host,
		BasePath: requestURL.Path,
		Schemes:  []string{requestURL.Scheme},
	})

	applyParams := &version_service.VersionServiceApplyParams{
		Context:            ctx,
		HTTPClient:         &http.Client{Timeout: 10 * time.Second},
		Product:            v2.ProductName,
		Apply:              vm.Apply,
		BackupVersion:      &vm.BackupVersion,
		CustomResourceUID:  &vm.CRUID,
		DatabaseVersion:    &vm.PGVersion,
		KubeVersion:        &vm.KubeVersion,
		NamespaceUID:       new(string),
		OperatorVersion:    vm.OperatorVersion,
		Platform:           &vm.Platform,
		PmmVersion:         &vm.PMMVersion,
		PmmEnabled:         &vm.PMMEnabled,
		HelmDeployCr:       &vm.HelmDeployCR,
		HelmDeployOperator: &vm.HelmDeployOperator,
		SidecarsUsed:       &vm.SidecarsUsed,
		Extensions:         &vm.Extensions,
	}
	applyParams = applyParams.WithTimeout(10 * time.Second)

	_, err = srvCl.VersionService.VersionServiceApply(applyParams)
	if err != nil {
		return errors.Wrap(err, "version service apply")
	}

	return nil
}
