package runtime

import (
	"context"
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	r "github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v2/internal/feature"
	"github.com/percona/percona-postgresql-operator/v2/internal/initialize"
	"github.com/percona/percona-postgresql-operator/v2/percona/k8s"
)

// default refresh interval in minutes
const refreshInterval time.Duration = 60 * time.Minute

const ElectionID string = "08db3feb.percona.com"

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
		SyncPeriod: initialize.Pointer(refreshInterval),
	}
	nn := strings.Split(namespaces, ",")
	if len(nn) > 0 && nn[0] != "" {
		namespaces := make(map[string]cache.Config)
		for _, ns := range nn {
			namespaces[ns] = cache.Config{}
		}
		options.Cache.DefaultNamespaces = namespaces
	}

	options.BaseContext = func() context.Context {
		ctx := context.Background()
		return feature.NewContext(ctx, features)
	}

	return r.NewManager(config, options)
}
