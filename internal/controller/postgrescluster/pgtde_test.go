// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package postgrescluster

import (
	"context"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	"gotest.tools/v3/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/pgtde"
	"github.com/percona/percona-postgresql-operator/v2/internal/pki"
	"github.com/percona/percona-postgresql-operator/v2/internal/testing/events"
	"github.com/percona/percona-postgresql-operator/v2/internal/testing/require"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

// execCall records a single invocation of Reconciler.PodExec.
type execCall struct {
	namespace string
	pod       string
	container string
	stdin     string
	command   []string
}

// execRecorder returns a PodExec function that appends every call to calls and
// returns the error produced by result, if any.
func execRecorder(calls *[]execCall, result func(call execCall) error) func(
	ctx context.Context, namespace, pod, container string,
	stdin io.Reader, stdout, stderr io.Writer, command ...string,
) error {
	return func(
		ctx context.Context, namespace, pod, container string,
		stdin io.Reader, stdout, stderr io.Writer, command ...string,
	) error {
		call := execCall{
			namespace: namespace,
			pod:       pod,
			container: container,
			command:   command,
		}
		if stdin != nil {
			b, err := io.ReadAll(stdin)
			if err != nil {
				return err
			}
			call.stdin = string(b)
		}

		*calls = append(*calls, call)

		if result != nil {
			if err := result(call); err != nil {
				return err
			}
		}

		// Stand in for the "wc -c" that fetchSecretToTempFile appends to its
		// write command to detect short writes.
		if len(command) > 2 && strings.Contains(command[2], "wc -c") {
			_, _ = io.WriteString(stdout, strconv.Itoa(len(call.stdin))+"\n")
		}
		return nil
	}
}

// tdeVaultSpec is the vault configuration shared by the tests below.
func tdeVaultSpec() *v1beta1.PGTDEVaultSpec {
	return &v1beta1.PGTDEVaultSpec{
		Host:      "https://vault.example:8200",
		MountPath: "tde",
		TokenSecret: v1beta1.PGTDESecretObjectReference{
			Name: "vault-secret", Key: "token",
		},
		CASecret: v1beta1.PGTDESecretObjectReference{
			Name: "vault-secret", Key: "ca.crt",
		},
	}
}

// tdeInstance builds an observed instance that is running, writable and whose
// Pod matches its PodTemplate, i.e. one that reconcilePGTDEProviders accepts.
func tdeInstance(annotations map[string]string) *Instance {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns1",
			Name:      "pgc1-instance1-abcd-0",
			Labels: map[string]string{
				appsv1.StatefulSetRevisionLabel: "rev-1",
			},
			Annotations: map[string]string{
				"status": `{"role":"primary"}`,
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:  naming.ContainerDatabase,
				State: corev1.ContainerState{Running: new(corev1.ContainerStateRunning)},
			}},
		},
	}
	for k, v := range annotations {
		pod.Annotations[k] = v
	}

	return &Instance{
		Name: "instance1-abcd",
		Pods: []*corev1.Pod{pod},
		Runner: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Generation: 2},
			Status: appsv1.StatefulSetStatus{
				ObservedGeneration: 2,
				UpdateRevision:     "rev-1",
			},
		},
	}
}

func TestPGTDEVaultRevision(t *testing.T) {
	t.Parallel()

	vault := tdeVaultSpec()
	tokenPath, caPath := pgtde.VaultCredentialPaths(vault)

	base, err := pgtde.VaultRevision(vault, tokenPath, caPath)
	assert.NilError(t, err)
	assert.Assert(t, base != "")

	t.Run("Deterministic", func(t *testing.T) {
		again, err := pgtde.VaultRevision(tdeVaultSpec(), tokenPath, caPath)
		assert.NilError(t, err)
		assert.Equal(t, base, again, "same input should hash the same")
	})

	t.Run("TempPathsDiffer", func(t *testing.T) {
		tempToken, tempCA := pgtde.TempVaultCredentialPaths(vault)
		temp, err := pgtde.VaultRevision(vault, tempToken, tempCA)
		assert.NilError(t, err)
		assert.Assert(t, temp != base,
			"temp revision must differ from standard revision; the two-phase "+
				"provider change relies on telling them apart")
	})

	// Every field that influences how PostgreSQL reaches Vault must change the
	// revision, otherwise a configuration change is silently never applied.
	for _, tc := range []struct {
		name   string
		mutate func(*v1beta1.PGTDEVaultSpec)
	}{
		{"Host", func(v *v1beta1.PGTDEVaultSpec) { v.Host = "https://other:8200" }},
		{"MountPath", func(v *v1beta1.PGTDEVaultSpec) { v.MountPath = "other" }},
		{"TokenSecretName", func(v *v1beta1.PGTDEVaultSpec) { v.TokenSecret.Name = "other" }},
		{"TokenSecretKey", func(v *v1beta1.PGTDEVaultSpec) { v.TokenSecret.Key = "other" }},
		{"CASecretName", func(v *v1beta1.PGTDEVaultSpec) { v.CASecret.Name = "other" }},
		{"CASecretKey", func(v *v1beta1.PGTDEVaultSpec) { v.CASecret.Key = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := tdeVaultSpec()
			tc.mutate(changed)

			// Recompute the paths; some of the fields above feed into them.
			token, ca := pgtde.VaultCredentialPaths(changed)
			rev, err := pgtde.VaultRevision(changed, token, ca)
			assert.NilError(t, err)
			assert.Assert(t, rev != base, "changing %s should change the revision", tc.name)
		})
	}

	// Host and MountPath are hashed one after another and neither feeds into
	// the credential paths, so they isolate how the fields are delimited from
	// everything else that goes into the revision.
	hostMount := func(host, mountPath string) string {
		t.Helper()
		vault := tdeVaultSpec()
		vault.Host, vault.MountPath = host, mountPath

		token, ca := pgtde.VaultCredentialPaths(vault)
		revision, err := pgtde.VaultRevision(vault, token, ca)
		assert.NilError(t, err)
		return revision
	}

	// Without a delimiter, moving a character across a field boundary produces
	// the same revision, and reconcilePGTDEProviders takes its "matches the
	// spec" early return on a Vault the cluster has never been pointed at.
	t.Run("FieldBoundaries", func(t *testing.T) {
		assert.Assert(t,
			hostMount("https://vault:8200", "secret/data") !=
				hostMount("https://vault:8200secret", "/data"),
			"a character moved from one field to the next must change the revision")
	})

	// Delimiting with a quote is only injective when a quote appearing inside a
	// value is escaped.
	t.Run("QuotesInValues", func(t *testing.T) {
		assert.Assert(t, hostMount(`a"`, `b`) != hostMount(`a`, `"b`),
			"a quote inside a value must not fake a field boundary")
	})
}

