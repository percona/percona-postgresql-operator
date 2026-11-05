// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/percona/percona-postgresql-operator/v3/internal/feature"
	"github.com/percona/percona-postgresql-operator/v3/internal/naming"
	"github.com/percona/percona-postgresql-operator/v3/internal/testing/cmp"
	"github.com/percona/percona-postgresql-operator/v3/percona/version"
	"github.com/percona/percona-postgresql-operator/v3/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func TestDataVolumeMount(t *testing.T) {
	mount := DataVolumeMount()

	assert.DeepEqual(t, mount, corev1.VolumeMount{
		Name:      "postgres-data",
		MountPath: "/pgdata",
		ReadOnly:  false,
	})
}

func TestWALVolumeMount(t *testing.T) {
	mount := WALVolumeMount()

	assert.DeepEqual(t, mount, corev1.VolumeMount{
		Name:      "postgres-wal",
		MountPath: "/pgwal",
		ReadOnly:  false,
	})
}

func TestDownwardAPIVolumeMount(t *testing.T) {
	mount := DownwardAPIVolumeMount()

	assert.DeepEqual(t, mount, corev1.VolumeMount{
		Name:      "database-containerinfo",
		MountPath: "/etc/database-containerinfo",
		ReadOnly:  true,
	})
}

func TestTablespaceVolumeMount(t *testing.T) {
	mount := TablespaceVolumeMount("trial")

	assert.DeepEqual(t, mount, corev1.VolumeMount{
		Name:      "tablespace-trial",
		MountPath: "/tablespaces/trial",
		ReadOnly:  false,
	})
}

