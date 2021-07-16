package pgc

import (
	"context"
	"io/ioutil"
	"reflect"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/percona/percona-postgresql-operator/internal/config"
	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
	"github.com/percona/percona-postgresql-operator/percona/controllers/pmm"
	"github.com/percona/percona-postgresql-operator/percona/controllers/replica"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
	informers "github.com/percona/percona-postgresql-operator/pkg/generated/informers/externalversions/crunchydata.com/v1"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// Controller holds the connections for the controller
type Controller struct {
	Client                      *kubeapi.Client
	Queue                       workqueue.RateLimitingInterface
	Informer                    informers.PerconaPGClusterInformer
	PerconaPGClusterWorkerCount int
	deploymentTemplateData      []byte
}

const (
	deploymentTemplateName = "cluster-deployment.json"
	templatePath           = "/"
	defaultPGOVersion      = "0.1.0"
)

// onAdd is called when a pgcluster is added
func (c *Controller) onAdd(obj interface{}) {
	ctx := context.TODO()
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		log.Printf("get key %s", err)
	}
	log.Debugf("percona cluster putting key in queue %s", key)

	c.Queue.Add(key)
	defer c.Queue.Done(key)
	newCluster := obj.(*crv1.PerconaPGCluster)
	err = c.updateTemplate(newCluster)
	if err != nil {
		log.Errorf("update deployment template: %s", err)
	}

	err = createOrUpdatePrimaryService(c.Client, newCluster)
	if err != nil {
		log.Errorf("handle primary service on create")
		return
	}

	cluster := getPGCLuster(newCluster, &crv1.Pgcluster{})

	_, err = c.Client.CrunchydataV1().Pgclusters(newCluster.Namespace).Create(ctx, cluster, metav1.CreateOptions{})
	if err != nil {
		log.Errorf("create pgcluster resource: %s", err)
	}

	if newCluster.Spec.PGReplicas != nil {
		err = replica.Create(c.Client, newCluster)
		if err != nil {
			log.Errorf("create pgreplicas: %s", err)
		}
	}
	c.Queue.Forget(key)
}

func (c *Controller) updateTemplate(newCluster *crv1.PerconaPGCluster) error {
	templateData, err := pmm.HandlePMMTemplate(c.deploymentTemplateData, newCluster)
	if err != nil {
		return errors.Wrap(err, "handle pmm template data")
	}

	t, err := template.New(deploymentTemplateName).Parse(string(templateData))
	if err != nil {
		return errors.Wrap(err, "parse template")
	}

	config.DeploymentTemplate = t

	return nil
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
	specAnnotationsGlobal["keep-data"] = strconv.FormatBool(pgc.Spec.KeepData)

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
	cluster.Spec.PGOImagePrefix = "perconalab/percona-postgresql-operator"
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
	s, ok := pgc.Spec.Backup.Storages["s3"]
	if ok {
		cluster.Spec.BackrestS3Bucket = s.Bucket
		cluster.Spec.BackrestS3Endpoint = s.EndpointURL
		cluster.Spec.BackrestS3Region = s.Region
		cluster.Spec.BackrestS3URIStyle = s.URIStyle
		cluster.Spec.BackrestS3VerifyTLS = strconv.FormatBool(s.VerifyTLS)
	}
	cluster.Spec.BackrestStorageTypes = pgc.Spec.Backup.StorageTypes
	cluster.Spec.PGDataSource.Namespace = pgc.Spec.PGDataSource.Namespace
	cluster.Spec.PGDataSource.RestoreFrom = pgc.Spec.PGDataSource.RestoreFrom
	cluster.Spec.PGDataSource.RestoreOpts = pgc.Spec.PGDataSource.RestoreOpts
	cluster.Spec.ServiceType = pgc.Spec.PGPrimary.Expose.ServiceType
	cluster.Spec.TLSOnly = pgc.Spec.TlSOnly
	cluster.Spec.Standby = pgc.Spec.Standby
	cluster.Spec.Shutdown = pgc.Spec.Pause
	cluster.Spec.TablespaceMounts = pgc.Spec.TablespaceStorages
	cluster.Spec.WALStorage = pgc.Spec.WalStorage
	cluster.Spec.PGDataSource = pgc.Spec.PGDataSource

	return cluster
}

func getStorage(storageSpec *crv1.PgStorageSpec) crv1.PgStorageSpec {
	newStorage := crv1.PgStorageSpec{
		AccessMode:  "ReadWriteOnce",
		Size:        "1G",
		StorageType: "dynamic",
	}
	if storageSpec == nil {
		return newStorage
	}

	if len(storageSpec.AccessMode) > 0 {
		newStorage.AccessMode = storageSpec.AccessMode
	}

	if len(storageSpec.Size) > 0 {
		newStorage.Size = storageSpec.Size
	}

	if len(storageSpec.StorageType) > 0 {
		newStorage.StorageType = storageSpec.StorageType
	}

	newStorage.Name = storageSpec.Name
	newStorage.StorageClass = storageSpec.StorageClass
	newStorage.SupplementalGroups = storageSpec.SupplementalGroups
	newStorage.MatchLabels = storageSpec.MatchLabels

	return newStorage
}

// RunWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) RunWorker(stopCh <-chan struct{}, doneCh chan<- struct{}) {
	go c.waitForShutdown(stopCh)

	go c.reconcileStatuses(stopCh)

	deploymentTemplateData, err := ioutil.ReadFile(templatePath + deploymentTemplateName)
	if err != nil {
		log.Printf("new template data: %s", err)
		return
	}
	c.deploymentTemplateData = deploymentTemplateData

	log.Debug("perconapgcluster Contoller: worker queue has been shutdown, writing to the done channel")
	doneCh <- struct{}{}
}

// waitForShutdown waits for a message on the stop channel and then shuts down the work queue
func (c *Controller) waitForShutdown(stopCh <-chan struct{}) {
	<-stopCh
	c.Queue.ShutDown()
	log.Debug("perconapgcluster Contoller: received stop signal, worker queue told to shutdown")
}

func (c *Controller) reconcileStatuses(stopCh <-chan struct{}) {
	for {
		select {
		case <-stopCh:
			return
		default:
			err := c.handleStatuses()
			if err != nil {
				log.Error(errors.Wrap(err, "handle statuses"))
			}
			time.Sleep(5 * time.Second)
		}
	}
}

func (c *Controller) handleStatuses() error {
	ctx := context.TODO()
	ns, err := c.Client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return errors.Wrap(err, "get ns list")
	}
	for _, n := range ns.Items {
		perconaPGClusters, err := c.Client.CrunchydataV1().PerconaPGClusters(n.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil
		}
		for _, p := range perconaPGClusters.Items {
			pgCluster, err := c.Client.CrunchydataV1().Pgclusters(n.Name).Get(ctx, p.Name, metav1.GetOptions{})
			if err != nil {
				return errors.Wrap(err, "get pgCluster")
			}
			replStatuses := make(map[string]crv1.PgreplicaStatus)
			selector := config.LABEL_PG_CLUSTER + "=" + p.Name
			pgReplicas, err := c.Client.CrunchydataV1().Pgreplicas(n.Name).List(ctx, metav1.ListOptions{LabelSelector: selector})
			if err != nil {
				return errors.Wrap(err, "get pgReplicas list")
			}
			for _, repl := range pgReplicas.Items {
				replStatuses[repl.Name] = repl.Status
			}
			if reflect.DeepEqual(p.Status.PGCluster, pgCluster.Status) && reflect.DeepEqual(p.Status.PGReplicas, replStatuses) {
				return nil
			}

			value := crv1.PerconaPGClusterStatus{
				PGCluster:  pgCluster.Status,
				PGReplicas: replStatuses,
			}

			patch, err := kubeapi.NewJSONPatch().Replace("status")(value).Bytes()
			if err != nil {
				return errors.Wrap(err, "create patch bytes")
			}
			_, err = c.Client.CrunchydataV1().PerconaPGClusters(p.Namespace).
				Patch(ctx, p.Name, types.JSONPatchType, patch, metav1.PatchOptions{})
			if err != nil {
				return errors.Wrap(err, "patch percona status")
			}
		}
	}

	return nil
}

// onUpdate is called when a pgcluster is updated
func (c *Controller) onUpdate(oldObj, newObj interface{}) {
	ctx := context.TODO()

	oldCluster := oldObj.(*crv1.PerconaPGCluster)
	newCluster := newObj.(*crv1.PerconaPGCluster)

	if reflect.DeepEqual(oldCluster.Spec, newCluster.Spec) {
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(newObj)
	if err == nil {
		log.Debugf("percona cluster putting key in queue %s", key)

	}

	keyParts := strings.Split(key, "/")
	keyNamespace := keyParts[0]

	err = c.updateTemplate(newCluster)
	if err != nil {
		log.Errorf("update deployment template: %s", err)
	}

	if !reflect.DeepEqual(oldCluster.Spec.PGPrimary.Expose, newCluster.Spec.PGPrimary.Expose) {
		err = createOrUpdatePrimaryService(c.Client, newCluster)
		if err != nil {
			log.Errorf("handle primary service on update")
			return
		}
	}

	oldPGCluster, err := c.Client.CrunchydataV1().Pgclusters(oldCluster.Namespace).Get(ctx, oldCluster.Name, metav1.GetOptions{})
	if err != nil {
		log.Errorf("get old pgcluster resource: %s", err)
		return
	}

	pgCluster := getPGCLuster(newCluster, oldPGCluster)

	err = replica.Update(c.Client, newCluster, oldCluster)
	if err != nil {
		log.Errorf("update pgreplicas: %s", err)
	}

	if !reflect.DeepEqual(oldCluster.Spec.PMM, newCluster.Spec.PMM) {
		deployment, err := c.Client.AppsV1().Deployments(pgCluster.Namespace).Get(ctx,
			pgCluster.Name, metav1.GetOptions{})
		if err != nil {
			log.Errorf("could not find instance for pgcluster: %q", err.Error())
			return
		}
		err = pmm.UpdatePMMSidecar(c.Client, pgCluster, deployment)
		if err != nil {
			log.Errorf("update pmm sidecar: %q", err.Error())
		}
		if _, err := c.Client.AppsV1().Deployments(deployment.Namespace).Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
			log.Errorf("could not update deployment for pgcluster: %q", err.Error())
		}
	}

	_, err = c.Client.CrunchydataV1().Pgclusters(keyNamespace).Update(ctx, pgCluster, metav1.UpdateOptions{})
	if err != nil {
		log.Errorf("update pgcluster resource: %s", err)
		return
	}
}