func TestPreserveOldTDEVolume(t *testing.T) {
	t.Parallel()

	oldVolume := corev1.Volume{
		Name: naming.PGTDEVolume,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{{
					Secret: &corev1.SecretProjection{
						LocalObjectReference: corev1.LocalObjectReference{Name: "old-secret"},
					},
				}},
			},
		},
	}
	newVolume := corev1.Volume{
		Name: naming.PGTDEVolume,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{{
					Secret: &corev1.SecretProjection{
						LocalObjectReference: corev1.LocalObjectReference{Name: "new-secret"},
					},
				}},
			},
		},
	}

	runnerWith := func(volumes ...corev1.Volume) *appsv1.StatefulSet {
		return &appsv1.StatefulSet{
			Spec: appsv1.StatefulSetSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Volumes: volumes},
				},
			},
		}
	}

	t.Run("Replaces", func(t *testing.T) {
		podSpec := &corev1.PodSpec{Volumes: []corev1.Volume{
			{Name: "pgdata"}, newVolume, {Name: "tmp"},
		}}

		pgtde.PreserveOldTDEVolume(podSpec, runnerWith(oldVolume, corev1.Volume{Name: "pgdata"}))

		assert.Equal(t, len(podSpec.Volumes), 3, "no volumes should be added or removed")
		assert.Equal(t, podSpec.Volumes[0].Name, "pgdata", "unrelated volumes keep their order")
		assert.Equal(t, podSpec.Volumes[2].Name, "tmp")
		assert.Equal(t,
			podSpec.Volumes[1].Projected.Sources[0].Secret.Name, "old-secret",
			"the running Pod's TDE volume should be kept")
	})

	t.Run("NoVolumeInRunner", func(t *testing.T) {
		podSpec := &corev1.PodSpec{Volumes: []corev1.Volume{newVolume}}

		pgtde.PreserveOldTDEVolume(podSpec, runnerWith(corev1.Volume{Name: "pgdata"}))

		assert.Equal(t,
			podSpec.Volumes[0].Projected.Sources[0].Secret.Name, "new-secret",
			"without an old volume to preserve the intent is left alone")
	})

	t.Run("NoVolumeInPodSpec", func(t *testing.T) {
		podSpec := &corev1.PodSpec{Volumes: []corev1.Volume{{Name: "pgdata"}}}

		pgtde.PreserveOldTDEVolume(podSpec, runnerWith(oldVolume))

		assert.Equal(t, len(podSpec.Volumes), 1,
			"the old volume should not be grafted onto a Pod that has none")
		assert.Equal(t, podSpec.Volumes[0].Name, "pgdata")
	})
}

// TestScaleUpInstancesPreservesTDEVolume covers the wiring around
// preserveOldTDEVolume rather than the helper itself. scaleUpInstances passes
// the observed StatefulSet as the intent to build into, and reconcileInstance
// empties that object before generating the new Pod spec. Reading the old
// volume from the observed instance therefore copies the new volume onto
// itself, and every unit test of the helper still passes because it builds the
// two objects separately.
func TestScaleUpInstancesPreservesTDEVolume(t *testing.T) {
	ctx := context.Background()
	_, cc := setupKubernetes(t)
	require.ParallelCapacity(t, 1)

	ns := setupNamespace(t, cc)

	r := &Reconciler{
		Client:   cc,
		Owner:    client.FieldOwner(t.Name()),
		Recorder: new(record.FakeRecorder),
		Tracer:   otel.Tracer(t.Name()),
	}

	rootCA, err := pki.NewRootCertificateAuthority()
	assert.NilError(t, err)

	vaultWith := func(secretName string) *v1beta1.PGTDEVaultSpec {
		return &v1beta1.PGTDEVaultSpec{
			Host:        "https://vault.example.com:8200",
			MountPath:   "secret/data",
			TokenSecret: v1beta1.PGTDESecretObjectReference{Name: secretName, Key: "token"},
		}
	}

	cluster := testCluster()
	cluster.Namespace = ns.Name
	cluster.Spec.PostgresVersion = 17
	cluster.Spec.Extensions.PGTDE = v1beta1.PGTDESpec{
		Enabled: true,
		Vault:   vaultWith("vault-old"),
	}
	assert.NilError(t, cluster.Default(ctx, nil))
	assert.NilError(t, cc.Create(ctx, cluster))

	set := &cluster.Spec.InstanceSets[0]

	// observe lists the StatefulSets the previous round applied, the way
	// Reconcile does at the top of each pass.
	observe := func() *observedInstances {
		t.Helper()
		var runners appsv1.StatefulSetList
		assert.NilError(t, cc.List(ctx, &runners, client.InNamespace(ns.Name)))
		return newObservedInstances(cluster, runners.Items, nil)
	}

	scaleUp := func(observed *observedInstances) {
		t.Helper()
		object := func(name string) metav1.ObjectMeta {
			return metav1.ObjectMeta{Namespace: ns.Name, Name: name}
		}
		_, err := r.scaleUpInstances(ctx, cluster, observed, set,
			&corev1.ConfigMap{ObjectMeta: object("cluster-config")},
			&corev1.Secret{ObjectMeta: object("cluster-replication")},
			rootCA,
			&corev1.Service{ObjectMeta: object("cluster-pods")},
			&corev1.ServiceAccount{ObjectMeta: object("cluster-instance")},
			&corev1.Service{ObjectMeta: object("cluster-ha")},
			clusterCertSecretProjection(&corev1.Secret{ObjectMeta: object("cluster-cert")}),
			nil, 1, nil, nil, nil, false)
		assert.NilError(t, err)
	}

	// appliedTokenSecret returns the Secret projected into the pg-tde volume of
	// the instance StatefulSet as it exists in the API.
	appliedTokenSecret := func() string {
		t.Helper()
		var runners appsv1.StatefulSetList
		assert.NilError(t, cc.List(ctx, &runners, client.InNamespace(ns.Name)))
		assert.Equal(t, len(runners.Items), 1)

		for _, volume := range runners.Items[0].Spec.Template.Spec.Volumes {
			if volume.Name == naming.PGTDEVolume {
				return volume.Projected.Sources[0].Secret.Name
			}
		}
		t.Fatalf("no %q volume in %v", naming.PGTDEVolume,
			runners.Items[0].Spec.Template.Spec.Volumes)
		return ""
	}

	// revisionFor is the value reconcilePGTDEProviders stores in
	// Status.PGTDERevision once the provider names the given paths.
	revisionFor := func(vault *v1beta1.PGTDEVaultSpec, temp bool) string {
		t.Helper()
		paths := pgtde.VaultCredentialPaths
		if temp {
			paths = pgtde.TempVaultCredentialPaths
		}
		tokenPath, caPath := paths(vault)
		revision, err := pgtde.VaultRevision(vault, tokenPath, caPath)
		assert.NilError(t, err)
		return revision
	}

	// Initial setup: no provider has been configured, so there is nothing to
	// hold and the Pod gets the Secret named in the spec.
	scaleUp(observe())
	assert.Equal(t, appliedTokenSecret(), "vault-old")

	cluster.Status.PGTDERevision = revisionFor(vaultWith("vault-old"), false)

	// The user points pg_tde at a different Vault token. Until phase 1 has run
	// the provider still names the old credentials, so the Pod has to keep
	// mounting them; rolling now would leave pg_tde unable to fetch its key.
	cluster.Spec.Extensions.PGTDE.Vault = vaultWith("vault-new")
	scaleUp(observe())
	assert.Equal(t, appliedTokenSecret(), "vault-old",
		"the vault volume should be held until the provider change has run")

	// Phase 1 has run: the provider now names the staged credentials on the
	// data volume, so the hold is released and the Pods roll.
	cluster.Status.PGTDERevision = revisionFor(vaultWith("vault-new"), true)
	scaleUp(observe())
	assert.Equal(t, appliedTokenSecret(), "vault-new",
		"the hold should be released once the provider names the staged credentials")
}