func TestInstancePod(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cluster := new(v1beta1.PostgresCluster)
	err := cluster.Default(context.Background(), nil)
	assert.NilError(t, err)
	cluster.Spec.ImagePullPolicy = corev1.PullAlways
	cluster.Spec.PostgresVersion = 11
	cluster.SetLabels(map[string]string{
		naming.LabelVersion: version.Version(),
	})

	dataVolume := new(corev1.PersistentVolumeClaim)
	dataVolume.Name = "datavol"

	instance := new(v1beta1.PostgresInstanceSetSpec)
	instance.Resources.Requests = corev1.ResourceList{"cpu": resource.MustParse("9m")}
	instance.Sidecars = &v1beta1.InstanceSidecars{
		ReplicaCertCopy: &v1beta1.Sidecar{
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{"cpu": resource.MustParse("21m")},
			},
		},
	}

	serverSecretProjection := &corev1.SecretProjection{
		LocalObjectReference: corev1.LocalObjectReference{Name: "srv-secret"},
		Items: []corev1.KeyToPath{
			{
				Key:  naming.ReplicationCert,
				Path: naming.ReplicationCert,
			},
			{
				Key:  naming.ReplicationPrivateKey,
				Path: naming.ReplicationPrivateKey,
			},
			{
				Key:  naming.ReplicationCACert,
				Path: naming.ReplicationCACert,
			},
		},
	}

	clientSecretProjection := &corev1.SecretProjection{
		LocalObjectReference: corev1.LocalObjectReference{Name: "repl-secret"},
		Items: []corev1.KeyToPath{
			{
				Key:  naming.ReplicationCert,
				Path: naming.ReplicationCertPath,
			},
			{
				Key:  naming.ReplicationPrivateKey,
				Path: naming.ReplicationPrivateKeyPath,
			},
		},
	}

	// without WAL volume nor WAL volume spec
	pod := new(corev1.PodSpec)
	InstancePod(ctx, cluster, instance,
		serverSecretProjection, clientSecretProjection, nil, dataVolume, nil, nil, pod)

	assert.Assert(t, cmp.MarshalMatches(pod, `
containers:
- env:
  - name: PGDATA
    value: /pgdata/pg11
  - name: PGHOST
    value: /tmp/postgres
  - name: PGPORT
    value: "5432"
  - name: KRB5_CONFIG
    value: /etc/postgres/krb5.conf
  - name: KRB5RCACHEDIR
    value: /tmp
  - name: LDAPTLS_CACERT
    value: /etc/postgres/ldap/ca.crt
  - name: LC_ALL
    value: en_US.utf-8
  - name: LANG
    value: en_US.utf-8
  imagePullPolicy: Always
  name: database
  ports:
  - containerPort: 5432
    name: postgres
    protocol: TCP
  resources:
    requests:
      cpu: 9m
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
  - mountPath: /pgconf/tls
    name: cert-volume
    readOnly: true
  - mountPath: /pgdata
    name: postgres-data
  - mountPath: /etc/database-containerinfo
    name: database-containerinfo
    readOnly: true
- command:
  - bash
  - -ceu
  - --
  - |-
    monitor() {
    # Parameters for curl when managing autogrow annotation.
    APISERVER="https://kubernetes.default.svc"
    SERVICEACCOUNT="/var/run/secrets/kubernetes.io/serviceaccount"
    NAMESPACE=$(cat ${SERVICEACCOUNT}/namespace)
    TOKEN=$(cat ${SERVICEACCOUNT}/token)
    CACERT=${SERVICEACCOUNT}/ca.crt

    declare -r directory="/pgconf/tls"
    exec {fd}<> <(:||:)
    while read -r -t 5 -u "${fd}" ||:; do
      # Manage replication certificate.
      if [[ "${directory}" -nt "/proc/self/fd/${fd}" ]] &&
        install -D --mode=0600 -t "/tmp/replication" "${directory}"/{replication/tls.crt,replication/tls.key,replication/ca.crt} &&
        pkill -HUP --exact --parent=1 postgres
      then
        exec {fd}>&- && exec {fd}<> <(:||:)
        stat --format='Loaded certificates dated %y' "${directory}"
      fi

    done
    }; export -f monitor; exec -a "$0" bash -ceu monitor
  - replication-cert-copy
  imagePullPolicy: Always
  name: replication-cert-copy
  resources:
    requests:
      cpu: 21m
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
  - mountPath: /pgconf/tls
    name: cert-volume
    readOnly: true
  - mountPath: /pgdata
    name: postgres-data
initContainers:
- command:
  - bash
  - -ceu
  - --
  - |-
    declare -r expected_major_version="$1" pgwal_directory="$2" pgbrLog_directory="$3"
    permissions() { while [[ -n "$1" ]]; do set "${1%/*}" "$@"; done; shift; stat -Lc '%A %4u %4g %n' "$@"; }
    halt() { local rc=$?; >&2 echo "$@"; exit "${rc/#0/1}"; }
    results() { printf '::postgres-operator: %s::%s\n' "$@"; }
    recreate() (
      local tmp; tmp=$(mktemp -d -p "${1%/*}"); GLOBIGNORE='.:..'; set -x
      chmod "$2" "${tmp}"; mv "$1"/* "${tmp}"; rmdir "$1"; mv "${tmp}" "$1"
    )
    safelink() (
      local desired="$1" name="$2" current
      current=$(realpath "${name}")
      if [[ "${current}" == "${desired}" ]]; then return; fi
      set -x; mv --no-target-directory "${current}" "${desired}"
      ln --no-dereference --force --symbolic "${desired}" "${name}"
    )
    echo Initializing ...
    results 'uid' "$(id -u ||:)" 'gid' "$(id -G ||:)"
    if [[ "${pgwal_directory}" == *"pgwal/"* ]] && [[ ! -d "/pgwal/pgbackrest-spool" ]];then rm -rf "/pgdata/pgbackrest-spool" && mkdir -p "/pgwal/pgbackrest-spool" && ln --force --symbolic "/pgwal/pgbackrest-spool" "/pgdata/pgbackrest-spool";fi
    if [[ ! -e "/pgdata/pgbackrest-spool" ]];then rm -rf /pgdata/pgbackrest-spool;fi
    results 'postgres path' "$(command -v postgres ||:)"
    results 'postgres version' "${postgres_version:=$(postgres --version ||:)}"
    [[ "${postgres_version}" =~ ") ${expected_major_version}"($|[^0-9]) ]] ||
    halt Expected PostgreSQL version "${expected_major_version}"
    results 'config directory' "${PGDATA:?}"
    postgres_data_directory=$([[ -d "${PGDATA}" ]] && postgres -C data_directory || echo "${PGDATA}")
    results 'data directory' "${postgres_data_directory}"
    [[ "${postgres_data_directory}" == "${PGDATA}" ]] ||
    halt Expected matching config and data directories
    bootstrap_dir="${postgres_data_directory}_bootstrap"
    [[ -d "${bootstrap_dir}" ]] && results 'bootstrap directory' "${bootstrap_dir}"
    [[ -d "${bootstrap_dir}" ]] && postgres_data_directory="${bootstrap_dir}"
    if [[ ! -e "${postgres_data_directory}" || -O "${postgres_data_directory}" ]]; then
    install --directory --mode=0700 "${postgres_data_directory}"
    elif [[ -w "${postgres_data_directory}" && -g "${postgres_data_directory}" ]]; then
    recreate "${postgres_data_directory}" '0700'
    else (halt Permissions!); fi ||
    halt "$(permissions "${postgres_data_directory}" ||:)"
    results 'pgBackRest log directory' "${pgbrLog_directory}"
    install --directory --mode=0775 "${pgbrLog_directory}" ||
    halt "$(permissions "${pgbrLog_directory}" ||:)"
    install -D --mode=0600 -t "/tmp/replication" "/pgconf/tls/replication"/{tls.crt,tls.key,ca.crt}

    [[ -f "${postgres_data_directory}/PG_VERSION" ]] || exit 0
    results 'data version' "${postgres_data_version:=$(< "${postgres_data_directory}/PG_VERSION")}"
    [[ "${postgres_data_version}" == "${expected_major_version}" ]] ||
    halt Expected PostgreSQL data version "${expected_major_version}"
    [[ ! -f "${postgres_data_directory}/postgresql.conf" ]] &&
    touch "${postgres_data_directory}/postgresql.conf"
    safelink "${pgwal_directory}" "${postgres_data_directory}/pg_wal"
    results 'wal directory' "$(realpath "${postgres_data_directory}/pg_wal" ||:)"
    rm -f "${postgres_data_directory}/recovery.signal"
  - startup
  - "11"
  - /pgdata/pg11_wal
  - /pgdata/pgbackrest/log
  env:
  - name: PGDATA
    value: /pgdata/pg11
  - name: PGHOST
    value: /tmp/postgres
  - name: PGPORT
    value: "5432"
  - name: KRB5_CONFIG
    value: /etc/postgres/krb5.conf
  - name: KRB5RCACHEDIR
    value: /tmp
  - name: LDAPTLS_CACERT
    value: /etc/postgres/ldap/ca.crt
  - name: LC_ALL
    value: en_US.utf-8
  - name: LANG
    value: en_US.utf-8
  imagePullPolicy: Always
  name: postgres-startup
  resources:
    requests:
      cpu: 9m
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
  - mountPath: /pgconf/tls
    name: cert-volume
    readOnly: true
  - mountPath: /pgdata
    name: postgres-data
volumes:
- name: cert-volume
  projected:
    defaultMode: 384
    sources:
    - secret:
        items:
        - key: tls.crt
          path: tls.crt
        - key: tls.key
          path: tls.key
        - key: ca.crt
          path: ca.crt
        name: srv-secret
    - secret:
        items:
        - key: tls.crt
          path: replication/tls.crt
        - key: tls.key
          path: replication/tls.key
        name: repl-secret
- name: postgres-data
  persistentVolumeClaim:
    claimName: datavol
- downwardAPI:
    items:
    - path: cpu_limit
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: limits.cpu
    - path: cpu_request
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: requests.cpu
    - path: mem_limit
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: limits.memory
    - path: mem_request
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: requests.memory
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.labels
      path: labels
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.annotations
      path: annotations
  name: database-containerinfo
	`))

	t.Run("WithWALVolumeWithoutWALVolumeSpec", func(t *testing.T) {
		walVolume := new(corev1.PersistentVolumeClaim)
		walVolume.Name = "walvol"

		pod := new(corev1.PodSpec)
		InstancePod(ctx, cluster, instance,
			serverSecretProjection, clientSecretProjection, nil, dataVolume, walVolume, nil, pod)

		assert.Assert(t, len(pod.Containers) > 0)
		assert.Assert(t, len(pod.InitContainers) > 0)

		// Container has all mountPaths, including downwardAPI
		assert.Assert(t, cmp.MarshalMatches(pod.Containers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /etc/database-containerinfo
  name: database-containerinfo
  readOnly: true
- mountPath: /pgwal
  name: postgres-wal`), "expected WAL and downwardAPI mounts in %q container", pod.Containers[0].Name)

		// InitContainer has all mountPaths, except downwardAPI
		assert.Assert(t, cmp.MarshalMatches(pod.InitContainers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /pgwal
  name: postgres-wal`), "expected WAL mount, no downwardAPI mount in %q container", pod.InitContainers[0].Name)

		assert.Assert(t, cmp.MarshalMatches(pod.Volumes, `
- name: cert-volume
  projected:
    defaultMode: 384
    sources:
    - secret:
        items:
        - key: tls.crt
          path: tls.crt
        - key: tls.key
          path: tls.key
        - key: ca.crt
          path: ca.crt
        name: srv-secret
    - secret:
        items:
        - key: tls.crt
          path: replication/tls.crt
        - key: tls.key
          path: replication/tls.key
        name: repl-secret
- name: postgres-data
  persistentVolumeClaim:
    claimName: datavol
- downwardAPI:
    items:
    - path: cpu_limit
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: limits.cpu
    - path: cpu_request
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: requests.cpu
    - path: mem_limit
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: limits.memory
    - path: mem_request
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: requests.memory
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.labels
      path: labels
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.annotations
      path: annotations
  name: database-containerinfo
- name: postgres-wal
  persistentVolumeClaim:
    claimName: walvol
		`), "expected WAL volume")

		// Startup moves WAL files to data volume.
		assert.DeepEqual(t, pod.InitContainers[0].Command[4:],
			[]string{"startup", "11", "/pgdata/pg11_wal", "/pgdata/pgbackrest/log"})
	})

	t.Run("WithAdditionalConfigFiles", func(t *testing.T) {
		clusterWithConfig := cluster.DeepCopy()
		clusterWithConfig.Spec.Config = &v1beta1.PostgresConfigSpec{
			Files: []corev1.VolumeProjection{
				{
					Secret: &corev1.SecretProjection{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "keytab",
						},
					},
				},
			},
		}

		pod := new(corev1.PodSpec)
		InstancePod(ctx, clusterWithConfig, instance,
			serverSecretProjection, clientSecretProjection, nil, dataVolume, nil, nil, pod)

		assert.Assert(t, len(pod.Containers) > 0)
		assert.Assert(t, len(pod.InitContainers) > 0)

		// Container has all mountPaths, including downwardAPI,
		// and the postgres-config
		assert.Assert(t, cmp.MarshalMatches(pod.Containers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /etc/database-containerinfo
  name: database-containerinfo
  readOnly: true
- mountPath: /etc/postgres
  name: postgres-config
  readOnly: true`), "expected WAL and downwardAPI mounts in %q container", pod.Containers[0].Name)

		// InitContainer has all mountPaths, except downwardAPI and additionalConfig
		assert.Assert(t, cmp.MarshalMatches(pod.InitContainers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data`), "expected WAL mount, no downwardAPI mount in %q container", pod.InitContainers[0].Name)
	})

	t.Run("WithCustomSidecarContainer", func(t *testing.T) {
		sidecarInstance := new(v1beta1.PostgresInstanceSetSpec)
		sidecarInstance.Containers = []corev1.Container{
			{Name: "customsidecar1"},
		}

		t.Run("SidecarNotEnabled", func(t *testing.T) {
			InstancePod(ctx, cluster, sidecarInstance,
				serverSecretProjection, clientSecretProjection, nil, dataVolume, nil, nil, pod)

			assert.Equal(t, len(pod.Containers), 2, "expected 2 containers in Pod, got %d", len(pod.Containers))
		})

		t.Run("SidecarEnabled", func(t *testing.T) {
			gate := feature.NewGate()
			assert.NilError(t, gate.SetFromMap(map[string]bool{
				feature.InstanceSidecars: true,
			}))
			ctx := feature.NewContext(ctx, gate)

			InstancePod(ctx, cluster, sidecarInstance,
				serverSecretProjection, clientSecretProjection, nil, dataVolume, nil, nil, pod)

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

	t.Run("WithTablespaces", func(t *testing.T) {
		clusterWithTablespaces := cluster.DeepCopy()
		clusterWithTablespaces.Spec.InstanceSets = []v1beta1.PostgresInstanceSetSpec{
			{
				TablespaceVolumes: []v1beta1.TablespaceVolume{
					{Name: "trial"},
					{Name: "castle"},
				},
			},
		}

		tablespaceVolume1 := new(corev1.PersistentVolumeClaim)
		tablespaceVolume1.Labels = map[string]string{
			"postgres-operator.crunchydata.com/data": "castle",
		}
		tablespaceVolume2 := new(corev1.PersistentVolumeClaim)
		tablespaceVolume2.Labels = map[string]string{
			"postgres-operator.crunchydata.com/data": "trial",
		}
		tablespaceVolumes := []*corev1.PersistentVolumeClaim{tablespaceVolume1, tablespaceVolume2}

		InstancePod(ctx, cluster, instance,
			serverSecretProjection, clientSecretProjection, nil, dataVolume, nil, tablespaceVolumes, pod)

		assert.Assert(t, cmp.MarshalMatches(pod.Containers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /etc/database-containerinfo
  name: database-containerinfo
  readOnly: true
- mountPath: /tablespaces/castle
  name: tablespace-castle
- mountPath: /tablespaces/trial
  name: tablespace-trial`), "expected tablespace mount(s) in %q container", pod.Containers[0].Name)

		// InitContainer has all mountPaths, except downwardAPI and additionalConfig
		assert.Assert(t, cmp.MarshalMatches(pod.InitContainers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /tablespaces/castle
  name: tablespace-castle
- mountPath: /tablespaces/trial
  name: tablespace-trial`), "expected tablespace mount(s) in %q container", pod.InitContainers[0].Name)
	})

	t.Run("WithWALVolumeWithWALVolumeSpec", func(t *testing.T) {
		walVolume := new(corev1.PersistentVolumeClaim)
		walVolume.Name = "walvol"

		instance := new(v1beta1.PostgresInstanceSetSpec)
		instance.WALVolumeClaimSpec = new(corev1.PersistentVolumeClaimSpec)

		pod := new(corev1.PodSpec)
		InstancePod(ctx, cluster, instance,
			serverSecretProjection, clientSecretProjection, nil, dataVolume, walVolume, nil, pod)

		assert.Assert(t, len(pod.Containers) > 0)
		assert.Assert(t, len(pod.InitContainers) > 0)

		assert.Assert(t, cmp.MarshalMatches(pod.Containers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /etc/database-containerinfo
  name: database-containerinfo
  readOnly: true
- mountPath: /pgwal
  name: postgres-wal`), "expected WAL and downwardAPI mounts in %q container", pod.Containers[0].Name)

		assert.Assert(t, cmp.MarshalMatches(pod.InitContainers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /pgwal
  name: postgres-wal`), "expected WAL mount, no downwardAPI mount in %q container", pod.InitContainers[0].Name)

		assert.Assert(t, cmp.MarshalMatches(pod.Volumes, `
- name: cert-volume
  projected:
    defaultMode: 384
    sources:
    - secret:
        items:
        - key: tls.crt
          path: tls.crt
        - key: tls.key
          path: tls.key
        - key: ca.crt
          path: ca.crt
        name: srv-secret
    - secret:
        items:
        - key: tls.crt
          path: replication/tls.crt
        - key: tls.key
          path: replication/tls.key
        name: repl-secret
- name: postgres-data
  persistentVolumeClaim:
    claimName: datavol
- downwardAPI:
    items:
    - path: cpu_limit
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: limits.cpu
    - path: cpu_request
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: requests.cpu
    - path: mem_limit
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: limits.memory
    - path: mem_request
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: requests.memory
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.labels
      path: labels
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.annotations
      path: annotations
  name: database-containerinfo
- name: postgres-wal
  persistentVolumeClaim:
    claimName: walvol
		`), "expected WAL volume")

		// Startup moves WAL files to WAL volume.
		assert.DeepEqual(t, pod.InitContainers[0].Command[4:],
			[]string{"startup", "11", "/pgwal/pg11_wal", "/pgdata/pgbackrest/log"})
	})

	// K8SPG-440
	t.Run("WithExtraVolumes", func(t *testing.T) {
		extraInstance := new(v1beta1.PostgresInstanceSetSpec)
		extraInstance.ExtraVolumes = []v1beta1.ExtraVolume{
			{
				Name: "fts-dicts",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "my-dicts"},
					},
				},
				Mounts: []v1beta1.ExtraVolumeMount{
					{MountPath: "/pgdata/dicts", ReadOnly: true},
				},
			},
			{
				Name: "extra-data",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "extra-storage",
					},
				},
				Mounts: []v1beta1.ExtraVolumeMount{
					{MountPath: "/pgdata/extra", SubPath: "sub"},
				},
			},
		}

		pod := new(corev1.PodSpec)
		InstancePod(ctx, cluster, extraInstance,
			serverSecretProjection, clientSecretProjection, nil, dataVolume, nil, nil, pod)

		// Extra volume mounts are appended to the postgres container, not the
		// startup init container.
		assert.Assert(t, cmp.MarshalMatches(pod.Containers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /etc/database-containerinfo
  name: database-containerinfo
  readOnly: true
- mountPath: /pgdata/dicts
  name: fts-dicts
  readOnly: true
- mountPath: /pgdata/extra
  name: extra-data
  subPath: sub`), "expected extra volume mounts in %q container", pod.Containers[0].Name)

		assert.Assert(t, cmp.MarshalMatches(pod.InitContainers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data`), "expected no extra volume mounts in %q container", pod.InitContainers[0].Name)

		assert.Assert(t, cmp.MarshalMatches(pod.Volumes, `
- name: cert-volume
  projected:
    defaultMode: 384
    sources:
    - secret:
        items:
        - key: tls.crt
          path: tls.crt
        - key: tls.key
          path: tls.key
        - key: ca.crt
          path: ca.crt
        name: srv-secret
    - secret:
        items:
        - key: tls.crt
          path: replication/tls.crt
        - key: tls.key
          path: replication/tls.key
        name: repl-secret
- name: postgres-data
  persistentVolumeClaim:
    claimName: datavol
- downwardAPI:
    items:
    - path: cpu_limit
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: limits.cpu
    - path: cpu_request
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: requests.cpu
    - path: mem_limit
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: limits.memory
    - path: mem_request
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: requests.memory
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.labels
      path: labels
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.annotations
      path: annotations
  name: database-containerinfo
- configMap:
    name: my-dicts
  name: fts-dicts
- name: extra-data
  persistentVolumeClaim:
    claimName: extra-storage`), "expected extra volumes appended to the pod")
	})

	// K8SPG-440
	t.Run("WithExtraVolumesOldVersion", func(t *testing.T) {
		oldCluster := cluster.DeepCopy()
		oldCluster.SetLabels(map[string]string{naming.LabelVersion: "3.0.0"})

		extraInstance := new(v1beta1.PostgresInstanceSetSpec)
		extraInstance.ExtraVolumes = []v1beta1.ExtraVolume{
			{
				Name: "fts-dicts",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "my-dicts"},
					},
				},
				Mounts: []v1beta1.ExtraVolumeMount{
					{MountPath: "/pgdata/dicts"},
				},
			},
		}

		pod := new(corev1.PodSpec)
		InstancePod(ctx, oldCluster, extraInstance,
			serverSecretProjection, clientSecretProjection, nil, dataVolume, nil, nil, pod)

		for _, m := range pod.Containers[0].VolumeMounts {
			assert.Assert(t, m.Name != "fts-dicts", "extra volumes must not be mounted before 3.1.0")
		}
		for _, v := range pod.Volumes {
			assert.Assert(t, v.Name != "fts-dicts", "extra volumes must not be added before 3.1.0")
		}
	})
}

