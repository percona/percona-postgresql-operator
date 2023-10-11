package runtime

import (
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	r "github.com/percona/percona-postgresql-operator/internal/controller/runtime"
)

// default refresh interval in minutes
var refreshInterval = 60 * time.Minute

// CreateRuntimeManager does the same thing as `internal/controller/runtime.CreateRuntimeManager`,
// excet it configures the manager to watch multiple namespaces.
func CreateRuntimeManager(namespaces string, config *rest.Config,
	disableMetrics bool) (manager.Manager, error) {

	pgoScheme, err := r.CreatePostgresOperatorScheme()
	if err != nil {
		return nil, err
	}

	options := manager.Options{
		SyncPeriod: &refreshInterval,
		Scheme:     pgoScheme,
	}

	nn := strings.Split(namespaces, ",")
	if len(nn) > 0 && nn[0] != ""{
		options.NewCache = cache.MultiNamespacedCacheBuilder(nn)
	} else {
		options.Namespace = ""
	}

	if disableMetrics {
		options.HealthProbeBindAddress = "0"
		options.MetricsBindAddress = "0"
	}

	// create controller runtime manager
	mgr, err := manager.New(config, options)
	if err != nil {
		return nil, err
	}

	return mgr, nil
}
