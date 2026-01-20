package pgcluster

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/percona/percona-postgresql-operator/v2/internal/controller/postgrescluster"
	"github.com/percona/percona-postgresql-operator/v2/internal/logging"
	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	perconaPG "github.com/percona/percona-postgresql-operator/v2/percona/postgres"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

// TODO: make these configurable?
const (
	// default interval for checking for lag when no lag was previously detected
	defaultReplicationLagDetectionInterval = 5 * time.Minute
	// interval for checking lag when a lag was previously detected
	laggedReplicationInterval = 1 * time.Minute
)

// The presence of this file in the database container indicates the readiness probe that the
// data is lagging behind, and the pod readiness should fail.
const replicationLagSignalFile = "/pgdata/replication-lag-detected"

func (r *PGClusterReconciler) reconcileStandbyLag(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if !cr.ShouldCheckStandbyLag() {
		return nil
	}

	if cr.Status.Standby == nil {
		cr.Status.Standby = &v2.StandbyStatus{}
	}

	// If pgbackrest repo is the only source, we cannot get the lag if the primary cluster is not in k8s.
	if cr.Spec.Standby.RepoName != "" && cr.Spec.Standby.Host == "" {
		mainSiteNN, ok := cr.GetAnnotations()[pNaming.AnnotationReplicationMainSite]
		if !ok || mainSiteNN == "" {
			meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{
				Type:    postgrescluster.ConditionStandbyLagging,
				Status:  metav1.ConditionUnknown,
				Reason:  "MainSiteNotFound",
				Message: "Cannot find main site for replication lag calculation",
			})
			return nil
		}
	}

	// Do not try to calculate if the cluster is still initializing. We do not know the primary.
	isCondPresent := meta.FindStatusCondition(cr.Status.Conditions, postgrescluster.ConditionStandbyLagging) != nil
	if cr.Status.State != v2.AppStateReady && !isCondPresent {
		return nil
	}

	// Check if we should skip this reconciliation cycle based on the interval.
	if shouldSkipLagCheck(cr) {
		return nil
	}

	lagBytes, err := r.getStandbyLag(ctx, cr)
	if err != nil {
		return errors.Wrap(err, "calculate replication lag bytes")
	}

	maxLag := cr.Spec.Standby.MaxAcceptableLag.AsDec().UnscaledBig().Int64()
	lagDetected := lagBytes > maxLag

	cond := metav1.Condition{
		Type:   postgrescluster.ConditionStandbyLagging,
		Reason: "LagNotDetected",
		Status: metav1.ConditionFalse,
	}

	if lagDetected {
		cond.Status = metav1.ConditionTrue
		cond.Reason = "LagDetected"
		cond.Message = fmt.Sprintf("WAL is lagging by %d bytes (threshold: %d bytes)", lagBytes, maxLag)
	}

	// Set pod readiness only when the lag state transitions.
	if !meta.IsStatusConditionPresentAndEqual(cr.Status.Conditions, postgrescluster.ConditionStandbyLagging, cond.Status) {
		if err := r.setPodReplicationLagSignal(ctx, cr, !lagDetected); err != nil {
			return errors.Wrap(err, "set pod replication readiness signal")
		}
	}

	meta.SetStatusCondition(&cr.Status.Conditions, cond)
	cr.Status.Standby.LagBytes = lagBytes
	cr.Status.Standby.LagLastComputedAt = ptr.To(metav1.Now())
	return nil
}

// shouldSkipLagCheck determines if lag checking should be skipped based on the configured interval.
// We compute the lag at intervals because this is an expensive operation (requires pod execs and database queries).
func shouldSkipLagCheck(cr *v2.PerconaPGCluster) bool {
	interval := defaultReplicationLagDetectionInterval
	if meta.IsStatusConditionTrue(cr.Status.Conditions, postgrescluster.ConditionStandbyLagging) {
		interval = laggedReplicationInterval
	}

	if cr.Status.Standby.LagLastComputedAt == nil || cr.Status.Standby.LagLastComputedAt.IsZero() {
		return false
	}

	nextCheckTime := cr.Status.Standby.LagLastComputedAt.Add(interval)
	return time.Now().Before(nextCheckTime)
}

func (r *PGClusterReconciler) setPodReplicationLagSignal(
	ctx context.Context,
	cr *v2.PerconaPGCluster,
	ready bool,
) error {
	log := logging.FromContext(ctx)
	primary, err := perconaPG.GetPrimaryPod(ctx, r.Client, cr)
	if err != nil {
		return errors.Wrap(err, "get primary pod")
	}

	cmd := []string{"rm", "-f", replicationLagSignalFile}
	if !ready {
		cmd = []string{"touch", replicationLagSignalFile}
	}

	log.V(1).Info("Setting pod replication lag readiness signal", "pod", primary.Name, "ready", ready)
	return r.PodExec(ctx, primary.GetNamespace(), primary.GetName(), naming.ContainerDatabase, nil, io.Discard, nil, cmd...)
}