func TestStageVaultCredentials(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "vault-secret"},
		Data: map[string][]byte{
			"token":  []byte("hvs.sometoken"),
			"ca.crt": []byte("-----BEGIN CERTIFICATE-----"),
		},
	}

	newPods := func(names ...string) []*corev1.Pod {
		pods := make([]*corev1.Pod, 0, len(names))
		for _, name := range names {
			pods = append(pods, &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: name},
			})
		}
		return pods
	}

	vault := tdeVaultSpec()
	tokenPath, caPath := pgtde.TempVaultCredentialPaths(vault)

	t.Run("WritesToEveryPod", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().WithObjects(secret).Build()
		pods := newPods("pgc1-instance1-abcd-0", "pgc1-instance2-efgh-0", "pgc1-instance3-ijkl-0")

		assert.NilError(t, stagePGTDEVaultCredentials(ctx, k8s, execRecorder(&calls, nil),
			"ns1", vault, pods, naming.ContainerDatabase, tokenPath, caPath))

		// pg_tde names one path cluster-wide, but each instance has its own
		// /pgdata, so every one of them needs its own copy.
		assert.Equal(t, len(calls), 6, "two files on each of three instances")

		for i, pod := range pods {
			token, ca := calls[i*2], calls[i*2+1]

			assert.Equal(t, token.pod, pod.Name)
			assert.Equal(t, token.stdin, "hvs.sometoken")
			assert.Assert(t, strings.Contains(token.command[2], tokenPath))

			assert.Equal(t, ca.pod, pod.Name)
			assert.Equal(t, ca.stdin, "-----BEGIN CERTIFICATE-----")
			assert.Assert(t, strings.Contains(ca.command[2], caPath))
		}
	})

	t.Run("ReadsEachSecretOnce", func(t *testing.T) {
		var calls []execCall
		gets := 0
		k8s := &countingReader{
			Reader: fake.NewClientBuilder().WithObjects(secret).Build(),
			gets:   &gets,
		}

		assert.NilError(t, stagePGTDEVaultCredentials(ctx, k8s, execRecorder(&calls, nil),
			"ns1", vault, newPods("a", "b", "c", "d"), naming.ContainerDatabase,
			tokenPath, caPath))

		assert.Equal(t, gets, 2,
			"the token and CA Secrets should be read once, not once per instance")
	})

	t.Run("WithoutCASecret", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().WithObjects(secret).Build()

		noCA := tdeVaultSpec()
		noCA.CASecret = v1beta1.PGTDESecretObjectReference{}
		_, noCAPath := pgtde.TempVaultCredentialPaths(noCA)

		assert.NilError(t, stagePGTDEVaultCredentials(ctx, k8s, execRecorder(&calls, nil),
			"ns1", noCA, newPods("a", "b"), naming.ContainerDatabase,
			tokenPath, noCAPath))

		assert.Equal(t, len(calls), 2, "only the token is staged")
	})

	t.Run("MissingSecretWritesNothing", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().Build()

		err := stagePGTDEVaultCredentials(ctx, k8s, execRecorder(&calls, nil),
			"ns1", vault, newPods("a", "b"), naming.ContainerDatabase, tokenPath, caPath)

		assert.ErrorContains(t, err, "token secret")
		assert.Equal(t, len(calls), 0,
			"the Secrets are read before anything is written to any Pod")
	})

	t.Run("MissingKey", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().WithObjects(secret).Build()

		badKey := tdeVaultSpec()
		badKey.TokenSecret.Key = "nope"

		err := stagePGTDEVaultCredentials(ctx, k8s, execRecorder(&calls, nil),
			"ns1", badKey, newPods("a"), naming.ContainerDatabase, tokenPath, caPath)

		assert.ErrorContains(t, err, `key "nope" not found`)
		assert.Equal(t, len(calls), 0)
	})

	t.Run("FailureNamesThePod", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().WithObjects(secret).Build()

		err := stagePGTDEVaultCredentials(ctx, k8s,
			execRecorder(&calls, func(call execCall) error {
				if call.pod == "b" {
					return errors.New("no space left on device")
				}
				return nil
			}),
			"ns1", vault, newPods("a", "b", "c"), naming.ContainerDatabase,
			tokenPath, caPath)

		assert.ErrorContains(t, err, "pod b")
		assert.ErrorContains(t, err, "no space left on device")
		assert.Equal(t, len(calls), 3,
			"staging stops at the first instance it cannot write to")
	})
}

func TestWriteTempFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "pgc1-instance1-abcd-0"},
	}

	t.Run("PipesDataAndSetsMode", func(t *testing.T) {
		var calls []execCall

		assert.NilError(t, writeTempFile(ctx, execRecorder(&calls, nil), pod,
			naming.ContainerDatabase, pgtde.TempTokenPath, []byte("hvs.sometoken")))

		assert.Equal(t, len(calls), 1)
		assert.Equal(t, calls[0].namespace, "ns1")
		assert.Equal(t, calls[0].pod, pod.Name)
		assert.Equal(t, calls[0].container, naming.ContainerDatabase)
		assert.Equal(t, calls[0].stdin, "hvs.sometoken",
			"the secret value should be piped in, not interpolated into the command")
		assert.DeepEqual(t, calls[0].command[:2], []string{"bash", "-ceu"})
		assert.Assert(t, strings.Contains(calls[0].command[2], pgtde.TempTokenPath))
		assert.Assert(t, strings.Contains(calls[0].command[2], "umask 077"),
			"the token file must never exist in a world readable state")
	})

	t.Run("ShortWrite", func(t *testing.T) {
		// The container reports fewer bytes on disk than were sent.
		err := writeTempFile(ctx,
			func(ctx context.Context, namespace, pod, container string,
				stdin io.Reader, stdout, stderr io.Writer, command ...string,
			) error {
				_, _ = io.WriteString(stdout, "4\n")
				return nil
			},
			pod, naming.ContainerDatabase, pgtde.TempTokenPath, []byte("hvs.sometoken"))

		assert.ErrorContains(t, err, "wrote 4 of 13 bytes",
			"a truncated token must not be accepted as written")
	})

	t.Run("ExecFails", func(t *testing.T) {
		var calls []execCall

		err := writeTempFile(ctx,
			execRecorder(&calls, func(execCall) error {
				return errors.New("no such file or directory")
			}),
			pod, naming.ContainerDatabase, pgtde.TempTokenPath, []byte("x"))

		assert.ErrorContains(t, err, pgtde.TempTokenPath)
		assert.ErrorContains(t, err, "no such file or directory")
	})
}

// countingReader counts Get calls made against the embedded reader.
type countingReader struct {
	client.Reader
	gets *int
}

func (c *countingReader) Get(
	ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption,
) error {
	*c.gets++
	return c.Reader.Get(ctx, key, obj, opts...)
}

