// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package pgbouncer

import (
	"context"
	"slices"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/percona/percona-postgresql-operator/v2/internal/feature"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/pki"
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	"github.com/percona/percona-postgresql-operator/v2/internal/testing/cmp"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func TestConfigMap(t *testing.T) {
	t.Parallel()

	cluster := new(v1beta1.PostgresCluster)
	config := new(corev1.ConfigMap)

	t.Run("Disabled", func(t *testing.T) {
		// Nothing happens when PgBouncer is disabled.
		constant := config.DeepCopy()
		ConfigMap(cluster, config)
		assert.DeepEqual(t, constant, config)
	})

	cluster.Spec.Proxy = new(v1beta1.PostgresProxySpec)
	cluster.Spec.Proxy.PGBouncer = new(v1beta1.PGBouncerPodSpec)
	err := cluster.Default(context.Background(), nil)
	assert.NilError(t, err)

	ConfigMap(cluster, config)

	// The output of clusterINI should go into config.
	data := clusterINI(cluster)
	assert.DeepEqual(t, config.Data["pgbouncer.ini"], data)

	// No change when called again.
	before := config.DeepCopy()
	ConfigMap(cluster, config)
	assert.DeepEqual(t, before, config)
}

func TestConfigMapPaused(t *testing.T) {
	t.Parallel()

	newCluster := func(version string, paused bool) *v1beta1.PostgresCluster {
		cluster := new(v1beta1.PostgresCluster)
		cluster.SetLabels(map[string]string{naming.LabelVersion: version})
		cluster.Spec.Proxy = new(v1beta1.PostgresProxySpec)
		cluster.Spec.Proxy.PGBouncer = new(v1beta1.PGBouncerPodSpec)
		cluster.Spec.Proxy.PGBouncer.Paused = &paused
		assert.NilError(t, cluster.Default(context.Background(), nil))
		return cluster
	}

	t.Run("BeforeVersion", func(t *testing.T) {
		config := new(corev1.ConfigMap)
		ConfigMap(newCluster("3.0.0", true), config)

		_, ok := config.Data[pausedConfigMapKey]
		assert.Assert(t, !ok, "expected no paused marker before 3.1.0")
	})

	t.Run("Paused", func(t *testing.T) {
		config := new(corev1.ConfigMap)
		ConfigMap(newCluster("3.1.0", true), config)

		assert.Equal(t, config.Data[pausedConfigMapKey], PausedValue)
	})

	t.Run("Resumed", func(t *testing.T) {
		// The marker must not outlive the pause: a leftover key would re-pause
		// the cluster on the next PgBouncer restart.
		config := new(corev1.ConfigMap)
		ConfigMap(newCluster("3.1.0", true), config)
		assert.Equal(t, config.Data[pausedConfigMapKey], PausedValue)

		ConfigMap(newCluster("3.1.0", false), config)

		_, ok := config.Data[pausedConfigMapKey]
		assert.Assert(t, !ok, "expected paused marker to be removed on resume")
	})
}

func TestSecret(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cluster := new(v1beta1.PostgresCluster)
	service := new(corev1.Service)
	service.Namespace = "ns1"
	service.Name = "some-name"
	existing := new(corev1.Secret)
	intent := new(corev1.Secret)

	root, err := pki.NewRootCertificateAuthority()
	assert.NilError(t, err)

	t.Run("Disabled", func(t *testing.T) {
		// Nothing happens when PgBouncer is disabled.
		constant := intent.DeepCopy()
		require.NoError(t, Secret(ctx, cluster, root, existing, nil, service, intent, nil, nil))
		assert.DeepEqual(t, constant, intent)
	})

	cluster.Spec.Proxy = new(v1beta1.PostgresProxySpec)
	cluster.Spec.Proxy.PGBouncer = new(v1beta1.PGBouncerPodSpec)
	userSecret := &corev1.Secret{
		Data: map[string][]byte{
			"stats_user": []byte("stats-password"),
		},
	}
	err = cluster.Default(context.Background(), nil)
	assert.NilError(t, err)

	constant := existing.DeepCopy()
	require.NoError(t, Secret(ctx, cluster, root, existing, userSecret, service, intent, nil, nil))
	assert.DeepEqual(t, constant, existing)

	// A password should be generated.
	assert.Assert(t, len(intent.Data["pgbouncer-password"]) != 0)
	assert.Assert(t, len(intent.Data["pgbouncer-verifier"]) != 0)

	// The output of authFileContents should go into intent.
	assert.Assert(t, len(intent.Data["pgbouncer-users.txt"]) != 0)
	assert.Assert(t, strings.Contains(string(intent.Data["pgbouncer-users.txt"]),
		`"stats_user" "stats-password"`))

	// Assuming the intent is written, no change when called again.
	existing.Data = intent.Data
	before := intent.DeepCopy()
	require.NoError(t, Secret(ctx, cluster, root, existing, userSecret, service, intent, nil, nil))
	assert.DeepEqual(t, before, intent)
}

func TestSecretAdminPassword(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := new(corev1.Service)
	service.Namespace = "ns1"
	service.Name = "some-name"

	root, err := pki.NewRootCertificateAuthority()
	assert.NilError(t, err)

	newCluster := func(version string) *v1beta1.PostgresCluster {
		cluster := new(v1beta1.PostgresCluster)
		cluster.SetLabels(map[string]string{naming.LabelVersion: version})
		cluster.Spec.Proxy = new(v1beta1.PostgresProxySpec)
		cluster.Spec.Proxy.PGBouncer = new(v1beta1.PGBouncerPodSpec)
		assert.NilError(t, cluster.Default(ctx, nil))
		return cluster
	}

	t.Run("Generated", func(t *testing.T) {
		cluster := newCluster("3.1.0")
		intent := new(corev1.Secret)

		require.NoError(t, Secret(ctx, cluster, root, new(corev1.Secret), nil, service, intent, nil, nil))

		adminPassword := string(intent.Data["pgbouncer-admin-password"])
		assert.Equal(t, len(adminPassword), 32)

		// It is not the password of the "auth_user".
		assert.Assert(t, adminPassword != string(intent.Data["pgbouncer-password"]))

		// No SCRAM verifier is generated; there is no PostgreSQL role.
		_, ok := intent.Data["pgbouncer-admin-verifier"]
		assert.Assert(t, !ok)

		// It goes into the authentication file.
		assert.Assert(t, strings.Contains(string(intent.Data["pgbouncer-users.txt"]),
			`"_crunchypgbounceradmin" "`+adminPassword+`"`))
	})

	t.Run("PreservedWhenCalledAgain", func(t *testing.T) {
		cluster := newCluster("3.1.0")
		existing := new(corev1.Secret)
		intent := new(corev1.Secret)

		require.NoError(t, Secret(ctx, cluster, root, existing, nil, service, intent, nil, nil))
		assert.Assert(t, len(intent.Data["pgbouncer-admin-password"]) != 0)

		existing.Data = intent.Data
		before := intent.DeepCopy()
		require.NoError(t, Secret(ctx, cluster, root, existing, nil, service, intent, nil, nil))
		assert.DeepEqual(t, before, intent)
	})

	t.Run("BelowCRVersion", func(t *testing.T) {
		cluster := newCluster("3.0.0")
		intent := new(corev1.Secret)

		require.NoError(t, Secret(ctx, cluster, root, new(corev1.Secret), nil, service, intent, nil, nil))

		_, ok := intent.Data["pgbouncer-admin-password"]
		assert.Assert(t, !ok)
		assert.Assert(t, !strings.Contains(string(intent.Data["pgbouncer-users.txt"]),
			"_crunchypgbounceradmin"))
	})
}

func TestSecretCertManagementPolicy(t *testing.T) {
	t.Parallel()

	root, err := pki.NewRootCertificateAuthority()
	assert.NilError(t, err)

	tests := []struct {
		name          string
		policy        v1beta1.CertManagementPolicy
		customTLS     bool
		expectTLSData bool
	}{
		{
			name:          "auto generates TLS data in the operator Secret",
			policy:        v1beta1.CertManagementAuto,
			expectTLSData: true,
		},
		{
			name:          "operator provided only generates TLS data in the operator Secret",
			policy:        v1beta1.CertManagementOperatorProvidedOnly,
			expectTLSData: true,
		},
		{
			name:      "operator provided only with custom TLS does not generate TLS data",
			policy:    v1beta1.CertManagementOperatorProvidedOnly,
			customTLS: true,
		},
		{
			name:      "auto with custom TLS leaves TLS data out of the operator Secret",
			policy:    v1beta1.CertManagementAuto,
			customTLS: true,
		},
		{
			name:          "user provided only preserves TLS data",
			policy:        v1beta1.CertManagementUserProvidedOnly,
			expectTLSData: true,
		},
		{
			name:      "user provided only with custom TLS does not generate TLS data",
			policy:    v1beta1.CertManagementUserProvidedOnly,
			customTLS: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			cluster := new(v1beta1.PostgresCluster)
			cluster.Spec.Proxy = &v1beta1.PostgresProxySpec{
				PGBouncer: new(v1beta1.PGBouncerPodSpec),
			}
			cluster.Spec.TLS = &v1beta1.TLSSpec{CertManagementPolicy: tt.policy}
			if tt.customTLS {
				cluster.Spec.Proxy.PGBouncer.CustomTLSSecret = &corev1.SecretProjection{
					LocalObjectReference: corev1.LocalObjectReference{Name: "custom-pgbouncer-tls"},
				}
			}
			assert.NilError(t, cluster.Default(ctx, nil))

			existing := &corev1.Secret{Data: map[string][]byte{
				"pgbouncer-frontend.ca-roots": []byte("user-ca"),
				"pgbouncer-frontend.crt":      []byte("user-cert"),
				"pgbouncer-frontend.key":      []byte("user-key"),
			}}
			intent := new(corev1.Secret)
			service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Namespace: "ns1", Name: "some-name",
			}}

			err := Secret(ctx, cluster, root, existing, nil,
				service, intent, nil, nil)
			require.NoError(t, err)

			assert.Assert(t, len(intent.Data["pgbouncer-password"]) != 0)
			assert.Assert(t, len(intent.Data["pgbouncer-verifier"]) != 0)
			assert.Assert(t, len(intent.Data["pgbouncer-users.txt"]) != 0)

			for _, key := range []string{
				"pgbouncer-frontend.ca-roots",
				"pgbouncer-frontend.crt",
				"pgbouncer-frontend.key",
			} {
				_, found := intent.Data[key]
				assert.Equal(t, found, tt.expectTLSData, key)
			}
		})
	}
}

