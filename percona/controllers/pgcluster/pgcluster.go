package pgcluster

import (
	"context"
	"fmt"
	"reflect"
	"strconv"

	"github.com/percona/percona-postgresql-operator/internal/config"
	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
	"github.com/percona/percona-postgresql-operator/internal/operator"
	dplmnt "github.com/percona/percona-postgresql-operator/percona/controllers/deployment"
	"github.com/percona/percona-postgresql-operator/percona/controllers/pmm"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	templatePath      = "/"
	defaultPGOVersion = "0.2.0"
	S3StorageType     = crv1.StorageType("s3")
	GCSStorageType    = crv1.StorageType("gcs")
)

func Create(clientset kubeapi.Interface, newPerocnaPGCluster *crv1.PerconaPGCluster) error {
	ctx := context.TODO()
	cluster := getPGCLuster(newPerocnaPGCluster, &crv1.Pgcluster{})

	_, err := clientset.CrunchydataV1().Pgclusters(newPerocnaPGCluster.Namespace).Create(ctx, cluster, metav1.CreateOptions{})
	if err != nil {
		return errors.Wrap(err, "create pgcluster resource")
	}

	return nil
}

func Update(clientset kubeapi.Interface, newPerocnaPGCluster, oldPerocnaPGCluster *crv1.PerconaPGCluster) error {
	ctx := context.TODO()
	oldPGCluster, err := clientset.CrunchydataV1().Pgclusters(oldPerocnaPGCluster.Namespace).Get(ctx, oldPerocnaPGCluster.Name, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "get old pgcluster resource")
	}
	pgCluster := getPGCLuster(newPerocnaPGCluster, oldPGCluster)
	deployment, err := clientset.AppsV1().Deployments(pgCluster.Namespace).Get(ctx,
		pgCluster.Name, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, "could not find instance")
	}
	if !reflect.DeepEqual(oldPerocnaPGCluster.Spec.PMM, newPerocnaPGCluster.Spec.PMM) {
		err = pmm.UpdatePMMSidecar(clientset, pgCluster, deployment)
		if err != nil {
			return errors.Wrap(err, "update pmm sidecar")
		}
	}
	dplmnt.UpdateSpecTemplateSpecSecurityContext(newPerocnaPGCluster, deployment)

	if _, err := clientset.AppsV1().Deployments(deployment.Namespace).Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		return errors.Wrap(err, "could not update deployment")
	}
	_, err = clientset.CrunchydataV1().Pgclusters(oldPerocnaPGCluster.Namespace).Update(ctx, pgCluster, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrap(err, "update pgcluster resource")
	}

	return nil
}

func Delete(clientset kubeapi.Interface, perocnaPGCluster *crv1.PerconaPGCluster) error {
	ctx := context.TODO()
	err := clientset.CrunchydataV1().Pgclusters(perocnaPGCluster.Namespace).Delete(ctx, perocnaPGCluster.Name, metav1.DeleteOptions{})
	if err != nil {
		return errors.Wrap(err, "delete pgcluster resource")
	}

	return nil
}

func IsPrimary(clientset kubeapi.Interface, perocnaPGCluster *crv1.PerconaPGCluster) (bool, error) {
	ctx := context.TODO()

	selector := fmt.Sprintf("%s=%s",
		"deployment-name", perocnaPGCluster.Name)

	pods, err := clientset.CoreV1().Pods(perocnaPGCluster.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return false, err
	}
	if len(pods.Items) == 0 {
		return false, errors.Errorf("no pods for deployment %s", perocnaPGCluster.Name)
	}
	primaryField, ok := pods.Items[0].GetLabels()[config.LABEL_PGHA_ROLE]
	if !ok {
		return false, errors.Errorf("no role labels in pod %s", pods.Items[0].Name)
	}
	if primaryField == config.LABEL_PGHA_ROLE_PRIMARY {
		return true, nil
	}

	return false, nil
}

