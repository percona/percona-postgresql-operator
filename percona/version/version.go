package version

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/pkg/errors"

	"github.com/percona/percona-postgresql-operator/internal/logging"
	"github.com/percona/percona-postgresql-operator/percona/version/service/client"
	"github.com/percona/percona-postgresql-operator/percona/version/service/client/version_service"
	"github.com/percona/percona-postgresql-operator/pkg/apis/pg.percona.com/v2beta1"
)

const (
	Version     = "2.1.0"
	productName = "pg-operator"
)

type DepVersion struct{}

type Meta struct {
	Apply           string
	OperatorVersion string
	PGVersion       string
	KubeVersion     string
	Platform        string
	PMMVersion      string
	BackupVersion   string
	CRUID           string
}

func EnsureVersion(ctx context.Context, cr *v2beta1.PerconaPGCluster, meta Meta) error {
	log := logging.FromContext(ctx)

	if !telemetryEnabled() {
		return nil
	}

	_, err := fetchVersions(ctx, cr, v2beta1.GetDefaultVersionServiceEndpoint(), meta)
	if err != nil {
		// we don't return here to not block execution just because we can't phone home
		log.Error(err, fmt.Sprintf("failed to send telemetry to %s", v2beta1.GetDefaultVersionServiceEndpoint()))
	}

	return nil
}

func telemetryEnabled() bool {
	value, ok := os.LookupEnv("DISABLE_TELEMETRY")
	if ok {
		return value != "true"
	}
	return true
}

func fetchVersions(ctx context.Context, cr *v2beta1.PerconaPGCluster, endpoint string, vm Meta) (DepVersion, error) {
	requestURL, err := url.Parse(endpoint)
	if err != nil {
		return DepVersion{}, errors.Wrap(err, "parse endpoint")
	}

	srvCl := client.NewHTTPClientWithConfig(nil, &client.TransportConfig{
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
		OperatorVersion:   vm.OperatorVersion,
		Platform:          &vm.Platform,
		PmmVersion:        &vm.PMMVersion,
		Product:           productName,
		Context:           ctx,
		HTTPClient:        &http.Client{Timeout: 10 * time.Second},
	}
	applyParams = applyParams.WithTimeout(10 * time.Second)

	_, err = srvCl.VersionService.VersionServiceApply(applyParams)
	if err != nil {
		return DepVersion{}, errors.Wrap(err, "version service apply")
	}

	return DepVersion{}, nil
}
