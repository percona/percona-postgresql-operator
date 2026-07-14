package runtime

import (
	"testing"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/percona/percona-postgresql-operator/v2/internal/feature"
)

func TestCreateRuntimeManagerDisablesClusterIssuerCache(t *testing.T) {
	t.Setenv("WATCH_NAMESPACE", "")

	scheme := runtime.NewScheme()
	_ = cmv1.AddToScheme(scheme)

	opts := manager.Options{Scheme: scheme}
	mgr, err := CreateRuntimeManager(&rest.Config{Host: "https://127.0.0.1:1"}, feature.NewGate(), opts)
	require.NoError(t, err)
	require.NotNil(t, mgr)
}

func TestClientCacheOptionsDisablesClusterIssuer(t *testing.T) {
	opts := ClientCacheOptions()
	require.NotNil(t, opts)
	require.Len(t, opts.DisableFor, 1)
	_, ok := opts.DisableFor[0].(*cmv1.ClusterIssuer)
	assert.True(t, ok)
}