func getPGCLuster(pgc *crv1.PerconaPGCluster, cluster *crv1.Pgcluster) *crv1.Pgcluster {
	metaAnnotations := map[string]string{
		"current-primary": pgc.Name,
	}
	if cluster.Annotations != nil {
		for k, v := range cluster.Annotations {
			metaAnnotations[k] = v
		}
	}
	if pgc.Annotations != nil {
		for k, v := range pgc.Annotations {
			metaAnnotations[k] = v
		}
	}
	specAnnotationsGlobal := make(map[string]string)
	for k, v := range cluster.Spec.Annotations.Global {
		specAnnotationsGlobal[k] = v
	}
	specAnnotationsGlobal[config.ANNOTATION_CLUSTER_KEEP_DATA] = strconv.FormatBool(pgc.Spec.KeepData)
	specAnnotationsGlobal[config.ANNOTATION_CLUSTER_KEEP_BACKUPS] = strconv.FormatBool(pgc.Spec.KeepBackups)

	pgoVersion := defaultPGOVersion
	version, ok := pgc.Labels["pgo-version"]
	if ok {
		pgoVersion = version
	}
	pgoUser := "admin"
	user, ok := pgc.Labels["pgouser"]
	if ok {
		pgoUser = user
	}
	metaLabels := map[string]string{
		"crunchy-pgha-scope": pgc.Name,
		"deployment-name":    pgc.Name,
		"name":               pgc.Name,
		"pg-cluster":         pgc.Name,
		"pgo-version":        pgoVersion,
		"pgouser":            pgoUser,
	}

	for k, v := range cluster.Labels {
		metaLabels[k] = v
	}
	for k, v := range pgc.Labels {
		metaLabels[k] = v
	}

	userLabels := make(map[string]string)
	for k, v := range pgc.Spec.UserLabels {
		userLabels[k] = v
	}
	var syncReplication *bool
	if pgc.Spec.PGReplicas != nil {
		syncReplication = &pgc.Spec.PGReplicas.HotStandby.EnableSyncStandby
	}
	cluster.Annotations = metaAnnotations
	cluster.Labels = metaLabels
	cluster.Name = pgc.Name
	cluster.Namespace = pgc.Namespace
	cluster.Spec.BackrestStorage = getStorage(pgc.Spec.Backup.VolumeSpec)
	cluster.Spec.PrimaryStorage = getStorage(pgc.Spec.PGPrimary.VolumeSpec)
	cluster.Spec.ReplicaStorage = getStorage(pgc.Spec.PGReplicas.HotStandby.VolumeSpec)
	cluster.Spec.ClusterName = pgc.Name
	cluster.Spec.PGImage = pgc.Spec.PGPrimary.Image
	cluster.Spec.BackrestImage = pgc.Spec.Backup.Image
	cluster.Spec.BackrestRepoImage = pgc.Spec.Backup.BackrestRepoImage
	cluster.Spec.BackrestResources = pgc.Spec.Backup.Resources.Requests
	cluster.Spec.BackrestLimits = pgc.Spec.Backup.Resources.Limits
	cluster.Spec.DisableAutofail = pgc.Spec.DisableAutofail
	cluster.Spec.Name = pgc.Name
	cluster.Spec.Database = pgc.Spec.Database
	cluster.Spec.PGBadger = pgc.Spec.PGBadger.Enabled
	cluster.Spec.PGBadgerImage = pgc.Spec.PGBadger.Image
	cluster.Spec.PGBadgerPort = strconv.Itoa(pgc.Spec.PGBadger.Port)
	cluster.Spec.PgBouncer.Image = pgc.Spec.PGBouncer.Image
	cluster.Spec.PgBouncer.Replicas = pgc.Spec.PGBouncer.Size
	cluster.Spec.PgBouncer.Resources = pgc.Spec.PGBouncer.Resources.Requests
	cluster.Spec.PgBouncer.Limits = pgc.Spec.PGBouncer.Resources.Limits
	cluster.Spec.PGOImagePrefix = operator.Pgo.Cluster.CCPImagePrefix
	if len(pgc.Spec.PGPrimary.AntiAffinityType) == 0 {
		pgc.Spec.PGPrimary.AntiAffinityType = "preferred"
	}
	if len(pgc.Spec.Backup.AntiAffinityType) == 0 {
		pgc.Spec.Backup.AntiAffinityType = "preferred"
	}
	if len(pgc.Spec.PGBouncer.AntiAffinityType) == 0 {
		pgc.Spec.PGBouncer.AntiAffinityType = "preferred"
	}
	cluster.Spec.PodAntiAffinity = crv1.PodAntiAffinitySpec{
		Default:    pgc.Spec.PGPrimary.AntiAffinityType,
		PgBackRest: pgc.Spec.Backup.AntiAffinityType,
		PgBouncer:  pgc.Spec.PGBouncer.AntiAffinityType,
	}
	cluster.Spec.Port = pgc.Spec.Port
	cluster.Spec.Resources = pgc.Spec.PGPrimary.Resources.Requests
	cluster.Spec.Limits = pgc.Spec.PGPrimary.Resources.Limits
	cluster.Spec.User = pgc.Spec.User
	cluster.Spec.UserLabels = pgc.Spec.UserLabels
	cluster.Spec.SyncReplication = syncReplication
	cluster.Spec.UserLabels = userLabels
	cluster.Spec.Annotations.Global = specAnnotationsGlobal
	cluster.Spec.Tolerations = pgc.Spec.PGPrimary.Tolerations
	for _, s := range pgc.Spec.Backup.Storages {
		switch s.Type {
		case S3StorageType:
			cluster.Spec.BackrestS3Bucket = s.Bucket
			cluster.Spec.BackrestS3Endpoint = s.EndpointURL
			cluster.Spec.BackrestS3Region = s.Region
			cluster.Spec.BackrestS3URIStyle = s.URIStyle
			cluster.Spec.BackrestS3VerifyTLS = strconv.FormatBool(s.VerifyTLS)
		case GCSStorageType:
			cluster.Spec.BackrestGCSBucket = s.Bucket
			cluster.Spec.BackrestGCSEndpoint = s.EndpointURL
			cluster.Spec.BackrestGCSKeyType = s.KeyType
		}
	}
	cluster.Spec.BackrestRepoPath = pgc.Spec.Backup.RepoPath
	cluster.Spec.BackrestStorageTypes = pgc.Spec.Backup.StorageTypes
	cluster.Spec.PGDataSource.Namespace = pgc.Spec.PGDataSource.Namespace
	cluster.Spec.PGDataSource.RestoreFrom = pgc.Spec.PGDataSource.RestoreFrom
	cluster.Spec.PGDataSource.RestoreOpts = pgc.Spec.PGDataSource.RestoreOpts
	cluster.Spec.ServiceType = pgc.Spec.PGPrimary.Expose.ServiceType
	cluster.Spec.TLSOnly = pgc.Spec.TlSOnly
	cluster.Spec.Standby = pgc.Spec.Standby
	cluster.Spec.Shutdown = pgc.Spec.Pause
	if cluster.Spec.TablespaceMounts == nil {
		cluster.Spec.TablespaceMounts = make(map[string]crv1.PgStorageSpec)
	}
	for k, v := range pgc.Spec.TablespaceStorages {
		cluster.Spec.TablespaceMounts[k] = v.VolumeSpec
	}
	cluster.Spec.WALStorage = pgc.Spec.WalStorage.VolumeSpec
	cluster.Spec.PGDataSource = pgc.Spec.PGDataSource

	return cluster
}

func getStorage(storageSpec *crv1.PgStorageSpec) crv1.PgStorageSpec {
	if storageSpec == nil {
		return crv1.PgStorageSpec{
			AccessMode:  "ReadWriteOnce",
			Size:        "1G",
			StorageType: "dynamic",
		}
	}
	if len(storageSpec.AccessMode) == 0 {
		storageSpec.AccessMode = "ReadWriteOnce"
	}
	if len(storageSpec.Size) == 0 {
		storageSpec.Size = "1G"
	}
	if len(storageSpec.StorageType) == 0 {
		storageSpec.StorageType = "dynamic"
	}

	return *storageSpec
}
