package pgcluster

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/percona/percona-postgresql-operator/v2/internal/naming"
	pNaming "github.com/percona/percona-postgresql-operator/v2/percona/naming"
	v2 "github.com/percona/percona-postgresql-operator/v2/pkg/apis/pgv2.percona.com/v2"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/upstream.pgv2.percona.com/v1beta1"
)

func compareMaps(x map[string]string, y map[string]string) bool {
	if len(x) != len(y) {
		return false
	}

	for k, v := range x {
		if y[k] != v {
			return false
		}
	}
	return true
}

func TestBackupOwnerReference(t *testing.T) {
	// The test is disabled, because there are problems with the fake client
	//
	// Problem: apply patches are not supported in the fake client. Follow https://github.com/kubernetes/kubernetes/issues/115598 for the current status
	// TODO: remove the line below
	t.Skip()

	ctx := context.Background()

	const crName = "backup-owner-reference"
	const ns = crName

	cr, err := readDefaultCR(crName, ns)
	if err != nil {
		t.Fatal(err)
	}
	schedule := "* * * * *"
	cr.Spec.Backups.PGBackRest.Repos[0].BackupSchedules = &v1beta1.PGBackRestBackupSchedules{
		Full: &schedule,
	}

	fakeClient, err := buildFakeClient(ctx, cr)
	if err != nil {
		t.Fatalf("failed to build fake client: %v", err)
	}

	reconciler := reconciler(cr)
	reconciler.Client = fakeClient
	crunchyReconciler := crunchyReconciler()
	crunchyReconciler.Client = fakeClient

	reconcile := func() {
		crNamespacedName := types.NamespacedName{Name: crName, Namespace: ns}

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		if err != nil {
			t.Fatal(err)
		}
		_, err = crunchyReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: crNamespacedName})
		if err != nil {
			t.Fatal(err)
		}
	}

	reconcile() // this reconcile should create statefulsets

	stsList := &appsv1.StatefulSetList{}
	if err := fakeClient.List(ctx, stsList, client.InNamespace(ns)); err != nil {
		t.Fatal(err)
	}
	if err := createFakePodsForStatefulsets(ctx, fakeClient, stsList); err != nil {
		t.Fatal(err)
	}

	reconcile() // this reconcile should create "replica-create" job

	jobList := &batchv1.JobList{}
	err = fakeClient.List(ctx, jobList, client.InNamespace(ns))
	if err != nil {
		t.Fatal(err)
	}

	for _, job := range jobList.Items {
		job := job
		if job.Labels[naming.LabelPGBackRestBackup] == string(naming.BackupReplicaCreate) {
			job.Status.Conditions = []batchv1.JobCondition{
				{
					Type:   batchv1.JobComplete,
					Status: corev1.ConditionTrue,
				},
			}
			if err := fakeClient.Status().Update(ctx, &job); err != nil {
				t.Fatal(err)
			}
			break
		}
	}

	reconcile() // this reconcile should set ConditionReplicaCreate to true
	reconcile() // this reconcile should create cronjob for schedule

	cronjobList := &batchv1.CronJobList{}
	err = fakeClient.List(ctx, cronjobList, client.InNamespace(ns))
	if err != nil {
		t.Fatal(err)
	}
	for _, cj := range cronjobList.Items {
		cj := cj
		if err := createFakeJobForCron(ctx, fakeClient, &cj); err != nil {
			t.Fatal(err)
		}
	}

	reconcile() // this reconcile should create pg-backup for scheduled job

	jobList = &batchv1.JobList{}
	err = fakeClient.List(ctx, jobList, client.InNamespace(ns))
	if err != nil {
		t.Fatal(err)
	}
	pgBackupList := &v2.PerconaPGBackupList{}
	err = fakeClient.List(ctx, pgBackupList, client.InNamespace(ns))
	if err != nil {
		t.Fatal(err)
	}

	if len(jobList.Items) != len(pgBackupList.Items) {
		t.Fatal("job list and pgbackup list should have the same length")
	}
	for _, job := range jobList.Items {
		for _, ownerRef := range job.OwnerReferences {
			if ownerRef.Kind != "PerconaPGBackup" {
				t.Fatal("owner reference should be set to PerconaPGBackup")
			}
			foundPGBackup := false
			for _, pgBackup := range pgBackupList.Items {
				if pgBackup.Name == ownerRef.Name {
					foundPGBackup = true
					break
				}
			}
			if !foundPGBackup {
				t.Fatalf("%s PerconaPGBackup not found", ownerRef.Name)
			}
		}
	}
}