func TestSecretAdditionalCAs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := new(corev1.Service)
	service.Namespace = "ns1"
	service.Name = "some-name"

	root, err := pki.NewRootCertificateAuthority()
	assert.NilError(t, err)

	rootPEM, err := root.Certificate.MarshalText()
	assert.NilError(t, err)

	newCluster := func(version string) *v1beta1.PostgresCluster {
		cluster := new(v1beta1.PostgresCluster)
		cluster.SetLabels(map[string]string{naming.LabelVersion: version})
		cluster.Spec.Proxy = new(v1beta1.PostgresProxySpec)
		cluster.Spec.Proxy.PGBouncer = new(v1beta1.PGBouncerPodSpec)
		assert.NilError(t, cluster.Default(context.Background(), nil))
		return cluster
	}

	ca1 := []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n")
	// No trailing newline. Entries are re-encoded into canonical PEM, so this
	// gains one and the next entry is separated from it either way.
	ca2 := []byte("-----BEGIN CERTIFICATE-----\nBBBB\n-----END CERTIFICATE-----")
	ca2Normalized := append(append([]byte{}, ca2...), '\n')

	t.Run("AppendedToGeneratedBundle", func(t *testing.T) {
		cluster := newCluster("3.1.0")
		intent := new(corev1.Secret)

		require.NoError(t, Secret(ctx, cluster, root, new(corev1.Secret), nil, service, intent, nil, [][]byte{ca1, ca2}))

		expected := append(append([]byte{}, rootPEM...), ca1...)
		expected = append(expected, ca2Normalized...)
		assert.DeepEqual(t, intent.Data["pgbouncer-frontend.ca-roots"], expected)
	})

	t.Run("DuplicatesDropped", func(t *testing.T) {
		// The same CA reaching the bundle from two references must not be
		// written twice, or the bundle grows on every reconcile.
		cluster := newCluster("3.1.0")
		intent := new(corev1.Secret)

		require.NoError(t, Secret(ctx, cluster, root, new(corev1.Secret), nil, service, intent, nil,
			[][]byte{ca1, rootPEM, ca1}))

		expected := append(append([]byte{}, rootPEM...), ca1...)
		assert.DeepEqual(t, intent.Data["pgbouncer-frontend.ca-roots"], expected)
	})

	t.Run("SeparatorInsertedWhenMissingNewline", func(t *testing.T) {
		cluster := newCluster("3.1.0")
		intent := new(corev1.Secret)

		require.NoError(t, Secret(ctx, cluster, root, new(corev1.Secret), nil, service, intent, nil, [][]byte{ca2, ca1}))

		expected := append(append([]byte{}, rootPEM...), ca2...)
		expected = append(expected, '\n')
		expected = append(expected, ca1...)
		assert.DeepEqual(t, intent.Data["pgbouncer-frontend.ca-roots"], expected)
	})

	t.Run("AppendedToCertManagerBundle", func(t *testing.T) {
		cluster := newCluster("3.1.0")
		intent := new(corev1.Secret)
		frontend := &corev1.Secret{Data: map[string][]byte{
			corev1.TLSCertKey:       []byte("tls-cert"),
			corev1.TLSPrivateKeyKey: []byte("tls-key"),
		}}

		require.NoError(t, Secret(ctx, cluster, root, new(corev1.Secret), nil, service, intent, frontend, [][]byte{ca1}))

		expected := append(append([]byte{}, rootPEM...), ca1...)
		assert.DeepEqual(t, intent.Data["pgbouncer-frontend.ca-roots"], expected)
	})

	t.Run("CustomTLSSecret", func(t *testing.T) {
		// In manual mode nothing generates the bundle; the key holds
		// exactly the CAs the caller resolved.
		cluster := newCluster("3.1.0")
		cluster.Spec.Proxy.PGBouncer.CustomTLSSecret = &corev1.SecretProjection{
			LocalObjectReference: corev1.LocalObjectReference{Name: "tls-name"},
		}
		intent := new(corev1.Secret)

		require.NoError(t, Secret(ctx, cluster, root, new(corev1.Secret), nil, service, intent, nil, [][]byte{ca1, ca2}))

		expected := append(append([]byte{}, ca1...), ca2Normalized...)
		assert.DeepEqual(t, intent.Data["pgbouncer-frontend.ca-roots"], expected)
	})

	t.Run("CustomTLSSecretWithoutCAs", func(t *testing.T) {
		cluster := newCluster("3.1.0")
		cluster.Spec.Proxy.PGBouncer.CustomTLSSecret = &corev1.SecretProjection{
			LocalObjectReference: corev1.LocalObjectReference{Name: "tls-name"},
		}
		intent := new(corev1.Secret)

		require.NoError(t, Secret(ctx, cluster, root, new(corev1.Secret), nil, service, intent, nil, nil))

		_, ok := intent.Data["pgbouncer-frontend.ca-roots"]
		assert.Assert(t, !ok)
	})

	t.Run("NoChangeWhenCalledAgain", func(t *testing.T) {
		cluster := newCluster("3.1.0")
		existing := new(corev1.Secret)
		intent := new(corev1.Secret)

		require.NoError(t, Secret(ctx, cluster, root, existing, nil, service, intent, nil, [][]byte{ca1, ca2}))

		existing.Data = intent.Data
		before := intent.DeepCopy()
		require.NoError(t, Secret(ctx, cluster, root, existing, nil, service, intent, nil, [][]byte{ca1, ca2}))
		assert.DeepEqual(t, before, intent)
	})
}