func TestReconcilePGTDEProviders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "vault-secret"},
		Data: map[string][]byte{
			"token":  []byte("hvs.newtoken"),
			"ca.crt": []byte("-----BEGIN CERTIFICATE-----"),
		},
	}

	vault := tdeVaultSpec()
	tokenPath, caPath := pgtde.VaultCredentialPaths(vault)
	tempTokenPath, tempCAPath := pgtde.TempVaultCredentialPaths(vault)

	standardRevision, err := pgtde.VaultRevision(vault, tokenPath, caPath)
	assert.NilError(t, err)
	tempRevision, err := pgtde.VaultRevision(vault, tempTokenPath, tempCAPath)
	assert.NilError(t, err)

	newCluster := func() *v1beta1.PostgresCluster {
		cluster := &v1beta1.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "pgc1", UID: "the-uid"},
		}
		cluster.Spec.Extensions.PGTDE = v1beta1.PGTDESpec{
			Enabled: true,
			Vault:   tdeVaultSpec(),
		}
		return cluster
	}

	// psqlCalls returns the subset of calls that ran SQL, ignoring the shell
	// calls used to write and remove temporary files.
	psqlCalls := func(calls []execCall) []execCall {
		var out []execCall
		for _, call := range calls {
			if len(call.command) > 0 && call.command[0] == "psql" {
				out = append(out, call)
			}
		}
		return out
	}

	t.Run("Disabled", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster()
		cluster.Spec.Extensions.PGTDE.Enabled = false
		cluster.Status.PGTDERevision = standardRevision

		r := &Reconciler{
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}
		observed := &observedInstances{forCluster: []*Instance{
			tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"}),
		}}

		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, observed, failPatch(t)))
		assert.Equal(t, cluster.Status.PGTDERevision, "",
			"the revision must be cleared so re-enabling starts from scratch")
	})

	t.Run("NoVaultSpec", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster()
		cluster.Spec.Extensions.PGTDE.Vault = nil
		cluster.Status.PGTDERevision = standardRevision

		r := &Reconciler{
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}
		observed := &observedInstances{forCluster: []*Instance{
			tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"}),
		}}

		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, observed, failPatch(t)))
		assert.Equal(t, cluster.Status.PGTDERevision, "")
	})

	t.Run("WaitsForRollout", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster()

		instance := tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"})
		// The Pod is running an older revision than the StatefulSet intends.
		instance.Pods[0].Labels[appsv1.StatefulSetRevisionLabel] = "rev-0"

		r := &Reconciler{
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}
		observed := &observedInstances{forCluster: []*Instance{instance}}

		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, observed, failPatch(t)))
		assert.Equal(t, len(calls), 0,
			"SQL must not run against a Pod that is mid-rollout")
		assert.Equal(t, cluster.Status.PGTDERevision, "")
	})

	t.Run("WaitsForOtherInstances", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster()

		primary := tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"})
		replica := tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"})
		replica.Name = "instance2-efgh"
		replica.Pods[0].Name = "pgc1-instance2-efgh-0"
		replica.Pods[0].Annotations["status"] = `{"role":"replica"}`
		replica.Pods[0].Labels[appsv1.StatefulSetRevisionLabel] = "rev-0"

		r := &Reconciler{
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}
		observed := &observedInstances{forCluster: []*Instance{primary, replica}}

		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, observed, failPatch(t)))
		assert.Equal(t, len(calls), 0,
			"every instance must match its template, not just the primary")
	})

	t.Run("NoWritablePod", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster()

		instance := tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"})
		instance.Pods[0].Annotations["status"] = `{"role":"replica"}`

		r := &Reconciler{
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}
		observed := &observedInstances{forCluster: []*Instance{instance}}

		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, observed, failPatch(t)))
		assert.Equal(t, len(calls), 0)
	})

	t.Run("WaitsForExtension", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster()

		r := &Reconciler{
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}
		// No TDEInstalledAnnotation: the Pod predates the extension install.
		observed := &observedInstances{forCluster: []*Instance{tdeInstance(nil)}}

		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, observed, failPatch(t)))
		assert.Equal(t, len(calls), 0,
			"the provider cannot be configured before pg_tde is loaded")
		assert.Equal(t, cluster.Status.PGTDERevision, "")
	})

	t.Run("AlreadyConfigured", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster()
		cluster.Status.PGTDERevision = standardRevision
		// Steady state: the revision matches and the last reconcile confirmed
		// the data volumes hold no staged credentials.
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:   v1beta1.PGTDEVaultProviderReady,
			Status: metav1.ConditionTrue,
			Reason: "Configured",
		})

		r := &Reconciler{
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}
		observed := &observedInstances{forCluster: []*Instance{
			tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"}),
		}}

		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, observed, failPatch(t)))
		assert.Equal(t, len(calls), 0, "a matching revision is a no-op")
	})

	// A change that fails after staging leaves the new vault token on every
	// data volume. Reverting the spec makes the stored revision match again,
	// so nothing else in this function will ever look at those files.
	t.Run("RevertedAfterFailedChange", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster()
		cluster.Status.PGTDERevision = standardRevision
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    v1beta1.PGTDEVaultProviderReady,
			Status:  metav1.ConditionFalse,
			Reason:  "ChangeFailed",
			Message: "permission denied",
		})

		r := &Reconciler{
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}
		observed := &observedInstances{forCluster: []*Instance{
			tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"}),
		}}

		patched := 0
		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, observed,
			func() error { patched++; return nil }))

		assert.Equal(t, len(calls), 1,
			"the credentials staged for the abandoned change must be removed")
		assert.Assert(t, strings.Contains(calls[0].command[2], pgtde.TempTokenPath))
		assert.Assert(t, strings.Contains(calls[0].command[2], pgtde.TempCAPath))

		// The failure the user already resolved must stop being reported.
		assertTDEProviderCondition(t, cluster, metav1.ConditionTrue, "Configured")
		assert.Equal(t, patched, 1)
	})

	// The revision is only ever stored alongside a condition, so a missing one
	// means the status was lost rather than that the volumes are known clean.
	t.Run("RevisionWithoutCondition", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster()
		cluster.Status.PGTDERevision = standardRevision

		r := &Reconciler{
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}
		observed := &observedInstances{forCluster: []*Instance{
			tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"}),
		}}

		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, observed,
			func() error { return nil }))

		assertTDEProviderCondition(t, cluster, metav1.ConditionTrue, "Configured")
	})

	t.Run("InitialSetup", func(t *testing.T) {
		var calls []execCall
		patched := 0
		cluster := newCluster()

		r := &Reconciler{
			Client:   fake.NewClientBuilder().WithObjects(secret).Build(),
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}
		observed := &observedInstances{forCluster: []*Instance{
			tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"}),
		}}

		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, observed,
			func() error { patched++; return nil }))

		sql := psqlCalls(calls)
		assert.Equal(t, len(sql), 3,
			"initial setup adds the provider, creates the key and sets it as default")
		assert.Assert(t, strings.Contains(sql[0].stdin, "pg_tde_add_global_key_provider_vault_v2"))
		assert.Assert(t, strings.Contains(sql[1].stdin, "pg_tde_create_key_using_global_key_provider"))
		assert.Assert(t, strings.Contains(sql[2].stdin, "pg_tde_set_default_key_using_global_key_provider"))

		assert.Assert(t, argsContain(sql[0].command, "--set=token_path="+tokenPath),
			"initial setup uses the mounted credential paths, not the temporary ones")
		assert.Assert(t, argsContain(sql[0].command, "--set=ca_path="+caPath))

		assert.Equal(t, len(calls), len(sql),
			"no temporary files are needed when there is nothing to rotate")
		assert.Equal(t, cluster.Status.PGTDERevision, standardRevision)
		assert.Equal(t, patched, 1, "the revision must be persisted immediately")
		assertTDEProviderCondition(t, cluster, metav1.ConditionTrue, "Configured")
	})

	t.Run("PhaseOne", func(t *testing.T) {
		var calls []execCall
		patched := 0
		cluster := newCluster()
		// A revision that is neither the standard nor the temp one: the user
		// changed the vault configuration.
		cluster.Status.PGTDERevision = "stale"

		r := &Reconciler{
			Client:   fake.NewClientBuilder().WithObjects(secret).Build(),
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}
		observed := &observedInstances{forCluster: []*Instance{
			tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"}),
		}}

		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, observed,
			func() error { patched++; return nil }))

		// The new credentials are staged on the persistent volume first...
		assert.Equal(t, len(calls), 3)
		assert.Assert(t, strings.Contains(calls[0].command[2], tempTokenPath))
		assert.Equal(t, calls[0].stdin, "hvs.newtoken")
		assert.Assert(t, strings.Contains(calls[1].command[2], tempCAPath))

		// ...and only then is the provider pointed at them.
		sql := psqlCalls(calls)
		assert.Equal(t, len(sql), 1)
		assert.Assert(t, strings.Contains(sql[0].stdin, "pg_tde_change_global_key_provider_vault_v2"))
		assert.Assert(t, argsContain(sql[0].command, "--set=token_path="+tempTokenPath))
		assert.Assert(t, argsContain(sql[0].command, "--set=ca_path="+tempCAPath))

		assert.Equal(t, cluster.Status.PGTDERevision, tempRevision,
			"the temp revision releases the volume hold in reconcileInstance")
		assert.Equal(t, patched, 1)
		assertTDEProviderCondition(t, cluster, metav1.ConditionFalse, "ChangeInProgress")
	})

	t.Run("PhaseOneSecretMissing", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster()
		cluster.Status.PGTDERevision = "stale"

		r := &Reconciler{
			Client:   fake.NewClientBuilder().Build(),
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}
		observed := &observedInstances{forCluster: []*Instance{
			tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"}),
		}}

		patched := 0
		err := r.reconcilePGTDEProviders(ctx, cluster, observed,
			func() error { patched++; return nil })
		assert.ErrorContains(t, err, "token secret")
		assert.Equal(t, len(psqlCalls(calls)), 0,
			"the provider must not be changed to paths that were never written")
		assert.Equal(t, cluster.Status.PGTDERevision, "stale",
			"the revision must not advance when phase one fails")

		// reconcileInstance keeps holding the old vault volume while the
		// revision is stale, so the reason must reach the user.
		assertTDEProviderCondition(t, cluster, metav1.ConditionFalse, "ChangeFailed")
		assert.Assert(t, strings.Contains(tdeCondition(cluster).Message, "vault-secret"),
			"the condition should name the Secret that could not be read")
		assert.Equal(t, patched, 1,
			"the failure condition is useless unless it is written to the API")
		assertEvent(t, r.Recorder, "PGTDEVaultProviderChangeFailed")
	})

	t.Run("PhaseTwo", func(t *testing.T) {
		var calls []execCall
		patched := 0
		cluster := newCluster()
		cluster.Status.PGTDERevision = tempRevision

		r := &Reconciler{
			Client:   fake.NewClientBuilder().WithObjects(secret).Build(),
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}
		observed := &observedInstances{forCluster: []*Instance{
			tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"}),
		}}

		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, observed,
			func() error { patched++; return nil }))

		sql := psqlCalls(calls)
		assert.Equal(t, len(sql), 1)
		assert.Assert(t, strings.Contains(sql[0].stdin, "pg_tde_change_global_key_provider_vault_v2"))
		assert.Assert(t, argsContain(sql[0].command, "--set=token_path="+tokenPath),
			"phase two points the provider back at the mounted paths")
		assert.Assert(t, argsContain(sql[0].command, "--set=ca_path="+caPath))

		// The staged credentials are removed once nothing references them.
		assert.Equal(t, len(calls), 2)
		assert.Assert(t, strings.Contains(calls[1].command[2], tempTokenPath))
		assert.Assert(t, strings.Contains(calls[1].command[2], tempCAPath))

		assert.Equal(t, cluster.Status.PGTDERevision, standardRevision)
		assert.Equal(t, patched, 1)
		assertTDEProviderCondition(t, cluster, metav1.ConditionTrue, "Configured")
	})

	t.Run("PhaseTwoKeepsTempFilesOnFailure", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster()
		cluster.Status.PGTDERevision = tempRevision

		r := &Reconciler{
			Client:   fake.NewClientBuilder().WithObjects(secret).Build(),
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec: execRecorder(&calls, func(call execCall) error {
				if call.command[0] == "psql" {
					return errors.New("could not connect to server")
				}
				return nil
			}),
		}
		observed := &observedInstances{forCluster: []*Instance{
			tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"}),
		}}

		err := r.reconcilePGTDEProviders(ctx, cluster, observed, func() error { return nil })
		assert.ErrorContains(t, err, "could not connect to server")
		assert.Equal(t, len(calls), 1,
			"temp files must survive a failed phase two so it can be retried")
		assert.Equal(t, cluster.Status.PGTDERevision, tempRevision)
		assertTDEProviderCondition(t, cluster, metav1.ConditionFalse, "ChangeFailed")
	})

	t.Run("CleanupRetrySucceeds", func(t *testing.T) {
		var calls []execCall
		fail := true
		cluster := newCluster()
		cluster.Status.PGTDERevision = standardRevision

		r := &Reconciler{
			Client:   fake.NewClientBuilder().WithObjects(secret).Build(),
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec: execRecorder(&calls, func(execCall) error {
				if fail {
					return errors.New("permission denied")
				}
				return nil
			}),
		}
		observed := &observedInstances{forCluster: []*Instance{
			tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"}),
		}}

		fail = false
		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, observed,
			func() error { return nil }))
		assert.Equal(t, len(calls), 1)
		assert.Equal(t, len(psqlCalls(calls)), 0, "nothing about the provider changed")
		assertTDEProviderCondition(t, cluster, metav1.ConditionTrue, "Configured")
	})

	t.Run("PatchStatusFails", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster()

		r := &Reconciler{
			Client:   fake.NewClientBuilder().WithObjects(secret).Build(),
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}
		observed := &observedInstances{forCluster: []*Instance{
			tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"}),
		}}

		err := r.reconcilePGTDEProviders(ctx, cluster, observed,
			func() error { return errors.New("conflict") })
		assert.ErrorContains(t, err, "patch status")
	})
}

