package runtime

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsServer "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	r "github.com/percona/percona-postgresql-operator/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/internal/feature"
)

// default refresh interval in minutes
var refreshInterval = 60 * time.Minute

const (
	electionID string = "08db3feb.percona.com"
	// DefaultMaxConcurrentReconciles is the default number of concurrent reconciles
	DefaultMaxConcurrentReconciles = 10
)

// CreateRuntimeManager does the same thing as `internal/controller/runtime.CreateRuntimeManager`,
// excet it configures the manager to watch multiple namespaces.
func CreateRuntimeManager(namespaces string, config *rest.Config, disableMetrics, disableLeaderElection bool, features feature.MutableGate) (manager.Manager, error) {

	var leaderElectionID string
	if !disableLeaderElection {
		leaderElectionID = electionID
	}

	options := manager.Options{
		Cache: cache.Options{
			SyncPeriod: &refreshInterval,
		},
		Scheme:           r.Scheme,
		LeaderElection:   !disableLeaderElection,
		LeaderElectionID: leaderElectionID,
	}

	options.BaseContext = func() context.Context {
		ctx := context.Background()
		return feature.NewContext(ctx, features)
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

	// Enable concurrent reconciles
	maxConcurrentReconciles := DefaultMaxConcurrentReconciles
	if envVal := os.Getenv("MAX_CONCURRENT_RECONCILES"); envVal != "" {
		if val, err := strconv.Atoi(envVal); err == nil && val > 0 {
			maxConcurrentReconciles = val
		}
	}
	options.Controller.MaxConcurrentReconciles = maxConcurrentReconciles

	// Create a copy of the config to avoid modifying the original
	configCopy := rest.CopyConfig(config)

	// Ensure throttling is disabled by setting a fake rate limiter
	configCopy.RateLimiter = flowcontrol.NewFakeAlwaysRateLimiter()

	// create controller runtime manager
	mgr, err := manager.New(configCopy, options)
	if err != nil {
		return nil, err
	}

	return mgr, nil
}
