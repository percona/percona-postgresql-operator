package pgtde

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	crunchyv1beta1 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

// ReportExtension records the outcome of a ReconcileExtension.
// The PGTDEEnabled condition decides whether pg_tde is in shared_preload_libraries
// and whether instance Pods carry the vault volume.
func ReportExtension(cluster *crunchyv1beta1.PostgresCluster, record record.EventRecorder, err error) {
	enabled := cluster.Spec.Extensions.PGTDE.Enabled

	if err != nil {
		// Leave the condition alone: a failed DROP means the extension is
		// still installed, and a failed CREATE means whatever was there
		// before still is.
		if enabled {
			record.Event(cluster, corev1.EventTypeWarning,
				"PGTDEInstallFailed", "Unable to install pg_tde")
		} else {
			record.Event(cluster, corev1.EventTypeWarning,
				"PGTDEDisableFailed", "Unable to disable pg_tde")
		}
		return
	}

	condition := metav1.Condition{
		Type:               crunchyv1beta1.PGTDEEnabled,
		Status:             metav1.ConditionTrue,
		Reason:             "Enabled",
		Message:            "pg_tde is enabled in PerconaPGCluster",
		ObservedGeneration: cluster.GetGeneration(),
	}
	if !enabled {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "Disabled"
		condition.Message = "pg_tde is disabled in PerconaPGCluster"
	}
	meta.SetStatusCondition(&cluster.Status.Conditions, condition)
}

// Phase is where a cluster stands in the two-phase credential change
// described on reconcilePGTDEProviders.
type Phase int

const (
	// InitialSetup means no key provider has been configured yet, so the
	// credentials in the spec are the only ones there have ever been.
	InitialSetup Phase = iota

	// Configured means the key provider names the credentials in the spec.
	Configured

	// StageCredentials means the spec names credentials the key provider
	// has not been pointed at yet. Phase 1 has to copy them onto the data
	// volumes and repoint the provider before the Pods may mount them.
	StageCredentials

	// Finalize means the key provider names the staged copies on the data
	// volumes. Phase 2 repoints it at the mount paths and removes them.
	Finalize
)

type vaultChange struct {
	// Paths inside the Pod. The standard pair are the projected Secret's mount
	// paths; the temp pair are the copies staged on the data volume.
	TokenPath, CAPath         string
	TempTokenPath, TempCAPath string
}

type fileChange struct {
	KeyPath     string
	TempKeyPath string
}

// pgTDEChange is the state of a pg_tde credential change. reconcileInstance
// decides whether to hold the Pods' pg-tde volume from it and
// reconcilePGTDEProviders decides which SQL to run; the two have to agree,
// because releasing the volume in a phase that still expects the old
// credentials mounted is what leaves pg_tde unable to fetch its key.
type pgTDEChange struct {
	Phase Phase

	Vault *vaultChange
	File  *fileChange

	// Revisions matching each pair of paths, to compare with
	// cluster.Status.PGTDERevision.
	StandardRevision, TempRevision string
}

// ChangePhase derives the change from the spec and the stored revision.
func ChangePhase(provider KeyProvider, pgTDERevision string) (pgTDEChange, error) {
	var change pgTDEChange
	var err error

	if change.StandardRevision, err = provider.GetRevision(provider.GetCredentialPath()); err != nil {
		return change, err
	}
	if change.TempRevision, err = provider.GetRevision(provider.GetStagedCredentialPath()); err != nil {
		return change, err
	}

	switch pgTDERevision {
	case "":
		change.Phase = InitialSetup
	case change.StandardRevision:
		change.Phase = Configured
	case change.TempRevision:
		change.Phase = Finalize
	default:
		change.Phase = StageCredentials
	}

	return change, nil
}

// PreserveOldTDEVolume replaces the pg-tde volume and its mount on the database
// container with the ones from the StatefulSet as it exists in the cluster,
// adding them back when the new pod spec no longer has them. This prevents pods
// from restarting with new vault credentials before the vault provider change
// SQL has been executed, and from restarting with no credentials at all while
// the extension is still installed.
func PreserveOldTDEVolume(podSpec *corev1.PodSpec, existing *appsv1.StatefulSet) {
	var oldVolume *corev1.Volume
	for i := range existing.Spec.Template.Spec.Volumes {
		if existing.Spec.Template.Spec.Volumes[i].Name == naming.PGTDEVolume {
			oldVolume = &existing.Spec.Template.Spec.Volumes[i]
			break
		}
	}
	if oldVolume == nil {
		return
	}

	replaced := false
	for i := range podSpec.Volumes {
		if podSpec.Volumes[i].Name == naming.PGTDEVolume {
			podSpec.Volumes[i] = *oldVolume
			replaced = true
			break
		}
	}
	if !replaced {
		podSpec.Volumes = append(podSpec.Volumes, *oldVolume)
	}

	// The volume is only ever mounted into the database container.
	var oldMount *corev1.VolumeMount
	for _, container := range existing.Spec.Template.Spec.Containers {
		if container.Name == naming.ContainerDatabase {
			for i := range container.VolumeMounts {
				if container.VolumeMounts[i].Name == naming.PGTDEVolume {
					oldMount = &container.VolumeMounts[i]
					break
				}
			}
			break
		}
	}
	if oldMount == nil {
		return
	}

	for i := range podSpec.Containers {
		if podSpec.Containers[i].Name != naming.ContainerDatabase {
			continue
		}

		mounts := podSpec.Containers[i].VolumeMounts
		replaced := false
		for j := range mounts {
			if mounts[j].Name == naming.PGTDEVolume {
				mounts[j] = *oldMount
				replaced = true
				break
			}
		}
		if !replaced {
			podSpec.Containers[i].VolumeMounts = append(mounts, *oldMount)
		}
		break
	}
}