func (r *PGClusterReconciler) getStandbyLag(ctx context.Context, standby *v2.PerconaPGCluster) (int64, error) {
	if standby.Spec.Standby.Host != "" {
		return r.getLagFromStreamingHost(ctx, standby)
	}
	return r.getLagFromMainSite(ctx, standby)
}

func (r *PGClusterReconciler) getLagFromStreamingHost(ctx context.Context, standby *v2.PerconaPGCluster) (int64, error) {
	primary, err := perconaPG.GetPrimaryPod(ctx, r.Client, standby)
	if err != nil {
		return 0, errors.Wrap(err, "get primary pod")
	}

	podExecutor := postgres.Executor(func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string) error {
		return r.PodExec(ctx, primary.GetNamespace(), primary.GetName(), naming.ContainerDatabase, stdin, stdout, stderr, command...)
	})

	sql := "SELECT latest_end_lsn - pg_last_wal_replay_lsn() AS value from pg_catalog.pg_stat_wal_receiver;"
	stdout, stderr, err := podExecutor.Exec(ctx, strings.NewReader(sql), map[string]string{
		"ON_ERROR_STOP": "on",
		"QUIET":         "on",
	}, []string{"-t"})
	if err != nil {
		return 0, errors.Wrapf(err, "execute query: stderr=%s", stderr)
	}

	lagStr := strings.TrimSpace(stdout)
	lagBytes, err := strconv.ParseInt(lagStr, 10, 64)
	if err != nil {
		return 0, errors.Wrapf(err, "parse lag bytes: %s", lagStr)
	}
	return lagBytes, nil
}

func (r *PGClusterReconciler) getLagFromMainSite(ctx context.Context, standby *v2.PerconaPGCluster) (int64, error) {
	// Find the main site for the standby cluster.
	mainSiteNN, ok := standby.GetAnnotations()[pNaming.AnnotationReplicationMainSite]
	if !ok || mainSiteNN == "" {
		return 0, fmt.Errorf("annotation '%s' is missing or empty", pNaming.AnnotationReplicationMainSite)
	}

	mainSiteParts := strings.Split(mainSiteNN, "/")
	if len(mainSiteParts) != 2 {
		return 0, fmt.Errorf("invalid format for annotation '%s': expected 'namespace/name', got '%s'", pNaming.AnnotationReplicationMainSite, mainSiteNN)
	}

	mainSite := &v2.PerconaPGCluster{}
	objKey := client.ObjectKey{
		Name:      mainSiteParts[1],
		Namespace: mainSiteParts[0],
	}
	if err := r.Client.Get(ctx, objKey, mainSite); err != nil {
		return 0, errors.Wrap(err, "get main site for replication lag calculation")
	}
	curWALLSN, err := r.getCurrentWALLSN(ctx, mainSite)
	if err != nil {
		return 0, errors.Wrap(err, "get current WAL SN")
	}

	lagBytes, err := r.getWALLagBytes(ctx, curWALLSN, standby)
	if err != nil {
		return 0, errors.Wrap(err, "get WAL lag bytes")
	}

	return lagBytes, nil
}

func (r *PGClusterReconciler) getWALLagBytes(
	ctx context.Context,
	currentWALLSN string,
	standby *v2.PerconaPGCluster) (int64, error) {
	primary, err := perconaPG.GetPrimaryPod(ctx, r.Client, standby)
	if err != nil {
		return 0, errors.Wrap(err, "get primary pod")
	}

	podExecutor := postgres.Executor(func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string) error {
		return r.PodExec(ctx, primary.GetNamespace(), primary.GetName(), naming.ContainerDatabase, stdin, stdout, stderr, command...)
	})

	sql := fmt.Sprintf("SELECT pg_wal_lsn_diff('%s'::pg_lsn, pg_last_wal_replay_lsn());", currentWALLSN)
	stdout, stderr, err := podExecutor.Exec(ctx, strings.NewReader(sql), map[string]string{
		"ON_ERROR_STOP": "on",
		"QUIET":         "on",
	}, []string{"-t"})
	if err != nil {
		return 0, errors.Wrapf(err, "execute query: stderr=%s", stderr)
	}
	lagBytesStr := strings.TrimSpace(stdout)
	lagBytes, err := strconv.ParseInt(lagBytesStr, 10, 64)
	if err != nil {
		return 0, errors.Wrapf(err, "parse lag bytes: %s", lagBytesStr)
	}
	return lagBytes, nil
}

