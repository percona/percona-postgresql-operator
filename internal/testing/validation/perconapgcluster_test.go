package validation

import (
	"context"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/percona/percona-postgresql-operator/v3/internal/testing/require"
)

// TestPerconaPGClusterExternalDNS checks that the CRD schema itself rejects a
// malformed expose.externalDNS section, so a typo is reported at apply time
// rather than silently producing a broken DNS record.
func TestPerconaPGClusterExternalDNS(t *testing.T) {
	ctx := context.Background()
	cc := require.Kubernetes(t)
	t.Parallel()

	namespace := require.Namespace(t, cc)

	base := func(t *testing.T, expose string) *unstructured.Unstructured {
		t.Helper()

		spec := map[string]any{}
		assert.NilError(t, yaml.Unmarshal([]byte(`{
			postgresVersion: 16,
			backups: { pgbackrest: { repos: [{ name: repo1 }] } },
			instances: [{
				dataVolumeClaimSpec: {
					accessModes: [ReadWriteOnce],
					resources: { requests: { storage: 1Gi } },
				},
			}],
			expose: `+expose+`,
		}`), &spec))

		u := new(unstructured.Unstructured)
		u.SetAPIVersion("pgv2.percona.com/v2")
		u.SetKind("PerconaPGCluster")
		u.SetNamespace(namespace.Name)
		u.SetGenerateName("external-dns-")
		u.Object["spec"] = spec

		return u
	}

	t.Run("valid", func(t *testing.T) {
		cr := base(t, `{ externalDNS: { hostname: pg.example.com, ttl: 60 } }`)
		assert.NilError(t, cc.Create(ctx, cr, client.DryRunAll))
	})

	t.Run("hostname is required", func(t *testing.T) {
		cr := base(t, `{ externalDNS: { ttl: 60 } }`)
		err := cc.Create(ctx, cr, client.DryRunAll)

		assert.Assert(t, apierrors.IsInvalid(err), "got %v", err)
		assert.ErrorContains(t, err, "hostname")
	})

	t.Run("hostname must be a DNS name", func(t *testing.T) {
		for _, hostname := range []string{"'not a hostname'", "'PG.example.com'", "'-pg.example.com'", "''"} {
			cr := base(t, `{ externalDNS: { hostname: `+hostname+` } }`)
			err := cc.Create(ctx, cr, client.DryRunAll)

			assert.Assert(t, apierrors.IsInvalid(err), "hostname %s: got %v", hostname, err)
			assert.ErrorContains(t, err, "hostname")
		}
	})

	t.Run("hostname is bounded by RFC 1035", func(t *testing.T) {
		longLabel := strings.Repeat("a", 64) + ".example.com"
		// 24 labels of 10 characters, plus the 23 dots, is 263.
		longName := strings.TrimSuffix(strings.Repeat("aaaaaaaaaa.", 24), ".")

		for _, hostname := range []string{longLabel, longName} {
			cr := base(t, `{ externalDNS: { hostname: `+hostname+` } }`)
			err := cc.Create(ctx, cr, client.DryRunAll)

			assert.Assert(t, apierrors.IsInvalid(err), "hostname %s: got %v", hostname, err)
			assert.ErrorContains(t, err, "hostname")
		}
	})

	t.Run("a label of exactly 63 is accepted", func(t *testing.T) {
		cr := base(t, `{ externalDNS: { hostname: `+strings.Repeat("a", 63)+`.example.com } }`)
		assert.NilError(t, cc.Create(ctx, cr, client.DryRunAll))
	})

	t.Run("ttl must not be negative", func(t *testing.T) {
		cr := base(t, `{ externalDNS: { hostname: pg.example.com, ttl: -1 } }`)
		err := cc.Create(ctx, cr, client.DryRunAll)

		assert.Assert(t, apierrors.IsInvalid(err), "got %v", err)
		assert.ErrorContains(t, err, "ttl")
	})
}
