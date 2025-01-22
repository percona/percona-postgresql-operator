package runtime

import (
	"context"
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsServer "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	r "github.com/percona/percona-postgresql-operator/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/internal/feature"
)

// default refresh interval in minutes
var refreshInterval = 60 * time.Minute

const electionID string = "08db3feb.percona.com"

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

	// create controller runtime manager
	mgr, err := manager.New(config, options)
	if err != nil {
		return nil, err
	}

	return mgr, nil
}