func TestPod(t *testing.T) {
	t.Parallel()

	features := feature.NewGate()
	ctx := feature.NewContext(context.Background(), features)

	cluster := new(v1beta1.PostgresCluster)
	configMap := new(corev1.ConfigMap)
	primaryCertificate := new(corev1.SecretProjection)
	secret := new(corev1.Secret)
	pod := new(corev1.PodSpec)

	cluster.SetLabels(map[string]string{
		naming.LabelVersion: "2.5.0",
	})

	call := func() { Pod(ctx, cluster, configMap, primaryCertificate, secret, pod, "") }

	t.Run("Disabled", func(t *testing.T) {
		before := pod.DeepCopy()
		call()

		// No change when PgBouncer is not requested in the spec.
		assert.DeepEqual(t, before, pod)
	})

	t.Run("Defaults", func(t *testing.T) {
		cluster.Spec.Proxy = new(v1beta1.PostgresProxySpec)
		cluster.Spec.Proxy.PGBouncer = new(v1beta1.PGBouncerPodSpec)
		err := cluster.Default(context.Background(), nil)
		assert.NilError(t, err)

		call()

		assert.Assert(t, cmp.MarshalMatches(pod, `
containers:
- command:
  - pgbouncer
  - /etc/pgbouncer/~postgres-operator.ini
  name: pgbouncer
  ports:
  - containerPort: 5432
    name: pgbouncer
    protocol: TCP
  resources: {}
  securityContext:
    allowPrivilegeEscalation: false
    capabilities:
      drop:
      - ALL
    privileged: false
    readOnlyRootFilesystem: true
    runAsNonRoot: true
    seccompProfile:
      type: RuntimeDefault
  volumeMounts:
  - mountPath: /etc/pgbouncer
    name: pgbouncer-config
    readOnly: true
- command:
  - bash
  - -ceu
  - --
  - |-
    monitor() {
    exec {fd}<> <(:||:)
    while read -r -t 5 -u "${fd}" ||:; do
      if [[ "${directory}" -nt "/proc/self/fd/${fd}" ]] && pkill -HUP --exact pgbouncer
      then
        exec {fd}>&- && exec {fd}<> <(:||:)
        stat --format='Loaded configuration dated %y' "${directory}"
      fi
    done
    }; export directory="$1"; export -f monitor; exec -a "$0" bash -ceu monitor
  - pgbouncer-config
  - /etc/pgbouncer
  name: pgbouncer-config
  resources: {}
  securityContext:
    allowPrivilegeEscalation: false
    capabilities:
      drop:
      - ALL
    privileged: false
    readOnlyRootFilesystem: true
    runAsNonRoot: true
    seccompProfile:
      type: RuntimeDefault
  volumeMounts:
  - mountPath: /etc/pgbouncer
    name: pgbouncer-config
    readOnly: true
volumes:
- name: pgbouncer-config
  projected:
    sources:
    - configMap:
        items:
        - key: pgbouncer-empty
          path: pgbouncer.ini
    - configMap:
        items:
        - key: pgbouncer.ini
          path: ~postgres-operator.ini
    - secret:
        items:
        - key: pgbouncer-users.txt
          path: ~postgres-operator/users.txt
    - secret:
        items:
        - key: pgbouncer-frontend.ca-roots
          path: ~postgres-operator/frontend-ca.crt
        - key: pgbouncer-frontend.key
          path: ~postgres-operator/frontend-tls.key
        - key: pgbouncer-frontend.crt
          path: ~postgres-operator/frontend-tls.crt
    - secret:
        items:
        - key: ca.crt
          path: ~postgres-operator/backend-ca.crt
		`))

		// No change when called again.
		before := pod.DeepCopy()
		call()
		assert.DeepEqual(t, before, pod)
	})

	// A spec that asks for additional CAs does not always produce a merged
	// bundle: userProvidedOnly drops the cluster-wide list, and a referenced
	// Secret that does not exist yet is skipped. When the key was not written,
	// the custom Secret has to keep supplying ca.crt - projecting a missing key
	// leaves the Pod stuck with an unmountable volume.
	t.Run("CustomTLSSecretKeepsItsAuthorityWhenNoBundleWasWritten", func(t *testing.T) {
		// Self-contained: the subtests above share a mutable fixture, and this
		// one must hold whether or not they ran.
		cluster := new(v1beta1.PostgresCluster)
		cluster.SetLabels(map[string]string{naming.LabelVersion: "2.5.0"})
		cluster.Spec.Proxy = new(v1beta1.PostgresProxySpec)
		cluster.Spec.Proxy.PGBouncer = new(v1beta1.PGBouncerPodSpec)
		assert.NilError(t, cluster.Default(ctx, nil))

		cluster.Spec.Proxy.PGBouncer.CustomTLSSecret = &corev1.SecretProjection{
			LocalObjectReference: corev1.LocalObjectReference{Name: "tls-name"},
			Items: []corev1.KeyToPath{
				{Key: "k1", Path: "tls.crt"},
				{Key: "k2", Path: "tls.key"},
				{Key: "k3", Path: "ca.crt"},
			},
		}
		cluster.Spec.TLS = &v1beta1.TLSSpec{
			CertManagementPolicy: v1beta1.CertManagementUserProvidedOnly,
			AdditionalTrustedCAs: []corev1.LocalObjectReference{{Name: "external-ca"}},
		}

		pod := new(corev1.PodSpec)
		Pod(ctx, cluster, configMap, primaryCertificate, new(corev1.Secret), pod, "")

		var projected []string
		for _, volume := range pod.Volumes {
			if volume.Name != "pgbouncer-config" || volume.Projected == nil {
				continue
			}
			for _, source := range volume.Projected.Sources {
				if source.Secret != nil {
					for _, item := range source.Secret.Items {
						projected = append(projected, source.Secret.Name+"/"+item.Key)
					}
				}
			}
		}

		assert.Assert(t, len(projected) > 0, "no Secret sources were projected")
		assert.Assert(t, !slices.Contains(projected, "/"+CertFrontendAuthoritySecretKey),
			"projected a merged bundle that was never written: %v", projected)
		assert.Assert(t, slices.Contains(projected, "tls-name/k3"),
			"the custom Secret must keep supplying ca.crt: %v", projected)
	})

	// K8SPG-952: in manual TLS mode with additional CAs, the frontend
	// authority is mounted from the operator Secret where the merged CA
	// bundle is stored; tls.crt/tls.key still come from the custom Secret.
	t.Run("CustomTLSSecretWithAdditionalTrustedCAs", func(t *testing.T) {
		cluster := cluster.DeepCopy()
		cluster.Spec.Proxy.PGBouncer.CustomTLSSecret = &corev1.SecretProjection{
			LocalObjectReference: corev1.LocalObjectReference{Name: "tls-name"},
			Items: []corev1.KeyToPath{
				{Key: "k1", Path: "tls.crt"},
				{Key: "k2", Path: "tls.key"},
			},
		}
		cluster.Spec.Proxy.PGBouncer.AdditionalTrustedCAs = []corev1.LocalObjectReference{
			{Name: "external-ca"},
		}

		// The authority is mounted from the operator Secret only because
		// Secret() wrote the merged bundle there. Projecting a key that was
		// never written would leave the Pod unable to mount its volume, so the
		// mount follows the Secret rather than the spec.
		secret := &corev1.Secret{Data: map[string][]byte{
			CertFrontendAuthoritySecretKey: []byte("merged-ca-bundle"),
		}}

		pod := new(corev1.PodSpec)
		Pod(ctx, cluster, configMap, primaryCertificate, secret, pod, "")

		assert.Assert(t, cmp.MarshalMatches(pod.Volumes, `
- name: pgbouncer-config
  projected:
    sources:
    - configMap:
        items:
        - key: pgbouncer-empty
          path: pgbouncer.ini
    - configMap:
        items:
        - key: pgbouncer.ini
          path: ~postgres-operator.ini
    - secret:
        items:
        - key: pgbouncer-users.txt
          path: ~postgres-operator/users.txt
    - secret:
        items:
        - key: k1
          path: ~postgres-operator/frontend-tls.crt
        - key: k2
          path: ~postgres-operator/frontend-tls.key
        name: tls-name
    - secret:
        items:
        - key: pgbouncer-frontend.ca-roots
          path: ~postgres-operator/frontend-ca.crt
    - secret:
        items:
        - key: ca.crt
          path: ~postgres-operator/backend-ca.crt
		`))
	})

	t.Run("Customizations", func(t *testing.T) {
		cluster.Spec.ImagePullPolicy = corev1.PullAlways
		cluster.Spec.Proxy.PGBouncer.Image = "image-town"
		cluster.Spec.Proxy.PGBouncer.Resources.Requests = corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("100m"),
		}
		cluster.Spec.Proxy.PGBouncer.CustomTLSSecret = &corev1.SecretProjection{
			LocalObjectReference: corev1.LocalObjectReference{Name: "tls-name"},
			Items: []corev1.KeyToPath{
				{Key: "k1", Path: "tls.crt"},
				{Key: "k2", Path: "tls.key"},
			},
		}

		call()

		assert.Assert(t, cmp.MarshalMatches(pod, `
containers:
- command:
  - pgbouncer
  - /etc/pgbouncer/~postgres-operator.ini
  image: image-town
  imagePullPolicy: Always
  name: pgbouncer
  ports:
  - containerPort: 5432
    name: pgbouncer
    protocol: TCP
  resources:
    requests:
      cpu: 100m
  securityContext:
    allowPrivilegeEscalation: false
    capabilities:
      drop:
      - ALL
    privileged: false
    readOnlyRootFilesystem: true
    runAsNonRoot: true
    seccompProfile:
      type: RuntimeDefault
  volumeMounts:
  - mountPath: /etc/pgbouncer
    name: pgbouncer-config
    readOnly: true
- command:
  - bash
  - -ceu
  - --
  - |-
    monitor() {
    exec {fd}<> <(:||:)
    while read -r -t 5 -u "${fd}" ||:; do
      if [[ "${directory}" -nt "/proc/self/fd/${fd}" ]] && pkill -HUP --exact pgbouncer
      then
        exec {fd}>&- && exec {fd}<> <(:||:)
        stat --format='Loaded configuration dated %y' "${directory}"
      fi
    done
    }; export directory="$1"; export -f monitor; exec -a "$0" bash -ceu monitor
  - pgbouncer-config
  - /etc/pgbouncer
  image: image-town
  imagePullPolicy: Always
  name: pgbouncer-config
  resources:
    limits:
      cpu: 5m
      memory: 16Mi
  securityContext:
    allowPrivilegeEscalation: false
    capabilities:
      drop:
      - ALL
    privileged: false
    readOnlyRootFilesystem: true
    runAsNonRoot: true
    seccompProfile:
      type: RuntimeDefault
  volumeMounts:
  - mountPath: /etc/pgbouncer
    name: pgbouncer-config
    readOnly: true
volumes:
- name: pgbouncer-config
  projected:
    sources:
    - configMap:
        items:
        - key: pgbouncer-empty
          path: pgbouncer.ini
    - configMap:
        items:
        - key: pgbouncer.ini
          path: ~postgres-operator.ini
    - secret:
        items:
        - key: pgbouncer-users.txt
          path: ~postgres-operator/users.txt
    - secret:
        items:
        - key: k1
          path: ~postgres-operator/frontend-tls.crt
        - key: k2
          path: ~postgres-operator/frontend-tls.key
        name: tls-name
    - secret:
        items:
        - key: ca.crt
          path: ~postgres-operator/backend-ca.crt
			`))
	})

	t.Run("Sidecar customization", func(t *testing.T) {
		cluster.Spec.Proxy.PGBouncer.Sidecars = &v1beta1.PGBouncerSidecars{
			PGBouncerConfig: &v1beta1.Sidecar{
				Resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("200m"),
					},
				},
			},
		}

		call()

		assert.Assert(t, cmp.MarshalMatches(pod, `
containers:
- command:
  - pgbouncer
  - /etc/pgbouncer/~postgres-operator.ini
  image: image-town
  imagePullPolicy: Always
  name: pgbouncer
  ports:
  - containerPort: 5432
    name: pgbouncer
    protocol: TCP
  resources:
    requests:
      cpu: 100m
  securityContext:
    allowPrivilegeEscalation: false
    capabilities:
      drop:
      - ALL
    privileged: false
    readOnlyRootFilesystem: true
    runAsNonRoot: true
    seccompProfile:
      type: RuntimeDefault
  volumeMounts:
  - mountPath: /etc/pgbouncer
    name: pgbouncer-config
    readOnly: true
- command:
  - bash
  - -ceu
  - --
  - |-
    monitor() {
    exec {fd}<> <(:||:)
    while read -r -t 5 -u "${fd}" ||:; do
      if [[ "${directory}" -nt "/proc/self/fd/${fd}" ]] && pkill -HUP --exact pgbouncer
      then
        exec {fd}>&- && exec {fd}<> <(:||:)
        stat --format='Loaded configuration dated %y' "${directory}"
      fi
    done
    }; export directory="$1"; export -f monitor; exec -a "$0" bash -ceu monitor
  - pgbouncer-config
  - /etc/pgbouncer
  image: image-town
  imagePullPolicy: Always
  name: pgbouncer-config
  resources:
    requests:
      cpu: 200m
  securityContext:
    allowPrivilegeEscalation: false
    capabilities:
      drop:
      - ALL
    privileged: false
    readOnlyRootFilesystem: true
    runAsNonRoot: true
    seccompProfile:
      type: RuntimeDefault
  volumeMounts:
  - mountPath: /etc/pgbouncer
    name: pgbouncer-config
    readOnly: true
volumes:
- name: pgbouncer-config
  projected:
    sources:
    - configMap:
        items:
        - key: pgbouncer-empty
          path: pgbouncer.ini
    - configMap:
        items:
        - key: pgbouncer.ini
          path: ~postgres-operator.ini
    - secret:
        items:
        - key: pgbouncer-users.txt
          path: ~postgres-operator/users.txt
    - secret:
        items:
        - key: k1
          path: ~postgres-operator/frontend-tls.crt
        - key: k2
          path: ~postgres-operator/frontend-tls.key
        name: tls-name
    - secret:
        items:
        - key: ca.crt
          path: ~postgres-operator/backend-ca.crt
		`))
	})

	t.Run("WithCustomSidecarContainer", func(t *testing.T) {
		cluster.Spec.Proxy.PGBouncer.Containers = []corev1.Container{
			{Name: "customsidecar1"},
		}

		t.Run("SidecarNotEnabled", func(t *testing.T) {
			call()
			assert.Equal(t, len(pod.Containers), 2, "expected 2 containers in Pod, got %d", len(pod.Containers))
		})

		t.Run("SidecarEnabled", func(t *testing.T) {
			assert.NilError(t, features.SetFromMap(map[string]bool{
				feature.PGBouncerSidecars: true,
			}))
			call()

			assert.Equal(t, len(pod.Containers), 3, "expected 3 containers in Pod, got %d", len(pod.Containers))

			var found bool
			for i := range pod.Containers {
				if pod.Containers[i].Name == "customsidecar1" {
					found = true
					break
				}
			}
			assert.Assert(t, found, "expected custom sidecar 'customsidecar1', but container not found")
		})
	})

	// The startup script writes its log to a writable volume, which is only
	// mounted for clusters at or above the version that introduced it.
	t.Run("LogVolume", func(t *testing.T) {
		for _, tt := range []struct {
			version string
			expect  bool
		}{
			{version: "3.0.0", expect: false},
			{version: "3.1.0", expect: true},
		} {
			t.Run(tt.version, func(t *testing.T) {
				cluster := new(v1beta1.PostgresCluster)
				cluster.Spec.Proxy = new(v1beta1.PostgresProxySpec)
				cluster.Spec.Proxy.PGBouncer = new(v1beta1.PGBouncerPodSpec)
				cluster.SetLabels(map[string]string{naming.LabelVersion: tt.version})
				assert.NilError(t, cluster.Default(context.Background(), nil))

				pod := new(corev1.PodSpec)
				Pod(ctx, cluster, configMap, primaryCertificate, secret, pod, "")
				assert.Equal(t, len(pod.Containers), 2)

				var volume *corev1.Volume
				for i := range pod.Volumes {
					if pod.Volumes[i].Name == logVolumeName {
						volume = &pod.Volumes[i]
					}
				}

				var mount *corev1.VolumeMount
				for i := range pod.Containers[0].VolumeMounts {
					if pod.Containers[0].VolumeMounts[i].Name == logVolumeName {
						mount = &pod.Containers[0].VolumeMounts[i]
					}
				}

				if !tt.expect {
					assert.Assert(t, volume == nil)
					assert.Assert(t, mount == nil)
					return
				}

				assert.Assert(t, volume != nil)
				assert.Assert(t, cmp.MarshalMatches(volume, `
emptyDir: {}
name: pgbouncer-logs
				`))

				assert.Assert(t, mount != nil)
				assert.Assert(t, cmp.MarshalMatches(mount, `
mountPath: /var/logs
name: pgbouncer-logs
				`))

				// Only the PgBouncer container writes the startup log.
				for _, m := range pod.Containers[1].VolumeMounts {
					assert.Assert(t, m.Name != logVolumeName)
				}
			})
		}
	})

	// The startup probe re-applies the pause across restarts. It is attached
	// regardless of whether the cluster is currently paused: the binary exits
	// immediately when the paused marker is absent.
	t.Run("StartupProbe", func(t *testing.T) {
		for _, tt := range []struct {
			version string
			expect  bool
		}{
			{version: "3.0.0", expect: false},
			{version: "3.1.0", expect: true},
		} {
			t.Run(tt.version, func(t *testing.T) {
				cluster := new(v1beta1.PostgresCluster)
				cluster.Spec.Proxy = new(v1beta1.PostgresProxySpec)
				cluster.Spec.Proxy.PGBouncer = new(v1beta1.PGBouncerPodSpec)
				cluster.SetLabels(map[string]string{naming.LabelVersion: tt.version})
				assert.NilError(t, cluster.Default(context.Background(), nil))

				pod := new(corev1.PodSpec)
				Pod(ctx, cluster, configMap, primaryCertificate, secret, pod, "")
				assert.Equal(t, len(pod.Containers), 2)

				if !tt.expect {
					assert.Assert(t, pod.Containers[0].StartupProbe == nil)
					return
				}

				assert.Assert(t, cmp.MarshalMatches(pod.Containers[0].StartupProbe, `
exec:
  command:
  - /opt/crunchy/bin/pgbouncer-startup
failureThreshold: 3
periodSeconds: 10
timeoutSeconds: 35
				`))

				// Only the PgBouncer container is held back by the probe.
				assert.Assert(t, pod.Containers[1].StartupProbe == nil)
			})
		}
	})

	t.Run("InitContainer", func(t *testing.T) {
		for _, tt := range []struct {
			version string
			expect  bool
		}{
			{version: "3.0.0", expect: false},
			{version: "3.1.0", expect: true},
		} {
			t.Run(tt.version, func(t *testing.T) {
				cluster := new(v1beta1.PostgresCluster)
				cluster.Spec.Proxy = new(v1beta1.PostgresProxySpec)
				cluster.Spec.Proxy.PGBouncer = new(v1beta1.PGBouncerPodSpec)
				cluster.SetLabels(map[string]string{naming.LabelVersion: tt.version})
				assert.NilError(t, cluster.Default(context.Background(), nil))

				pod := new(corev1.PodSpec)
				Pod(ctx, cluster, configMap, primaryCertificate, secret, pod, "some/init:image")

				var volume *corev1.Volume
				for i := range pod.Volumes {
					if pod.Volumes[i].Name == pNaming.CrunchyBinVolumeName {
						volume = &pod.Volumes[i]
					}
				}

				var mount *corev1.VolumeMount
				for i := range pod.Containers[0].VolumeMounts {
					if pod.Containers[0].VolumeMounts[i].Name == pNaming.CrunchyBinVolumeName {
						mount = &pod.Containers[0].VolumeMounts[i]
					}
				}

				if !tt.expect {
					assert.Equal(t, len(pod.InitContainers), 0)
					assert.Assert(t, volume == nil)
					assert.Assert(t, mount == nil)
					return
				}

				assert.Equal(t, len(pod.InitContainers), 1)
				assert.Assert(t, cmp.MarshalMatches(pod.InitContainers[0], `
command:
- /usr/local/bin/init-entrypoint.sh
image: some/init:image
name: pgbouncer-init
resources: {}
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
    - ALL
  privileged: false
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault
terminationMessagePath: /dev/termination-log
terminationMessagePolicy: File
volumeMounts:
- mountPath: /opt/crunchy
  name: crunchy-bin
				`))

				assert.Assert(t, volume != nil)
				assert.Assert(t, cmp.MarshalMatches(volume, `
emptyDir: {}
name: crunchy-bin
				`))

				assert.Assert(t, mount != nil)
				assert.Assert(t, cmp.MarshalMatches(mount, `
mountPath: /opt/crunchy
name: crunchy-bin
				`))

				// Only the PgBouncer container runs the installed binaries.
				for _, m := range pod.Containers[1].VolumeMounts {
					assert.Assert(t, m.Name != pNaming.CrunchyBinVolumeName)
				}
			})
		}
	})
}