func TestInstancePodAllowVolumeGrow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	features := feature.NewGate()
	_ = features.SetFromMap(map[string]bool{
		feature.AutoGrowVolumes: true,
	})
	ctx = feature.NewContext(ctx, features)

	cluster := new(v1beta1.PostgresCluster)
	err := cluster.Default(context.Background(), nil)
	assert.NilError(t, err)
	cluster.Spec.ImagePullPolicy = corev1.PullAlways
	cluster.Spec.PostgresVersion = 11
	cluster.SetLabels(map[string]string{
		naming.LabelVersion: version.Version(),
	})

	dataVolume := new(corev1.PersistentVolumeClaim)
	dataVolume.Name = "datavol"

	instance := new(v1beta1.PostgresInstanceSetSpec)
	instance.Resources.Requests = corev1.ResourceList{"cpu": resource.MustParse("9m")}
	instance.Sidecars = &v1beta1.InstanceSidecars{
		ReplicaCertCopy: &v1beta1.Sidecar{
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{"cpu": resource.MustParse("21m")},
			},
		},
	}

	serverSecretProjection := &corev1.SecretProjection{
		LocalObjectReference: corev1.LocalObjectReference{Name: "srv-secret"},
		Items: []corev1.KeyToPath{
			{
				Key:  naming.ReplicationCert,
				Path: naming.ReplicationCert,
			},
			{
				Key:  naming.ReplicationPrivateKey,
				Path: naming.ReplicationPrivateKey,
			},
			{
				Key:  naming.ReplicationCACert,
				Path: naming.ReplicationCACert,
			},
		},
	}

	clientSecretProjection := &corev1.SecretProjection{
		LocalObjectReference: corev1.LocalObjectReference{Name: "repl-secret"},
		Items: []corev1.KeyToPath{
			{
				Key:  naming.ReplicationCert,
				Path: naming.ReplicationCertPath,
			},
			{
				Key:  naming.ReplicationPrivateKey,
				Path: naming.ReplicationPrivateKeyPath,
			},
		},
	}

	// without WAL volume nor WAL volume spec
	pod := new(corev1.PodSpec)
	InstancePod(ctx, cluster, instance,
		serverSecretProjection, clientSecretProjection, nil, dataVolume, nil, nil, pod)

	assert.Assert(t, cmp.MarshalMatches(pod, `
containers:
- env:
  - name: PGDATA
    value: /pgdata/pg11
  - name: PGHOST
    value: /tmp/postgres
  - name: PGPORT
    value: "5432"
  - name: KRB5_CONFIG
    value: /etc/postgres/krb5.conf
  - name: KRB5RCACHEDIR
    value: /tmp
  - name: LDAPTLS_CACERT
    value: /etc/postgres/ldap/ca.crt
  - name: LC_ALL
    value: en_US.utf-8
  - name: LANG
    value: en_US.utf-8
  imagePullPolicy: Always
  name: database
  ports:
  - containerPort: 5432
    name: postgres
    protocol: TCP
  resources:
    requests:
      cpu: 9m
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
  - mountPath: /pgconf/tls
    name: cert-volume
    readOnly: true
  - mountPath: /pgdata
    name: postgres-data
  - mountPath: /etc/database-containerinfo
    name: database-containerinfo
    readOnly: true
- command:
  - bash
  - -ceu
  - --
  - |-
    monitor() {
    # Parameters for curl when managing autogrow annotation.
    APISERVER="https://kubernetes.default.svc"
    SERVICEACCOUNT="/var/run/secrets/kubernetes.io/serviceaccount"
    NAMESPACE=$(cat ${SERVICEACCOUNT}/namespace)
    TOKEN=$(cat ${SERVICEACCOUNT}/token)
    CACERT=${SERVICEACCOUNT}/ca.crt

    declare -r directory="/pgconf/tls"
    exec {fd}<> <(:||:)
    while read -r -t 5 -u "${fd}" ||:; do
      # Manage replication certificate.
      if [[ "${directory}" -nt "/proc/self/fd/${fd}" ]] &&
        install -D --mode=0600 -t "/tmp/replication" "${directory}"/{replication/tls.crt,replication/tls.key,replication/ca.crt} &&
        pkill -HUP --exact --parent=1 postgres
      then
        exec {fd}>&- && exec {fd}<> <(:||:)
        stat --format='Loaded certificates dated %y' "${directory}"
      fi

      # Manage autogrow annotation.
      # Return size in Mebibytes.
      size=$(df --human-readable --block-size=M /pgdata | awk 'FNR == 2 {print $2}')
      use=$(df --human-readable /pgdata | awk 'FNR == 2 {print $5}')
      sizeInt="${size//M/}"
      # Use the sed punctuation class, because the shell will not accept the percent sign in an expansion.
      useInt=$(echo $use | sed 's/[[:punct:]]//g')
      triggerExpansion="$((useInt > 75))"
      if [ $triggerExpansion -eq 1 ]; then
        newSize="$(((sizeInt / 2)+sizeInt))"
        newSizeMi="${newSize}Mi"
        d='[{"op": "add", "path": "/metadata/annotations/suggested-pgdata-pvc-size", "value": "'"$newSizeMi"'"}]'
        curl --cacert ${CACERT} --header "Authorization: Bearer ${TOKEN}" -XPATCH "${APISERVER}/api/v1/namespaces/${NAMESPACE}/pods/${HOSTNAME}?fieldManager=kubectl-annotate" -H "Content-Type: application/json-patch+json" --data "$d"
      fi
    done
    }; export -f monitor; exec -a "$0" bash -ceu monitor
  - replication-cert-copy
  imagePullPolicy: Always
  name: replication-cert-copy
  resources:
    requests:
      cpu: 21m
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
  - mountPath: /pgconf/tls
    name: cert-volume
    readOnly: true
  - mountPath: /pgdata
    name: postgres-data
initContainers:
- command:
  - bash
  - -ceu
  - --
  - |-
    declare -r expected_major_version="$1" pgwal_directory="$2" pgbrLog_directory="$3"
    permissions() { while [[ -n "$1" ]]; do set "${1%/*}" "$@"; done; shift; stat -Lc '%A %4u %4g %n' "$@"; }
    halt() { local rc=$?; >&2 echo "$@"; exit "${rc/#0/1}"; }
    results() { printf '::postgres-operator: %s::%s\n' "$@"; }
    recreate() (
      local tmp; tmp=$(mktemp -d -p "${1%/*}"); GLOBIGNORE='.:..'; set -x
      chmod "$2" "${tmp}"; mv "$1"/* "${tmp}"; rmdir "$1"; mv "${tmp}" "$1"
    )
    safelink() (
      local desired="$1" name="$2" current
      current=$(realpath "${name}")
      if [[ "${current}" == "${desired}" ]]; then return; fi
      set -x; mv --no-target-directory "${current}" "${desired}"
      ln --no-dereference --force --symbolic "${desired}" "${name}"
    )
    echo Initializing ...
    results 'uid' "$(id -u ||:)" 'gid' "$(id -G ||:)"
    if [[ "${pgwal_directory}" == *"pgwal/"* ]] && [[ ! -d "/pgwal/pgbackrest-spool" ]];then rm -rf "/pgdata/pgbackrest-spool" && mkdir -p "/pgwal/pgbackrest-spool" && ln --force --symbolic "/pgwal/pgbackrest-spool" "/pgdata/pgbackrest-spool";fi
    if [[ ! -e "/pgdata/pgbackrest-spool" ]];then rm -rf /pgdata/pgbackrest-spool;fi
    results 'postgres path' "$(command -v postgres ||:)"
    results 'postgres version' "${postgres_version:=$(postgres --version ||:)}"
    [[ "${postgres_version}" =~ ") ${expected_major_version}"($|[^0-9]) ]] ||
    halt Expected PostgreSQL version "${expected_major_version}"
    results 'config directory' "${PGDATA:?}"
    postgres_data_directory=$([[ -d "${PGDATA}" ]] && postgres -C data_directory || echo "${PGDATA}")
    results 'data directory' "${postgres_data_directory}"
    [[ "${postgres_data_directory}" == "${PGDATA}" ]] ||
    halt Expected matching config and data directories
    bootstrap_dir="${postgres_data_directory}_bootstrap"
    [[ -d "${bootstrap_dir}" ]] && results 'bootstrap directory' "${bootstrap_dir}"
    [[ -d "${bootstrap_dir}" ]] && postgres_data_directory="${bootstrap_dir}"
    if [[ ! -e "${postgres_data_directory}" || -O "${postgres_data_directory}" ]]; then
    install --directory --mode=0700 "${postgres_data_directory}"
    elif [[ -w "${postgres_data_directory}" && -g "${postgres_data_directory}" ]]; then
    recreate "${postgres_data_directory}" '0700'
    else (halt Permissions!); fi ||
    halt "$(permissions "${postgres_data_directory}" ||:)"
    results 'pgBackRest log directory' "${pgbrLog_directory}"
    install --directory --mode=0775 "${pgbrLog_directory}" ||
    halt "$(permissions "${pgbrLog_directory}" ||:)"
    install -D --mode=0600 -t "/tmp/replication" "/pgconf/tls/replication"/{tls.crt,tls.key,ca.crt}

    [[ -f "${postgres_data_directory}/PG_VERSION" ]] || exit 0
    results 'data version' "${postgres_data_version:=$(< "${postgres_data_directory}/PG_VERSION")}"
    [[ "${postgres_data_version}" == "${expected_major_version}" ]] ||
    halt Expected PostgreSQL data version "${expected_major_version}"
    [[ ! -f "${postgres_data_directory}/postgresql.conf" ]] &&
    touch "${postgres_data_directory}/postgresql.conf"
    safelink "${pgwal_directory}" "${postgres_data_directory}/pg_wal"
    results 'wal directory' "$(realpath "${postgres_data_directory}/pg_wal" ||:)"
    rm -f "${postgres_data_directory}/recovery.signal"
  - startup
  - "11"
  - /pgdata/pg11_wal
  - /pgdata/pgbackrest/log
  env:
  - name: PGDATA
    value: /pgdata/pg11
  - name: PGHOST
    value: /tmp/postgres
  - name: PGPORT
    value: "5432"
  - name: KRB5_CONFIG
    value: /etc/postgres/krb5.conf
  - name: KRB5RCACHEDIR
    value: /tmp
  - name: LDAPTLS_CACERT
    value: /etc/postgres/ldap/ca.crt
  - name: LC_ALL
    value: en_US.utf-8
  - name: LANG
    value: en_US.utf-8
  imagePullPolicy: Always
  name: postgres-startup
  resources:
    requests:
      cpu: 9m
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
  - mountPath: /pgconf/tls
    name: cert-volume
    readOnly: true
  - mountPath: /pgdata
    name: postgres-data
volumes:
- name: cert-volume
  projected:
    defaultMode: 384
    sources:
    - secret:
        items:
        - key: tls.crt
          path: tls.crt
        - key: tls.key
          path: tls.key
        - key: ca.crt
          path: ca.crt
        name: srv-secret
    - secret:
        items:
        - key: tls.crt
          path: replication/tls.crt
        - key: tls.key
          path: replication/tls.key
        name: repl-secret
- name: postgres-data
  persistentVolumeClaim:
    claimName: datavol
- downwardAPI:
    items:
    - path: cpu_limit
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: limits.cpu
    - path: cpu_request
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: requests.cpu
    - path: mem_limit
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: limits.memory
    - path: mem_request
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: requests.memory
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.labels
      path: labels
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.annotations
      path: annotations
  name: database-containerinfo
	`))

	t.Run("WithWALVolumeWithoutWALVolumeSpec", func(t *testing.T) {
		walVolume := new(corev1.PersistentVolumeClaim)
		walVolume.Name = "walvol"

		pod := new(corev1.PodSpec)
		InstancePod(ctx, cluster, instance,
			serverSecretProjection, clientSecretProjection, nil, dataVolume, walVolume, nil, pod)

		assert.Assert(t, len(pod.Containers) > 0)
		assert.Assert(t, len(pod.InitContainers) > 0)

		// Container has all mountPaths, including downwardAPI
		assert.Assert(t, cmp.MarshalMatches(pod.Containers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /etc/database-containerinfo
  name: database-containerinfo
  readOnly: true
- mountPath: /pgwal
  name: postgres-wal`), "expected WAL and downwardAPI mounts in %q container", pod.Containers[0].Name)

		// InitContainer has all mountPaths, except downwardAPI
		assert.Assert(t, cmp.MarshalMatches(pod.InitContainers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /pgwal
  name: postgres-wal`), "expected WAL mount, no downwardAPI mount in %q container", pod.InitContainers[0].Name)

		assert.Assert(t, cmp.MarshalMatches(pod.Volumes, `
- name: cert-volume
  projected:
    defaultMode: 384
    sources:
    - secret:
        items:
        - key: tls.crt
          path: tls.crt
        - key: tls.key
          path: tls.key
        - key: ca.crt
          path: ca.crt
        name: srv-secret
    - secret:
        items:
        - key: tls.crt
          path: replication/tls.crt
        - key: tls.key
          path: replication/tls.key
        name: repl-secret
- name: postgres-data
  persistentVolumeClaim:
    claimName: datavol
- downwardAPI:
    items:
    - path: cpu_limit
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: limits.cpu
    - path: cpu_request
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: requests.cpu
    - path: mem_limit
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: limits.memory
    - path: mem_request
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: requests.memory
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.labels
      path: labels
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.annotations
      path: annotations
  name: database-containerinfo
- name: postgres-wal
  persistentVolumeClaim:
    claimName: walvol
		`), "expected WAL volume")

		// Startup moves WAL files to data volume.
		assert.DeepEqual(t, pod.InitContainers[0].Command[4:],
			[]string{"startup", "11", "/pgdata/pg11_wal", "/pgdata/pgbackrest/log"})
	})

	t.Run("WithAdditionalConfigFiles", func(t *testing.T) {
		clusterWithConfig := cluster.DeepCopy()
		clusterWithConfig.Spec.Config = &v1beta1.PostgresConfigSpec{
			Files: []corev1.VolumeProjection{
				{
					Secret: &corev1.SecretProjection{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "keytab",
						},
					},
				},
			},
		}

		pod := new(corev1.PodSpec)
		InstancePod(ctx, clusterWithConfig, instance,
			serverSecretProjection, clientSecretProjection, nil, dataVolume, nil, nil, pod)

		assert.Assert(t, len(pod.Containers) > 0)
		assert.Assert(t, len(pod.InitContainers) > 0)

		// Container has all mountPaths, including downwardAPI,
		// and the postgres-config
		assert.Assert(t, cmp.MarshalMatches(pod.Containers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /etc/database-containerinfo
  name: database-containerinfo
  readOnly: true
- mountPath: /etc/postgres
  name: postgres-config
  readOnly: true`), "expected WAL and downwardAPI mounts in %q container", pod.Containers[0].Name)

		// InitContainer has all mountPaths, except downwardAPI and additionalConfig
		assert.Assert(t, cmp.MarshalMatches(pod.InitContainers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data`), "expected WAL mount, no downwardAPI mount in %q container", pod.InitContainers[0].Name)
	})

	t.Run("WithCustomSidecarContainer", func(t *testing.T) {
		sidecarInstance := new(v1beta1.PostgresInstanceSetSpec)
		sidecarInstance.Containers = []corev1.Container{
			{Name: "customsidecar1"},
		}

		t.Run("SidecarNotEnabled", func(t *testing.T) {
			InstancePod(ctx, cluster, sidecarInstance,
				serverSecretProjection, clientSecretProjection, nil, dataVolume, nil, nil, pod)

			assert.Equal(t, len(pod.Containers), 2, "expected 2 containers in Pod, got %d", len(pod.Containers))
		})

		t.Run("SidecarEnabled", func(t *testing.T) {
			gate := feature.NewGate()
			assert.NilError(t, gate.SetFromMap(map[string]bool{
				feature.InstanceSidecars: true,
			}))
			ctx := feature.NewContext(ctx, gate)

			InstancePod(ctx, cluster, sidecarInstance,
				serverSecretProjection, clientSecretProjection, nil, dataVolume, nil, nil, pod)

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

	t.Run("WithTablespaces", func(t *testing.T) {
		clusterWithTablespaces := cluster.DeepCopy()
		clusterWithTablespaces.Spec.InstanceSets = []v1beta1.PostgresInstanceSetSpec{
			{
				TablespaceVolumes: []v1beta1.TablespaceVolume{
					{Name: "trial"},
					{Name: "castle"},
				},
			},
		}

		tablespaceVolume1 := new(corev1.PersistentVolumeClaim)
		tablespaceVolume1.Labels = map[string]string{
			"postgres-operator.crunchydata.com/data": "castle",
		}
		tablespaceVolume2 := new(corev1.PersistentVolumeClaim)
		tablespaceVolume2.Labels = map[string]string{
			"postgres-operator.crunchydata.com/data": "trial",
		}
		tablespaceVolumes := []*corev1.PersistentVolumeClaim{tablespaceVolume1, tablespaceVolume2}

		InstancePod(ctx, cluster, instance,
			serverSecretProjection, clientSecretProjection, nil, dataVolume, nil, tablespaceVolumes, pod)

		assert.Assert(t, cmp.MarshalMatches(pod.Containers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /etc/database-containerinfo
  name: database-containerinfo
  readOnly: true
- mountPath: /tablespaces/castle
  name: tablespace-castle
- mountPath: /tablespaces/trial
  name: tablespace-trial`), "expected tablespace mount(s) in %q container", pod.Containers[0].Name)

		// InitContainer has all mountPaths, except downwardAPI and additionalConfig
		assert.Assert(t, cmp.MarshalMatches(pod.InitContainers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /tablespaces/castle
  name: tablespace-castle
- mountPath: /tablespaces/trial
  name: tablespace-trial`), "expected tablespace mount(s) in %q container", pod.InitContainers[0].Name)
	})

	t.Run("WithWALVolumeWithWALVolumeSpec", func(t *testing.T) {
		walVolume := new(corev1.PersistentVolumeClaim)
		walVolume.Name = "walvol"

		instance := new(v1beta1.PostgresInstanceSetSpec)
		instance.WALVolumeClaimSpec = new(corev1.PersistentVolumeClaimSpec)

		pod := new(corev1.PodSpec)
		InstancePod(ctx, cluster, instance,
			serverSecretProjection, clientSecretProjection, nil, dataVolume, walVolume, nil, pod)

		assert.Assert(t, len(pod.Containers) > 0)
		assert.Assert(t, len(pod.InitContainers) > 0)

		assert.Assert(t, cmp.MarshalMatches(pod.Containers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /etc/database-containerinfo
  name: database-containerinfo
  readOnly: true
- mountPath: /pgwal
  name: postgres-wal`), "expected WAL and downwardAPI mounts in %q container", pod.Containers[0].Name)

		assert.Assert(t, cmp.MarshalMatches(pod.InitContainers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /pgwal
  name: postgres-wal`), "expected WAL mount, no downwardAPI mount in %q container", pod.InitContainers[0].Name)

		assert.Assert(t, cmp.MarshalMatches(pod.Volumes, `
- name: cert-volume
  projected:
    defaultMode: 384
    sources:
    - secret:
        items:
        - key: tls.crt
          path: tls.crt
        - key: tls.key
          path: tls.key
        - key: ca.crt
          path: ca.crt
        name: srv-secret
    - secret:
        items:
        - key: tls.crt
          path: replication/tls.crt
        - key: tls.key
          path: replication/tls.key
        name: repl-secret
- name: postgres-data
  persistentVolumeClaim:
    claimName: datavol
- downwardAPI:
    items:
    - path: cpu_limit
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: limits.cpu
    - path: cpu_request
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: requests.cpu
    - path: mem_limit
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: limits.memory
    - path: mem_request
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: requests.memory
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.labels
      path: labels
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.annotations
      path: annotations
  name: database-containerinfo
- name: postgres-wal
  persistentVolumeClaim:
    claimName: walvol
		`), "expected WAL volume")

		// Startup moves WAL files to WAL volume.
		assert.DeepEqual(t, pod.InitContainers[0].Command[4:],
			[]string{"startup", "11", "/pgwal/pg11_wal", "/pgdata/pgbackrest/log"})
	})

	t.Run("WithHugepages2Mi", func(t *testing.T) {
		clusterWithHugepages2Mi := cluster.DeepCopy()
		clusterWithHugepages2Mi.Spec.InstanceSets = []v1beta1.PostgresInstanceSetSpec{
			{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory:                  resource.MustParse("4Gi"),
						corev1.ResourceHugePagesPrefix + "2Mi": resource.MustParse("2Gi"),
					},
				},
			},
		}

		pod := new(corev1.PodSpec)
		InstancePod(ctx, clusterWithHugepages2Mi, instance,
			serverSecretProjection, clientSecretProjection, nil, dataVolume, nil, nil, pod)

		assert.Assert(t, cmp.MarshalMatches(pod.Volumes, `
- name: cert-volume
  projected:
    defaultMode: 384
    sources:
    - secret:
        items:
        - key: tls.crt
          path: tls.crt
        - key: tls.key
          path: tls.key
        - key: ca.crt
          path: ca.crt
        name: srv-secret
    - secret:
        items:
        - key: tls.crt
          path: replication/tls.crt
        - key: tls.key
          path: replication/tls.key
        name: repl-secret
- name: postgres-data
  persistentVolumeClaim:
    claimName: datavol
- downwardAPI:
    items:
    - path: cpu_limit
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: limits.cpu
    - path: cpu_request
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: requests.cpu
    - path: mem_limit
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: limits.memory
    - path: mem_request
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: requests.memory
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.labels
      path: labels
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.annotations
      path: annotations
  name: database-containerinfo
- emptyDir:
    medium: HugePages-2Mi
  name: hugepage-2mi
		`), "expected HugePages-2Mi volume")

		assert.Assert(t, cmp.MarshalMatches(pod.Containers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /etc/database-containerinfo
  name: database-containerinfo
  readOnly: true
- mountPath: /hugepages-2Mi
  name: hugepage-2mi`), "expected hugepage mount in %q container", pod.Containers[0].Name)
	})

	t.Run("WithHugepages1Gi", func(t *testing.T) {
		clusterWithHugepages1Gi := cluster.DeepCopy()
		clusterWithHugepages1Gi.Spec.InstanceSets = []v1beta1.PostgresInstanceSetSpec{
			{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory:                  resource.MustParse("4Gi"),
						corev1.ResourceHugePagesPrefix + "1Gi": resource.MustParse("2Gi"),
					},
				},
			},
		}

		pod := new(corev1.PodSpec)
		InstancePod(ctx, clusterWithHugepages1Gi, instance,
			serverSecretProjection, clientSecretProjection, nil, dataVolume, nil, nil, pod)

		assert.Assert(t, cmp.MarshalMatches(pod.Volumes, `
- name: cert-volume
  projected:
    defaultMode: 384
    sources:
    - secret:
        items:
        - key: tls.crt
          path: tls.crt
        - key: tls.key
          path: tls.key
        - key: ca.crt
          path: ca.crt
        name: srv-secret
    - secret:
        items:
        - key: tls.crt
          path: replication/tls.crt
        - key: tls.key
          path: replication/tls.key
        name: repl-secret
- name: postgres-data
  persistentVolumeClaim:
    claimName: datavol
- downwardAPI:
    items:
    - path: cpu_limit
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: limits.cpu
    - path: cpu_request
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: requests.cpu
    - path: mem_limit
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: limits.memory
    - path: mem_request
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: requests.memory
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.labels
      path: labels
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.annotations
      path: annotations
  name: database-containerinfo
- emptyDir:
    medium: HugePages-1Gi
  name: hugepage-1gi
		`), "expected HugePages-1Gi volume")

		assert.Assert(t, cmp.MarshalMatches(pod.Containers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /etc/database-containerinfo
  name: database-containerinfo
  readOnly: true
- mountPath: /hugepages-1Gi
  name: hugepage-1gi`), "expected hugepage mount in %q container", pod.Containers[0].Name)
	})

	t.Run("WithHugepages", func(t *testing.T) {
		clusterWithHugepages := cluster.DeepCopy()
		clusterWithHugepages.Spec.InstanceSets = []v1beta1.PostgresInstanceSetSpec{
			{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory:                  resource.MustParse("4Gi"),
						corev1.ResourceHugePagesPrefix + "2Mi": resource.MustParse("2Gi"),
						corev1.ResourceHugePagesPrefix + "1Gi": resource.MustParse("2Gi"),
					},
				},
			},
		}

		pod := new(corev1.PodSpec)
		InstancePod(ctx, clusterWithHugepages, instance,
			serverSecretProjection, clientSecretProjection, nil, dataVolume, nil, nil, pod)

		assert.Assert(t, cmp.MarshalMatches(pod.Volumes, `
- name: cert-volume
  projected:
    defaultMode: 384
    sources:
    - secret:
        items:
        - key: tls.crt
          path: tls.crt
        - key: tls.key
          path: tls.key
        - key: ca.crt
          path: ca.crt
        name: srv-secret
    - secret:
        items:
        - key: tls.crt
          path: replication/tls.crt
        - key: tls.key
          path: replication/tls.key
        name: repl-secret
- name: postgres-data
  persistentVolumeClaim:
    claimName: datavol
- downwardAPI:
    items:
    - path: cpu_limit
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: limits.cpu
    - path: cpu_request
      resourceFieldRef:
        containerName: database
        divisor: 1m
        resource: requests.cpu
    - path: mem_limit
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: limits.memory
    - path: mem_request
      resourceFieldRef:
        containerName: database
        divisor: 1Mi
        resource: requests.memory
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.labels
      path: labels
    - fieldRef:
        apiVersion: v1
        fieldPath: metadata.annotations
      path: annotations
  name: database-containerinfo
- emptyDir:
    medium: HugePages-2Mi
  name: hugepage-2mi
- emptyDir:
    medium: HugePages-1Gi
  name: hugepage-1gi
		`), "expected HugePages-2Mi and HugePages-1Gi volumes")

		assert.Assert(t, cmp.MarshalMatches(pod.Containers[0].VolumeMounts, `
- mountPath: /pgconf/tls
  name: cert-volume
  readOnly: true
- mountPath: /pgdata
  name: postgres-data
- mountPath: /etc/database-containerinfo
  name: database-containerinfo
  readOnly: true
- mountPath: /hugepages-2Mi
  name: hugepage-2mi
- mountPath: /hugepages-1Gi
  name: hugepage-1gi`), "expected hugepage mounts in %q container", pod.Containers[0].Name)
	})
}