func TestReconcileBackupJobCleansUpFinishedManualJob(t *testing.T) {
	const crName = "some-cluster"
	const ns = crName
	const repoName = "repo1"
	const jobName = "completed-manual-job-backup"
	const backupName = "attached-pgbackup"

	t.Run("without attached pg-backup", func(t *testing.T) {
		ctx := t.Context()

		cr, err := readDefaultCR(crName, ns)
		require.NoError(t, err)

		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:       jobName,
				Namespace:  ns,
				Labels:     naming.PGBackRestBackupJobLabels(crName, repoName, naming.BackupManual),
				Finalizers: []string{pNaming.FinalizerKeepJob},
			},
			Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{{
					Type:   batchv1.JobComplete,
					Status: corev1.ConditionTrue,
				}},
			},
		}

		cl, err := buildFakeClient(ctx, cr, job)
		require.NoError(t, err)

		require.NoError(t, reconcileBackupJob(ctx, cl, cr, *job, repoName))

		require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: jobName, Namespace: ns}, job))
		assert.False(t, controllerutil.ContainsFinalizer(job, pNaming.FinalizerKeepJob))
		for k := range naming.PGBackRestLabels(crName) {
			_, ok := job.Labels[k]
			assert.False(t, ok)
		}
		assert.Equal(t, string(naming.BackupManual), job.Labels[naming.LabelPGBackRestBackup])
		assert.Equal(t, repoName, job.Labels[naming.LabelPGBackRestRepo])
		assert.NotContains(t, job.Labels, naming.LabelPGBackRest)
	})

	t.Run("failed job without attached pg-backup", func(t *testing.T) {
		ctx := t.Context()

		cr, err := readDefaultCR(crName, ns)
		require.NoError(t, err)

		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:       jobName,
				Namespace:  ns,
				Labels:     naming.PGBackRestBackupJobLabels(crName, repoName, naming.BackupManual),
				Finalizers: []string{pNaming.FinalizerKeepJob},
			},
			Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{{
					Type:   batchv1.JobFailed,
					Status: corev1.ConditionTrue,
				}},
			},
		}

		cl, err := buildFakeClient(ctx, cr, job)
		require.NoError(t, err)

		require.NoError(t, reconcileBackupJob(ctx, cl, cr, *job, repoName))

		require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: jobName, Namespace: ns}, job))
		assert.False(t, controllerutil.ContainsFinalizer(job, pNaming.FinalizerKeepJob))
		for k := range naming.PGBackRestLabels(crName) {
			_, ok := job.Labels[k]
			assert.False(t, ok)
		}
		assert.Equal(t, string(naming.BackupManual), job.Labels[naming.LabelPGBackRestBackup])
		assert.Equal(t, repoName, job.Labels[naming.LabelPGBackRestRepo])
		assert.NotContains(t, job.Labels, naming.LabelPGBackRest)
	})

	t.Run("with attached pg-backup", func(t *testing.T) {
		ctx := t.Context()

		cr, err := readDefaultCR(crName, ns)
		require.NoError(t, err)

		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:       jobName,
				Namespace:  ns,
				Labels:     naming.PGBackRestBackupJobLabels(crName, repoName, naming.BackupManual),
				Finalizers: []string{pNaming.FinalizerKeepJob},
			},
			Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{{
					Type:   batchv1.JobComplete,
					Status: corev1.ConditionTrue,
				}},
			},
		}

		pgBackup := &v2.PerconaPGBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backupName,
				Namespace: ns,
			},
			Spec: v2.PerconaPGBackupSpec{
				PGCluster: crName,
				RepoName:  ptr.To(repoName),
			},
			Status: v2.PerconaPGBackupStatus{
				JobName: jobName,
			},
		}

		cl, err := buildFakeClient(ctx, cr, job, pgBackup)
		require.NoError(t, err)

		require.NoError(t, reconcileBackupJob(ctx, cl, cr, *job, repoName))

		require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: jobName, Namespace: ns}, job))
		assert.True(t, controllerutil.ContainsFinalizer(job, pNaming.FinalizerKeepJob))
		for k, v := range naming.PGBackRestBackupJobLabels(crName, repoName, naming.BackupManual) {
			assert.Equal(t, v, job.Labels[k])
		}

		backup := &v2.PerconaPGBackup{}
		require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: backupName, Namespace: ns}, backup))
		assert.Equal(t, jobName, backup.Status.JobName)
	})
}

func createFakeJobForCron(ctx context.Context, cl client.Client, cronJob *batchv1.CronJob) error {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cronJob.Name + "-1",
			Namespace: cronJob.Namespace,
			Labels:    cronJob.Labels,
		},
		Spec: cronJob.Spec.JobTemplate.Spec,
	}
	if err := controllerutil.SetControllerReference(cronJob, job, cl.Scheme()); err != nil {
		return err
	}
	if err := cl.Create(ctx, job); err != nil {
		return err
	}
	return nil
}

func createFakePodsForStatefulsets(ctx context.Context, cl client.Client, stsList *appsv1.StatefulSetList) error {
	for _, sts := range stsList.Items {
		sts := sts
		for i := 0; i < int(*sts.Spec.Replicas); i++ {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sts.Name + "-" + strconv.Itoa(i),
					Namespace: sts.Namespace,
					Labels:    sts.Spec.Template.Labels,
					Annotations: map[string]string{
						"status": `"role":"master"`,
					},
				},
				Spec: sts.Spec.Template.Spec,
			}
			if err := cl.Create(ctx, pod); err != nil {
				return err
			}
			if err := cl.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod); err != nil {
				return err
			}
			pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{
				Type: corev1.PodReady, Status: corev1.ConditionTrue,
			})
			if err := cl.Status().Update(ctx, pod); err != nil {
				return err
			}
		}
		sts.Status.ReadyReplicas = *sts.Spec.Replicas
		sts.Status.Replicas = *sts.Spec.Replicas
		sts.Status.UpdatedReplicas = *sts.Spec.Replicas
		sts.Status.CurrentReplicas = *sts.Spec.Replicas
		if err := cl.Status().Update(ctx, &sts); err != nil {
			return err
		}
	}
	return nil
}