func TestPostgreSQL(t *testing.T) {
	t.Parallel()

	cluster := new(v1beta1.PostgresCluster)
	hbas := new(postgres.HBAs)

	t.Run("Disabled", func(t *testing.T) {
		PostgreSQL(cluster, hbas)

		// No change when PgBouncer is not requested in the spec.
		assert.DeepEqual(t, hbas, new(postgres.HBAs))
	})

	t.Run("Enabled", func(t *testing.T) {
		cluster.Spec.Proxy = new(v1beta1.PostgresProxySpec)
		cluster.Spec.Proxy.PGBouncer = new(v1beta1.PGBouncerPodSpec)
		err := cluster.Default(context.Background(), nil)
		assert.NilError(t, err)

		PostgreSQL(cluster, hbas)

		assert.DeepEqual(t, hbas,
			&postgres.HBAs{
				Mandatory: postgresqlHBAs(),
			},
			// postgres.HostBasedAuthentication has unexported fields. Call String() to compare.
			gocmp.Transformer("", (*postgres.HostBasedAuthentication).String))
	})
}

// frontendAuthorityCert is the third of the operator's three CA resolvers, and
// the only one without direct coverage. spec.tls.additionalTrustedCAs exists
// because an issuer may return no ca.crt at all, so the anchor arriving from
// that field alone has to be enough.
func TestFrontendAuthorityCert(t *testing.T) {
	t.Parallel()

	caPEM := func(t *testing.T) []byte {
		t.Helper()
		root, err := pki.NewRootCertificateAuthority()
		assert.NilError(t, err, "bug in test")
		text, err := root.Certificate.MarshalText()
		assert.NilError(t, err, "bug in test")
		return text
	}
	secretWithCA := func(ca []byte) *corev1.Secret {
		return &corev1.Secret{Data: map[string][]byte{tlsAuthoritySecretKey: ca}}
	}
	certCount := func(b []byte) int {
		return strings.Count(string(b), "-----BEGIN CERTIFICATE-----")
	}

	t.Run("PrefersTheInternalRoot", func(t *testing.T) {
		root, err := pki.NewRootCertificateAuthority()
		assert.NilError(t, err)
		want, err := root.Certificate.MarshalText()
		assert.NilError(t, err)

		got, err := frontendAuthorityCert(root, new(corev1.Secret), nil)
		assert.NilError(t, err)
		assert.DeepEqual(t, want, got)
	})

	t.Run("UsesTheIssuedCA", func(t *testing.T) {
		issued := caPEM(t)

		got, err := frontendAuthorityCert(nil, secretWithCA(issued), nil)
		assert.NilError(t, err)
		assert.DeepEqual(t, issued, got)
	})

	t.Run("AdditionalCAsAloneAreEnough", func(t *testing.T) {
		// An ACME issuer writes only tls.crt and tls.key. Without this the
		// frontend bundle would be empty and reconciliation would fail.
		extra := caPEM(t)

		got, err := frontendAuthorityCert(nil, new(corev1.Secret), [][]byte{extra})
		assert.NilError(t, err)
		assert.DeepEqual(t, extra, got)
	})

	t.Run("MergesIssuedAndAdditionalCAs", func(t *testing.T) {
		issued, extra := caPEM(t), caPEM(t)

		got, err := frontendAuthorityCert(nil, secretWithCA(issued), [][]byte{extra})
		assert.NilError(t, err)
		assert.Equal(t, certCount(got), 2)
		assert.DeepEqual(t, got, append(append([]byte{}, issued...), extra...))
	})

	t.Run("DuplicatesAreNotRepeated", func(t *testing.T) {
		// Naming the issuer's own CA in additionalTrustedCAs must not grow the
		// bundle on every reconcile.
		issued := caPEM(t)

		got, err := frontendAuthorityCert(nil, secretWithCA(issued), [][]byte{issued})
		assert.NilError(t, err)
		assert.DeepEqual(t, issued, got)
	})

	t.Run("ErrorsWhenNothingSuppliesAnAnchor", func(t *testing.T) {
		_, err := frontendAuthorityCert(nil, new(corev1.Secret), nil)
		assert.ErrorContains(t, err, "additionalTrustedCAs")
	})
}
