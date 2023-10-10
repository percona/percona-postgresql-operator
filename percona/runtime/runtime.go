package runtime

import (
	"strings"
	"time"

	// "k8s.io/client-go/kubernetes/scheme"
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

	nn := strings.Split(namespaces, ",")

	options := manager.Options{
		SyncPeriod: &refreshInterval,
		Scheme:     pgoScheme,
		NewCache:   cache.MultiNamespacedCacheBuilder(nn),
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