func TestPodSecurityContext(t *testing.T) {
	cluster := new(v1beta1.PostgresCluster)
	err := cluster.Default(context.Background(), nil)
	assert.NilError(t, err)

	assert.Assert(t, cmp.MarshalMatches(PodSecurityContext(cluster), `
fsGroup: 26
fsGroupChangePolicy: OnRootMismatch
	`))

	cluster.Spec.OpenShift = new(true)

	cluster.Spec.SupplementalGroups = []int64{}
	assert.Assert(t, cmp.MarshalMatches(PodSecurityContext(cluster), `
fsGroupChangePolicy: OnRootMismatch
	`))

	cluster.Spec.SupplementalGroups = []int64{999, 65000}
	assert.Assert(t, cmp.MarshalMatches(PodSecurityContext(cluster), `
fsGroupChangePolicy: OnRootMismatch
supplementalGroups:
- 999
- 65000
	`))

	*cluster.Spec.OpenShift = false
	assert.Assert(t, cmp.MarshalMatches(PodSecurityContext(cluster), `
fsGroup: 26
fsGroupChangePolicy: OnRootMismatch
supplementalGroups:
- 999
- 65000
	`))

	t.Run("NoRootGID", func(t *testing.T) {
		cluster.Spec.SupplementalGroups = []int64{999, 0, 100, 0}
		assert.DeepEqual(t, []int64{999, 100}, PodSecurityContext(cluster).SupplementalGroups)

		cluster.Spec.SupplementalGroups = []int64{0}
		assert.Assert(t, PodSecurityContext(cluster).SupplementalGroups == nil)
	})
}