// tdeCondition returns the PGTDEVaultProviderReady condition, failing the test
// when it is absent.
func tdeCondition(cluster *v1beta1.PostgresCluster) metav1.Condition {
	condition := meta.FindStatusCondition(cluster.Status.Conditions,
		v1beta1.PGTDEVaultProviderReady)
	if condition == nil {
		return metav1.Condition{Reason: "<missing>"}
	}
	return *condition
}

// assertTDEProviderCondition checks the status and reason of the
// PGTDEVaultProviderReady condition.
func assertTDEProviderCondition(
	t *testing.T, cluster *v1beta1.PostgresCluster,
	status metav1.ConditionStatus, reason string,
) {
	t.Helper()

	condition := tdeCondition(cluster)
	assert.Equal(t, string(condition.Status), string(status))
	assert.Equal(t, condition.Reason, reason)
}

// assertEvent checks that an event with the given reason was recorded.
func assertEvent(t *testing.T, recorder record.EventRecorder, reason string) {
	t.Helper()

	rec, ok := recorder.(*events.Recorder)
	assert.Assert(t, ok, "expected a testing recorder")
	for _, event := range rec.Events {
		if event.Reason == reason {
			return
		}
	}
	t.Errorf("expected an event with reason %q, got %v", reason, rec.Events)
}

// failPatch returns a patch function that fails the test when it is called.
func failPatch(t *testing.T) func() error {
	t.Helper()
	return func() error {
		t.Error("status should not be patched")
		return nil
	}
}

// argsContain reports whether command contains arg.
func argsContain(command []string, arg string) bool {
	for _, c := range command {
		if c == arg {
			return true
		}
	}
	return false
}

func TestReconcilePostgresDatabasesPGTDEReporting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// newCluster returns a cluster that reconcilePostgresDatabases will act on.
	newCluster := func(enabled bool) *v1beta1.PostgresCluster {
		cluster := &v1beta1.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ns1", Name: "pgc1", UID: "the-uid",
				Labels: map[string]string{naming.LabelVersion: "17.2.0"},
			},
		}
		cluster.Spec.Extensions.PGTDE = v1beta1.PGTDESpec{Enabled: enabled}
		return cluster
	}

	observed := func() *observedInstances {
		return &observedInstances{forCluster: []*Instance{tdeInstance(nil)}}
	}

	// pgTDECondition reports the PGTDEEnabled condition, which decides whether
	// pg_tde goes into shared_preload_libraries and whether the vault volume is
	// mounted.
	pgTDECondition := func(cluster *v1beta1.PostgresCluster) *metav1.Condition {
		return meta.FindStatusCondition(cluster.Status.Conditions, v1beta1.PGTDEEnabled)
	}

	t.Run("InstallFailureIsNotReportedAsEnabled", func(t *testing.T) {
		cluster := newCluster(true)
		calls := 0

		r := &Reconciler{
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec: func(ctx context.Context, namespace, pod, container string,
				stdin io.Reader, stdout, stderr io.Writer, command ...string,
			) error {
				b, err := io.ReadAll(stdin)
				assert.NilError(t, err)
				if strings.Contains(string(b), "pg_tde") {
					calls++
					return errors.New("could not open extension control file")
				}
				return nil
			},
		}

		// The failure is folded into the "OK" flags rather than returned, the
		// same way the other extensions handle theirs; it withholds the
		// DatabaseRevision so the next reconcile retries.
		patched := 0
		assert.NilError(t, r.reconcilePostgresDatabases(ctx, cluster, observed(),
			func() error { patched++; return nil }))
		assert.Equal(t, calls, 1, "only the real run reaches PodExec")
		assert.Equal(t, patched, 1, "the failure event and condition still need persisting")

		// The regression: the hash run executes the same statements against a
		// fake executor that cannot fail, and used to flip this to True before
		// CREATE EXTENSION had been attempted.
		condition := pgTDECondition(cluster)
		assert.Assert(t, condition == nil || condition.Status != metav1.ConditionTrue,
			"pg_tde must not be reported as enabled when CREATE EXTENSION failed")
		assertEvent(t, r.Recorder, "PGTDEInstallFailed")
		assert.Equal(t, cluster.Status.DatabaseRevision, "")
	})

	t.Run("SuccessIsReported", func(t *testing.T) {
		cluster := newCluster(true)
		patched := 0

		r := &Reconciler{
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec: func(ctx context.Context, namespace, pod, container string,
				stdin io.Reader, stdout, stderr io.Writer, command ...string,
			) error {
				return nil
			},
		}

		assert.NilError(t, r.reconcilePostgresDatabases(ctx, cluster, observed(),
			func() error { patched++; return nil }))

		condition := pgTDECondition(cluster)
		assert.Assert(t, condition != nil)
		assert.Equal(t, condition.Status, metav1.ConditionTrue)
		assert.Assert(t, cluster.Status.DatabaseRevision != "")
		assert.Equal(t, patched, 1, "the condition and the revision share one patch")
	})

	t.Run("NothingIsReportedWithoutAWritablePod", func(t *testing.T) {
		cluster := newCluster(true)

		instance := tdeInstance(nil)
		instance.Pods[0].Annotations["status"] = `{"role":"replica"}`

		r := &Reconciler{Recorder: events.NewRecorder(t, runtime.Scheme)}

		assert.NilError(t, r.reconcilePostgresDatabases(ctx, cluster,
			&observedInstances{forCluster: []*Instance{instance}}, failPatch(t)))
		assert.Assert(t, pgTDECondition(cluster) == nil,
			"no SQL ran, so there is nothing to report")
	})

	t.Run("DisableIsReportedOnlyAfterItRuns", func(t *testing.T) {
		cluster := newCluster(false)

		r := &Reconciler{
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec: func(ctx context.Context, namespace, pod, container string,
				stdin io.Reader, stdout, stderr io.Writer, command ...string,
			) error {
				b, err := io.ReadAll(stdin)
				assert.NilError(t, err)
				if strings.Contains(string(b), "pg_tde") {
					return errors.New("cannot drop extension")
				}
				return nil
			},
		}

		assert.NilError(t, r.reconcilePostgresDatabases(ctx, cluster, observed(),
			func() error { return nil }))
		assert.Equal(t, cluster.Status.DatabaseRevision, "", "the drop must be retried")

		// Reporting False here would strip pg_tde from shared_preload_libraries
		// and restart the Pods while the extension is still installed.
		assert.Assert(t, pgTDECondition(cluster) == nil,
			"a failed DROP EXTENSION must not be reported as disabled")
		assertEvent(t, r.Recorder, "PGTDEDisableFailed")
	})
}