func (r *PGClusterReconciler) getCurrentWALLSN(ctx context.Context, cr *v2.PerconaPGCluster) (string, error) {
	primary, err := perconaPG.GetPrimaryPod(ctx, r.Client, cr)
	if err != nil {
		return "", errors.Wrap(err, "get primary pod")
	}

	podExecutor := postgres.Executor(func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, command ...string) error {
		return r.PodExec(ctx, primary.GetNamespace(), primary.GetName(), naming.ContainerDatabase, stdin, stdout, stderr, command...)
	})

	sql := "SELECT pg_current_wal_lsn();"
	stdout, stderr, err := podExecutor.Exec(ctx, strings.NewReader(sql), map[string]string{
		"ON_ERROR_STOP": "on",
		"QUIET":         "on",
	}, []string{"-t"})
	if err != nil {
		return "", errors.Wrapf(err, "execute query: stderr=%s", stderr)
	}

	lsn := strings.TrimSpace(stdout)
	if lsn == "" {
		return "", errors.New("empty WAL LSN result")
	}
	return lsn, nil
}

func (r *PGClusterReconciler) reconcileStandbyMainSiteAnnotation(ctx context.Context, cr *v2.PerconaPGCluster) error {
	if !cr.ShouldCheckStandbyLag() || cr.Spec.Standby.RepoName == "" {
		return nil
	}

	if _, ok := cr.GetAnnotations()[pNaming.AnnotationReplicationMainSite]; ok {
		return nil
	}

	mainSite, err := r.getStandbyMainSite(ctx, cr)
	if err != nil {
		return errors.Wrap(err, "get standby main site")
	}

	log := logging.FromContext(ctx)
	if mainSite == nil {
		log.V(1).Info("Main site not found in Kubernetes, cannot detect standby lag")
	}

	crCopy := cr.DeepCopy()
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(crCopy), crCopy); err != nil {
		return errors.Wrap(err, "get cluster for main site annotation update")
	}

	annots := crCopy.GetAnnotations()
	if annots == nil {
		annots = make(map[string]string)
	}
	annots[pNaming.AnnotationReplicationMainSite] = mainSite.GetNamespace() + "/" + mainSite.GetName()
	crCopy.SetAnnotations(annots)
	if err := r.Client.Update(ctx, crCopy); err != nil {
		return errors.Wrap(err, "update standby main site annotation")
	}
	return nil
}

// getStandbyMainSite returns the name of the main site for the standby cluster (based on pgbackrest only)
func (r *PGClusterReconciler) getStandbyMainSite(ctx context.Context, cr *v2.PerconaPGCluster) (*v2.PerconaPGCluster, error) {
	if !cr.ShouldCheckStandbyLag() || cr.Spec.Standby.RepoName == "" {
		return nil, errors.New("standby cluster is not enabled or repo name is not specified")
	}

	targetRepo := v1beta1.PGBackRestRepo{}
	for _, repo := range cr.Spec.Backups.PGBackRest.Repos {
		if repo.Name == cr.Spec.Standby.RepoName {
			targetRepo = repo
			break
		}
	}

	if targetRepo.Name == "" {
		return nil, errors.New("standby repo name not found in list of repos")
	}

	listOptions := []client.ListOption{}
	if len(r.WatchNamespace) == 1 {
		listOptions = append(listOptions, client.InNamespace(cr.Namespace))
	}
	clusters := &v2.PerconaPGClusterList{}
	if err := r.Client.List(ctx, clusters, listOptions...); err != nil {
		return nil, errors.Wrap(err, "list clusters")
	}

	for _, cluster := range clusters.Items {
		if cluster.Name == cr.Name {
			continue
		}
		if cluster.Spec.Standby != nil && cluster.Spec.Standby.Enabled {
			continue
		}
		for _, repo := range cluster.Spec.Backups.PGBackRest.Repos {
			if targetRepo.StorageEquals(&repo) {
				return cluster.DeepCopy(), nil
			}
		}
	}
	return nil, nil
}

// pollAndRequeueStandbys periodically polls the clusters and requeues those standbys that need to be checked for lag.
func pollAndRequeueStandbys(
	ctx context.Context,
	events chan event.GenericEvent,
	cl client.Client,
	namespace string) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	log := logging.FromContext(ctx).WithName("PollStandbys")
	for {
		select {
		case <-ticker.C:
			clusters := &v2.PerconaPGClusterList{}

			listOptions := []client.ListOption{}
			if namespace != "" {
				listOptions = append(listOptions, client.InNamespace(namespace))
			}
			if err := cl.List(ctx, clusters, listOptions...); err != nil {
				log.Error(err, "list clusters")
				continue
			}

			for _, cluster := range clusters.Items {
				status := cluster.Status
				if !cluster.ShouldCheckStandbyLag() || status.Standby == nil || shouldSkipLagCheck(&cluster) {
					continue
				}
				log.Info("Requeuing standby cluster for lag check", "cluster", cluster.Name)
				events <- event.GenericEvent{Object: cluster.DeepCopy()}
			}
		case <-ctx.Done():
			log.Info("Stopping poll and requeue standbys")
			return
		}
	}
}
