package runtime

import (
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsServer "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	r "github.com/percona/percona-postgresql-operator/internal/controller/runtime"
)

// default refresh interval in minutes
var refreshInterval = 60 * time.Minute

// CreateRuntimeManager does the same thing as `internal/controller/runtime.CreateRuntimeManager`,
// excet it configures the manager to watch multiple namespaces.
func CreateRuntimeManager(namespaces string, config *rest.Config,
	disableMetrics bool) (manager.Manager, error) {

	options := manager.Options{
		Cache: cache.Options{
			SyncPeriod: &refreshInterval,
		},
		Scheme: r.Scheme,
	}

	nn := strings.Split(namespaces, ",")
	if len(nn) > 0 && nn[0] != "" {
		namespaces := make(map[string]cache.Config)
		for _, ns := range nn {
			namespaces[ns] = cache.Config{}
		}
		options.Cache.DefaultNamespaces = namespaces
	}

	if disableMetrics {
		options.HealthProbeBindAddress = "0"
		options.Metrics = metricsServer.Options{
			BindAddress: "0",
		}
	}

	// create controller runtime manager
	mgr, err := manager.New(config, options)
	if err != nil {
		return nil, err
	}

	return mgr, nil
}