func TestReconcilePostgresDatabasesPGTDEStatusIsIndependent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	newCluster := func() *v1beta1.PostgresCluster {
		cluster := &v1beta1.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ns1", Name: "pgc1", UID: "the-uid",
				Labels: map[string]string{naming.LabelVersion: "17.2.0"},
			},
		}
		cluster.Spec.Extensions.PGTDE = v1beta1.PGTDESpec{Enabled: true}
		return cluster
	}

	observed := func() *observedInstances {
		return &observedInstances{forCluster: []*Instance{tdeInstance(nil)}}
	}

	// failOn returns a PodExec that fails every statement mentioning marker.
	failOn := func(marker string) func(
		ctx context.Context, namespace, pod, container string,
		stdin io.Reader, stdout, stderr io.Writer, command ...string,
	) error {
		return func(ctx context.Context, namespace, pod, container string,
			stdin io.Reader, stdout, stderr io.Writer, command ...string,
		) error {
			b, err := io.ReadAll(stdin)
			assert.NilError(t, err)
			if strings.Contains(string(b), marker) {
				return errors.New(marker + " is unavailable")
			}
			return nil
		}
	}

	t.Run("PersistedWhenAnotherExtensionFails", func(t *testing.T) {
		cluster := newCluster()
		patched := 0

		r := &Reconciler{
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  failOn("pgaudit"),
		}

		assert.NilError(t, r.reconcilePostgresDatabases(ctx, cluster, observed(),
			func() error { patched++; return nil }))

		// pgaudit failing withholds the revision, which is what makes the
		// whole batch of SQL run again next time.
		assert.Equal(t, cluster.Status.DatabaseRevision, "")

		// pg_tde installed fine, so its condition must not be held hostage:
		// reconcilePGTDEProviders runs next and needs it.
		condition := meta.FindStatusCondition(cluster.Status.Conditions, v1beta1.PGTDEEnabled)
		assert.Assert(t, condition != nil,
			"the pg_tde condition must not wait on unrelated extensions")
		assert.Equal(t, condition.Status, metav1.ConditionTrue)
		assert.Equal(t, patched, 1, "the condition should be persisted on its own")
	})

	t.Run("PatchFailureDoesNotCancelTheReconcile", func(t *testing.T) {
		cluster := newCluster()

		r := &Reconciler{
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec: func(ctx context.Context, namespace, pod, container string,
				stdin io.Reader, stdout, stderr io.Writer, command ...string,
			) error {
				return nil
			},
		}

		// A conflict on the status subresource says nothing about whether the
		// SQL worked, and Reconcile patches again before it returns.
		err := r.reconcilePostgresDatabases(ctx, cluster, observed(),
			func() error { return errors.New("the object has been modified") })

		assert.NilError(t, err,
			"a status conflict must not stop reconcilePGTDEProviders from running")
		assert.Assert(t, cluster.Status.DatabaseRevision != "",
			"the revision is still correct in memory for the final patch")
	})
}

