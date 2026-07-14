package runtime

import (
	"context"
	"strings"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	r "github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v2/internal/feature"
	"github.com/percona/percona-postgresql-operator/v2/percona/k8s"
)

// default refresh interval in minutes
const refreshInterval time.Duration = 60 * time.Minute

const ElectionID string = "08db3feb.percona.com"

// ClientCacheOptions returns the client.CacheOptions CreateRuntimeManager
// applies to every manager it builds. The operator does not request
// cluster-wide list/watch RBAC for clusterissuers.cert-manager.io by
// default (K8SPG-951's managed ClusterIssuer mode), so Get calls for
// ClusterIssuer must bypass the informer cache entirely and hit the API
// server directly, which only needs "get" on the one named object.
// Exported so it's directly unit-testable without constructing a manager.
func ClientCacheOptions() *client.CacheOptions {
	return &client.CacheOptions{
		DisableFor: []client.Object{&cmv1.ClusterIssuer{}},
	}
}

// CreateRuntimeManager wraps internal/controller/runtime.NewManager and modifies the given options:
//   - Fully overwrites the Cache field
//   - Sets Cache.SyncPeriod to refreshInterval const
//   - Sets Cache.DefaultNamespaces by using k8s.GetWatchNamespace() split by ","
//   - Sets BaseContext to include the provided feature gates
func CreateRuntimeManager(config *rest.Config, features feature.MutableGate, options manager.Options) (manager.Manager, error) {
	namespaces, err := k8s.GetWatchNamespace()
	if err != nil {
		return nil, err
	}

	options.Cache = cache.Options{
		SyncPeriod: new(refreshInterval),
	}
	nn := strings.Split(namespaces, ",")
	if len(nn) > 0 && nn[0] != "" {
		namespaces := make(map[string]cache.Config)
		for _, ns := range nn {
			namespaces[ns] = cache.Config{}
		}
		options.Cache.DefaultNamespaces = namespaces
	}

	options.Client.Cache = ClientCacheOptions()

	options.BaseContext = func() context.Context {
		ctx := context.Background()
		return feature.NewContext(ctx, features)
	}

	return r.NewManager(config, options)
}