// onDelete is called when a pgcluster is deleted
func (c *Controller) onDelete(obj interface{}) {
	ctx := context.TODO()

	// TODO: this object trick should be rework
	clusterObj, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		log.Errorln("delete cluster: object is not DeletedFinalStateUnknown")
		return
	}
	cluster, ok := clusterObj.Obj.(*crv1.PerconaPGCluster)
	if !ok {
		log.Errorln("delete cluster: object is not PerconaPGCluster")
		return
	}

	err := c.Client.CrunchydataV1().Pgclusters(cluster.Namespace).Delete(ctx, cluster.Name, metav1.DeleteOptions{})
	if err != nil {
		log.Errorf("delete pgcluster resource: %s", err)
	}
}

// AddPerconaPGClusterEventHandler adds the pgcluster event handler to the pgcluster informer
func (c *Controller) AddPerconaPGClusterEventHandler() {
	c.Informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onAdd,
		UpdateFunc: c.onUpdate,
		DeleteFunc: c.onDelete,
	})

	log.Debugf("percona pgcluster Controller: added event handler to informer")
}

// WorkerCount returns the worker count for the controller
func (c *Controller) WorkerCount() int {
	return c.PerconaPGClusterWorkerCount
}

func createOrUpdatePrimaryService(clientset kubeapi.Interface, cluster *crv1.PerconaPGCluster) error {
	ctx := context.TODO()
	service, err := getPrimaryServiceObject(cluster)
	if err != nil {
		return errors.Wrap(err, "get primary service object")
	}

	oldSvc, err := clientset.CoreV1().Services(cluster.Namespace).Get(ctx, cluster.Name, metav1.GetOptions{})
	if err != nil {
		_, err = clientset.CoreV1().Services(cluster.Namespace).Create(ctx, service, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrap(err, "create primary service")
		}
		return nil
	}
	if reflect.DeepEqual(service.Spec, oldSvc.Spec) {
		return nil
	}
	service.ResourceVersion = oldSvc.ResourceVersion
	service.Spec.ClusterIP = oldSvc.Spec.ClusterIP
	_, err = clientset.CoreV1().Services(cluster.Namespace).Update(ctx, service, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrap(err, "update primary service")
	}

	return nil
}

func getPrimaryServiceObject(cluster *crv1.PerconaPGCluster) (*corev1.Service, error) {

	labels := map[string]string{
		"name":       cluster.Name,
		"pg-cluster": cluster.Name,
	}

	for k, v := range cluster.Spec.PGPrimary.Expose.Labels {
		labels[k] = v
	}

	port, err := strconv.Atoi(cluster.Spec.Port)
	if err != nil {
		return &corev1.Service{}, errors.Wrap(err, "parse port")
	}
	svcType := corev1.ServiceTypeClusterIP
	if len(cluster.Spec.PGPrimary.Expose.ServiceType) > 0 {
		svcType = cluster.Spec.PGPrimary.Expose.ServiceType
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cluster.Name,
			Namespace:   cluster.Namespace,
			Labels:      labels,
			Annotations: cluster.Spec.PGPrimary.Expose.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Type: svcType,
			Ports: []corev1.ServicePort{
				{
					Name:       "sshd",
					Protocol:   corev1.ProtocolTCP,
					Port:       2022,
					TargetPort: intstr.FromInt(2022),
				},
				{
					Name:       "postgres",
					Protocol:   corev1.ProtocolTCP,
					Port:       int32(port),
					TargetPort: intstr.FromInt(port),
				},
			},
			Selector: map[string]string{
				"pg-cluster": cluster.Name,
				"role":       "master",
			},
			SessionAffinity:          corev1.ServiceAffinityNone,
			LoadBalancerSourceRanges: cluster.Spec.PGPrimary.Expose.LoadBalancerSourceRanges,
		},
	}, nil
}