func TestInstancePodCABundle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cluster := new(v1beta1.PostgresCluster)
	assert.NilError(t, cluster.Default(ctx, nil))
	instance := new(v1beta1.PostgresInstanceSetSpec)
	instance.Default(0)
	dataVolume := new(corev1.PersistentVolumeClaim)
	dataVolume.Name = "datavol"

	serverSecretProjection := &corev1.SecretProjection{
		LocalObjectReference: corev1.LocalObjectReference{Name: "srv-secret"},
		Items: []corev1.KeyToPath{
			{Key: "tls.crt", Path: "tls.crt"},
			{Key: "tls.key", Path: "tls.key"},
		},
	}
	clientSecretProjection := &corev1.SecretProjection{
		LocalObjectReference: corev1.LocalObjectReference{Name: "repl-secret"},
		Items: []corev1.KeyToPath{
			{Key: "tls.crt", Path: "replication/tls.crt"},
			{Key: "tls.key", Path: "replication/tls.key"},
		},
	}
	bundle := &corev1.SecretProjection{
		LocalObjectReference: corev1.LocalObjectReference{Name: "ca-bundle"},
		Items: []corev1.KeyToPath{
			{Key: "ca.crt", Path: "ca.crt"},
			{Key: "ca.crt", Path: "replication/ca.crt"},
		},
	}

	certVolume := func(pod *corev1.PodSpec) *corev1.Volume {
		for i := range pod.Volumes {
			if pod.Volumes[i].Name == naming.CertVolume {
				return &pod.Volumes[i]
			}
		}
		return nil
	}

	t.Run("NilBundleLeavesTwoSources", func(t *testing.T) {
		pod := new(corev1.PodSpec)
		InstancePod(ctx, cluster, instance,
			serverSecretProjection, clientSecretProjection, nil, dataVolume, nil, nil, pod)

		volume := certVolume(pod)
		assert.Assert(t, volume != nil)
		assert.Equal(t, len(volume.Projected.Sources), 2)
	})

	t.Run("BundleIsAppended", func(t *testing.T) {
		pod := new(corev1.PodSpec)
		InstancePod(ctx, cluster, instance,
			serverSecretProjection, clientSecretProjection, bundle, dataVolume, nil, nil, pod)

		volume := certVolume(pod)
		assert.Assert(t, volume != nil)
		assert.Equal(t, len(volume.Projected.Sources), 3)
		assert.Equal(t, volume.Projected.Sources[2].Secret.Name, "ca-bundle")

		// Two sources writing one path make the Pod unschedulable, so every
		// path in the volume has to be unique.
		seen := map[string]bool{}
		for _, source := range volume.Projected.Sources {
			for _, item := range source.Secret.Items {
				assert.Assert(t, !seen[item.Path], "duplicate path %q", item.Path)
				seen[item.Path] = true
			}
		}
		assert.Assert(t, seen["ca.crt"] && seen["replication/ca.crt"])
	})
}
