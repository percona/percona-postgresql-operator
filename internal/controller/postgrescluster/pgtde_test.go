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
	"gotest.tools/v3/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/pgtde"
	"github.com/percona/percona-postgresql-operator/v2/internal/testing/events"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
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

	base, err := pgTDEVaultRevision(vault, tokenPath, caPath)
	assert.NilError(t, err)
	assert.Assert(t, base != "")

	t.Run("Deterministic", func(t *testing.T) {
		again, err := pgTDEVaultRevision(tdeVaultSpec(), tokenPath, caPath)
		assert.NilError(t, err)
		assert.Equal(t, base, again, "same input should hash the same")
	})

	t.Run("TempPathsDiffer", func(t *testing.T) {
		tempToken, tempCA := pgtde.TempVaultCredentialPaths(vault)
		temp, err := pgTDEVaultRevision(vault, tempToken, tempCA)
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
			rev, err := pgTDEVaultRevision(changed, token, ca)
			assert.NilError(t, err)
			assert.Assert(t, rev != base, "changing %s should change the revision", tc.name)
		})
	}
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

		preserveOldTDEVolume(podSpec, runnerWith(oldVolume, corev1.Volume{Name: "pgdata"}))

		assert.Equal(t, len(podSpec.Volumes), 3, "no volumes should be added or removed")
		assert.Equal(t, podSpec.Volumes[0].Name, "pgdata", "unrelated volumes keep their order")
		assert.Equal(t, podSpec.Volumes[2].Name, "tmp")
		assert.Equal(t,
			podSpec.Volumes[1].Projected.Sources[0].Secret.Name, "old-secret",
			"the running Pod's TDE volume should be kept")
	})

	t.Run("NoVolumeInRunner", func(t *testing.T) {
		podSpec := &corev1.PodSpec{Volumes: []corev1.Volume{newVolume}}

		preserveOldTDEVolume(podSpec, runnerWith(corev1.Volume{Name: "pgdata"}))

		assert.Equal(t,
			podSpec.Volumes[0].Projected.Sources[0].Secret.Name, "new-secret",
			"without an old volume to preserve the intent is left alone")
	})

	t.Run("NoVolumeInPodSpec", func(t *testing.T) {
		podSpec := &corev1.PodSpec{Volumes: []corev1.Volume{{Name: "pgdata"}}}

		preserveOldTDEVolume(podSpec, runnerWith(oldVolume))

		assert.Equal(t, len(podSpec.Volumes), 1,
			"the old volume should not be grafted onto a Pod that has none")
		assert.Equal(t, podSpec.Volumes[0].Name, "pgdata")
	})
}

func TestFetchSecretToTempFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "vault-secret"},
		Data:       map[string][]byte{"token": []byte("hvs.sometoken")},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "pgc1-instance1-abcd-0"},
	}
	ref := v1beta1.PGTDESecretObjectReference{Name: "vault-secret", Key: "token"}

	t.Run("WritesSecretToPod", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().WithObjects(secret).Build()

		assert.NilError(t, fetchSecretToTempFile(ctx, k8s,
			execRecorder(&calls, nil), "ns1", ref, pod,
			naming.ContainerDatabase, pgtde.TempTokenPath))

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
		var calls []execCall
		k8s := fake.NewClientBuilder().WithObjects(secret).Build()

		// The container reports fewer bytes on disk than were sent.
		err := fetchSecretToTempFile(ctx, k8s,
			func(ctx context.Context, namespace, pod, container string,
				stdin io.Reader, stdout, stderr io.Writer, command ...string,
			) error {
				_, _ = io.WriteString(stdout, "4\n")
				return nil
			},
			"ns1", ref, pod, naming.ContainerDatabase, pgtde.TempTokenPath)

		assert.ErrorContains(t, err, "wrote 4 of 13 bytes",
			"a truncated token must not be accepted as written")
		assert.Equal(t, len(calls), 0)
	})

	t.Run("MissingSecret", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().Build()

		err := fetchSecretToTempFile(ctx, k8s,
			execRecorder(&calls, nil), "ns1", ref, pod,
			naming.ContainerDatabase, pgtde.TempTokenPath)

		assert.ErrorContains(t, err, "vault-secret")
		assert.Equal(t, len(calls), 0, "nothing should be written when the Secret is missing")
	})

	t.Run("MissingKey", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().WithObjects(secret).Build()

		err := fetchSecretToTempFile(ctx, k8s,
			execRecorder(&calls, nil), "ns1",
			v1beta1.PGTDESecretObjectReference{Name: "vault-secret", Key: "nope"},
			pod, naming.ContainerDatabase, pgtde.TempTokenPath)

		assert.ErrorContains(t, err, `key "nope" not found`)
		assert.Equal(t, len(calls), 0,
			"an empty file must not be written when the key is absent")
	})

	t.Run("ExecFails", func(t *testing.T) {
		var calls []execCall
		k8s := fake.NewClientBuilder().WithObjects(secret).Build()

		err := fetchSecretToTempFile(ctx, k8s,
			execRecorder(&calls, func(execCall) error {
				return errors.New("no such file or directory")
			}),
			"ns1", ref, pod, naming.ContainerDatabase, pgtde.TempTokenPath)

		assert.ErrorContains(t, err, pgtde.TempTokenPath)
		assert.ErrorContains(t, err, "no such file or directory")
	})
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

	standardRevision, err := pgTDEVaultRevision(vault, tokenPath, caPath)
	assert.NilError(t, err)
	tempRevision, err := pgTDEVaultRevision(vault, tempTokenPath, tempCAPath)
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

		assert.Equal(t, len(calls), 1,
			"staged credentials must not outlive the feature being disabled")
		assert.Assert(t, strings.Contains(calls[0].command[2], pgtde.TempTokenPath))
		assert.Assert(t, strings.Contains(calls[0].command[2], pgtde.TempCAPath))
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
		assert.Equal(t, len(calls), 1, "staged credentials are swept here too")
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

	t.Run("PhaseTwoCleanupFailure", func(t *testing.T) {
		var calls []execCall
		cluster := newCluster()
		cluster.Status.PGTDERevision = tempRevision

		r := &Reconciler{
			Client:   fake.NewClientBuilder().WithObjects(secret).Build(),
			Recorder: events.NewRecorder(t, runtime.Scheme),
			PodExec: execRecorder(&calls, func(call execCall) error {
				if strings.HasPrefix(call.command[len(call.command)-1], "rm -f") {
					return errors.New("permission denied")
				}
				return nil
			}),
		}
		observed := &observedInstances{forCluster: []*Instance{
			tdeInstance(map[string]string{naming.TDEInstalledAnnotation: "true"}),
		}}

		patched := 0
		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, observed,
			func() error { patched++; return nil }))

		// The rotation itself succeeded, so it must not be repeated...
		assert.Equal(t, cluster.Status.PGTDERevision, standardRevision)
		// ...but a vault token left in plaintext on the PersistentVolume is
		// not something to swallow.
		assertTDEProviderCondition(t, cluster, metav1.ConditionFalse,
			pgTDEReasonCredentialsNotRemoved)
		assertEvent(t, r.Recorder, "PGTDECredentialCleanupFailed")
		assert.Equal(t, patched, 1)

		// The next reconcile has nothing to change, but retries the removal.
		before := len(calls)
		assert.NilError(t, r.reconcilePGTDEProviders(ctx, cluster, observed,
			func() error { patched++; return nil }))
		assert.Equal(t, len(calls), before+1, "cleanup should be retried")
		assert.Assert(t, strings.Contains(calls[before].command[2], tempTokenPath))
	})

	t.Run("CleanupRetrySucceeds", func(t *testing.T) {
		var calls []execCall
		fail := true
		cluster := newCluster()
		cluster.Status.PGTDERevision = standardRevision
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    v1beta1.PGTDEVaultProviderReady,
			Status:  metav1.ConditionFalse,
			Reason:  pgTDEReasonCredentialsNotRemoved,
			Message: "permission denied",
		})

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