func TestReconcilePGTDEProvidersMultipleInstances(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "vault-secret"},
		Data: map[string][]byte{
			"token":  []byte("hvs.newtoken"),
			"ca.crt": []byte("-----BEGIN CERTIFICATE-----"),
		},
	}

	vault := tdeVaultSpec()
	tokenPath, caPath := pgtde.VaultCredentialPaths(vault)
	tempTokenPath, tempCAPath := pgtde.TempVaultCredentialPaths(vault)

	standardRevision, err := pgtde.VaultRevision(vault, tokenPath, caPath)
	assert.NilError(t, err)
	tempRevision, err := pgtde.VaultRevision(vault, tempTokenPath, tempCAPath)
	assert.NilError(t, err)

	newCluster := func(revision string) *v1beta1.PostgresCluster {
		cluster := &v1beta1.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "pgc1", UID: "the-uid"},
		}
		cluster.Spec.Extensions.PGTDE = v1beta1.PGTDESpec{Enabled: true, Vault: tdeVaultSpec()}
		cluster.Status.PGTDERevision = revision
		return cluster
	}

	// replica returns a running, up-to-date instance that is not writable.
	replica := func(name string) *Instance {
		instance := tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"})
		instance.Name = name
		instance.Pods[0].Name = "pgc1-" + name + "-0"
		instance.Pods[0].Annotations["status"] = `{"role":"replica"}`
		return instance
	}

	// threeInstances returns a primary plus two replicas, all healthy.
	threeInstances := func() *observedInstances {
		return &observedInstances{forCluster: []*Instance{
			tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"}),
			replica("instance2-efgh"),
			replica("instance3-ijkl"),
		}}
	}

	podsWritten := func(calls []execCall, marker string) []string {
		var names []string
		for _, call := range calls {
			if len(call.command) > 2 && strings.Contains(call.command[2], marker) {
				names = append(names, call.pod)
			}
		}
		return names
	}

	t.Run("PhaseOneStagesOnEveryInstance", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster("stale")

		r := &Reconciler{
			Client:   fake.NewClientBuilder().WithObjects(secret).Build(),
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}

		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, threeInstances(),
			func() error { return nil }))

		// pg_tde resolves one path cluster-wide; a replica promoted before
		// phase 2 must find the credentials on its own volume.
		assert.DeepEqual(t, podsWritten(calls, tempTokenPath), []string{
			"pgc1-instance1-abcd-0", "pgc1-instance2-efgh-0", "pgc1-instance3-ijkl-0",
		})
		assert.DeepEqual(t, podsWritten(calls, tempCAPath), []string{
			"pgc1-instance1-abcd-0", "pgc1-instance2-efgh-0", "pgc1-instance3-ijkl-0",
		})

		// The SQL still runs once, on the primary.
		var sql []execCall
		for _, call := range calls {
			if call.command[0] == "psql" {
				sql = append(sql, call)
			}
		}
		assert.Equal(t, len(sql), 1)
		assert.Equal(t, sql[0].pod, "pgc1-instance1-abcd-0")
		assert.Equal(t, cluster.Status.PGTDERevision, tempRevision)
	})

	t.Run("PhaseOneWaitsForEveryInstance", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster("stale")

		// One replica's database container is not running yet.
		instances := threeInstances()
		instances.forCluster[2].Pods[0].Status.ContainerStatuses[0].State =
			corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"}}

		r := &Reconciler{
			Client:   fake.NewClientBuilder().WithObjects(secret).Build(),
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}

		patched := 0
		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, instances,
			func() error { patched++; return nil }))

		assert.Equal(t, len(calls), 0,
			"the provider must not name a path that one instance cannot read")
		assert.Equal(t, cluster.Status.PGTDERevision, "stale",
			"the volume hold stays until every instance is staged")
		assertTDEProviderCondition(t, cluster, metav1.ConditionFalse, "WaitingForInstances")
		assert.Equal(t, patched, 1, "the reason for waiting must be visible")
	})

	t.Run("PhaseTwoCleansEveryInstance", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster(tempRevision)

		r := &Reconciler{
			Client:   fake.NewClientBuilder().WithObjects(secret).Build(),
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}

		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, threeInstances(),
			func() error { return nil }))

		assert.DeepEqual(t, podsWritten(calls, "rm -f"), []string{
			"pgc1-instance1-abcd-0", "pgc1-instance2-efgh-0", "pgc1-instance3-ijkl-0",
		})
		assert.Equal(t, cluster.Status.PGTDERevision, standardRevision)
		assertTDEProviderCondition(t, cluster, metav1.ConditionTrue, "Configured")
	})

	t.Run("PhaseTwoCleanupIncompleteIsRetried", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster(tempRevision)

		// A replica is down, so its copy of the token cannot be removed.
		instances := threeInstances()
		instances.forCluster[2].Pods[0].Status.ContainerStatuses[0].State =
			corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{}}

		r := &Reconciler{
			Client:   fake.NewClientBuilder().WithObjects(secret).Build(),
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec:  execRecorder(&calls, nil),
		}

		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, instances,
			func() error { return nil }))
		assert.Equal(t, cluster.Status.PGTDERevision, tempRevision)

		instances = threeInstances()
		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, instances,
			func() error { return nil }))
		assert.Equal(t, cluster.Status.PGTDERevision, standardRevision)
	})
}

func TestPGTDEVaultChangeFor(t *testing.T) {
	t.Parallel()

	vault := tdeVaultSpec()
	tokenPath, caPath := pgtde.VaultCredentialPaths(vault)
	tempTokenPath, tempCAPath := pgtde.TempVaultCredentialPaths(vault)

	standardRevision, err := pgtde.VaultRevision(vault, tokenPath, caPath)
	assert.NilError(t, err)
	tempRevision, err := pgtde.VaultRevision(vault, tempTokenPath, tempCAPath)
	assert.NilError(t, err)

	clusterWith := func(revision string) *v1beta1.PostgresCluster {
		cluster := &v1beta1.PostgresCluster{}
		cluster.Spec.Extensions.PGTDE = v1beta1.PGTDESpec{
			Enabled: true,
			Vault:   tdeVaultSpec(),
		}
		cluster.Status.PGTDERevision = revision
		return cluster
	}

	for _, tc := range []struct {
		name     string
		revision string
		expected pgtde.Phase
	}{
		{"InitialSetup", "", pgtde.InitialSetup},
		{"Configured", standardRevision, pgtde.Configured},
		{"Finalize", tempRevision, pgtde.Finalize},
		{"StageCredentials", "a-revision-from-another-vault", pgtde.StageCredentials},
	} {
		t.Run(tc.name, func(t *testing.T) {
			change, err := pgtde.VaultChangeFor(clusterWith(tc.revision))
			assert.NilError(t, err)
			assert.Equal(t, change.Phase, tc.expected)

			assert.Equal(t, change.TokenPath, tokenPath)
			assert.Equal(t, change.CAPath, caPath)
			assert.Equal(t, change.TempTokenPath, tempTokenPath)
			assert.Equal(t, change.TempCAPath, tempCAPath)
			assert.Equal(t, change.StandardRevision, standardRevision)
			assert.Equal(t, change.TempRevision, tempRevision)
		})
	}

	// reconcileInstance holds the Pods' vault volume in exactly one phase.
	// Holding in any other one pins the StatefulSet to credentials the provider
	// has already moved off of, and the Pods never roll.
	t.Run("HoldsTheVolumeInOnePhase", func(t *testing.T) {
		held := map[pgtde.Phase]bool{}
		for _, revision := range []string{
			"", standardRevision, tempRevision, "a-revision-from-another-vault",
		} {
			change, err := pgtde.VaultChangeFor(clusterWith(revision))
			assert.NilError(t, err)
			held[change.Phase] = change.Phase == pgtde.StageCredentials
		}

		assert.DeepEqual(t, held, map[pgtde.Phase]bool{
			pgtde.InitialSetup:     false,
			pgtde.Configured:       false,
			pgtde.StageCredentials: true,
			pgtde.Finalize:         false,
		})
	})
}
