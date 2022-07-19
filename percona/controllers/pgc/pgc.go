package pgc

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/percona/percona-postgresql-operator/internal/config"
	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
	ns "github.com/percona/percona-postgresql-operator/internal/ns"
	"github.com/percona/percona-postgresql-operator/percona/controllers/pgbouncer"
	"github.com/percona/percona-postgresql-operator/percona/controllers/pgcluster"
	"github.com/percona/percona-postgresql-operator/percona/controllers/pgreplica"
	"github.com/percona/percona-postgresql-operator/percona/controllers/service"
	"github.com/percona/percona-postgresql-operator/percona/controllers/template"
	"github.com/percona/percona-postgresql-operator/percona/controllers/version"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
	informers "github.com/percona/percona-postgresql-operator/pkg/generated/informers/externalversions/crunchydata.com/v1"

	"github.com/pkg/errors"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// Controller holds the connections for the controller
type Controller struct {
	Client                      *kubeapi.Client
	Queue                       workqueue.RateLimitingInterface
	Informer                    informers.PerconaPGClusterInformer
	PerconaPGClusterWorkerCount int
	templates                   Templates
	crons                       CronRegistry
	lockers                     lockStore
	scheme                      *runtime.Scheme
}

type Templates struct {
	deploymentTemplateData             []byte
	backrestRepoDeploymentTemplateData []byte
	bouncerTemplateData                []byte
	pgBadgerTemplateData               []byte
	backrestJobTemplateData            []byte
}

type CronRegistry struct {
	crons             *cron.Cron
	ensureVersionJobs map[string]Schedule
}

type Schedule struct {
	ID           int
	CronSchedule string
}

func NewCronRegistry() CronRegistry {
	c := CronRegistry{
		crons:             cron.New(),
		ensureVersionJobs: make(map[string]Schedule),
	}

	c.crons.Start()

	return c
}

type lockStore struct {
	store *sync.Map
}

func newLockStore() lockStore {
	return lockStore{
		store: new(sync.Map),
	}
}

func (l lockStore) LoadOrCreate(key string) lock {
	val, _ := l.store.LoadOrStore(key, lock{
		statusMutex: new(sync.Mutex),
		updateSync:  new(int32),
	})

	return val.(lock)
}

type lock struct {
	statusMutex *sync.Mutex
	updateSync  *int32
}

const (
	updateDone = 0
	updateWait = 1
)

// onAdd is called when a pgcluster is added
func (c *Controller) onAdd(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		log.Printf("get key %s", err)
	}
	log.Debugf("percona cluster putting key in queue %s", key)

	c.Queue.Add(key)
	defer c.Queue.Done(key)

	newCluster := obj.(*crv1.PerconaPGCluster)
	nn := types.NamespacedName{
		Name:      newCluster.Name,
		Namespace: newCluster.Namespace,
	}
	if c.lockers.store == nil {
		c.lockers = newLockStore()
	}

	l := c.lockers.LoadOrCreate(nn.String())
	l.statusMutex.Lock()
	defer l.statusMutex.Unlock()

	err = version.EnsureVersion(c.Client, newCluster, version.VersionServiceClient{
		OpVersion: newCluster.ObjectMeta.Labels["pgo-version"],
	})
	if err != nil {
		log.Errorf("ensure version: %s", err)
	}

	newCluster.CheckAndSetDefaults()

	err = c.updateTemplates(newCluster)
	if err != nil {
		log.Errorf("update templates: %s", err)
		return
	}
	err = c.handleTLS(newCluster)
	if err != nil {
		log.Errorf("handle tls: %s", err)
		return
	}

	err = service.CreateOrUpdate(c.Client, newCluster, service.PGPrimaryServiceType)
	if err != nil {
		log.Errorf("handle primary service on create: %s", err)
		return
	}
	if newCluster.Spec.PGBouncer.Size > 0 {
		err = service.CreateOrUpdate(c.Client, newCluster, service.PGBouncerServiceType)
		if err != nil {
			log.Errorf("handle bouncer service on create: %s", err)
			return
		}
	}

	err = c.handleSecrets(newCluster)
	if err != nil {
		log.Errorf("handle secrets: %s", err)
		return
	}

	err = pgcluster.Create(c.Client, newCluster)
	if err != nil {
		log.Errorf("create pgcluster resource: %s", err)
		return
	}

	if newCluster.Spec.PGReplicas != nil {
		err = c.createReplicas(newCluster)
		if err != nil {
			log.Errorf("create pgreplicas: %s", err)
			return
		}
	}

	err = c.handleScheduleBackup(newCluster, nil)
	if err != nil {
		log.Errorf("handle schedule backups: %s", err)
		return
	}
	ctx := context.TODO()
	_, err = c.Client.CrunchydataV1().PerconaPGClusters(newCluster.Namespace).Update(ctx, newCluster, metav1.UpdateOptions{})
	if err != nil {
		log.Errorf("update perconapgcluster: %s", err)
	}

	c.Queue.Forget(key)
}

func (c *Controller) createReplicas(cluster *crv1.PerconaPGCluster) error {
	if cluster.Spec.PGReplicas.HotStandby.Size == 0 {
		return nil
	}
	err := service.CreateOrUpdate(c.Client, cluster, service.PGReplicaServiceType)
	if err != nil {
		return errors.Wrap(err, "handle replica service")
	}
	if cluster.Spec.PGReplicas == nil {
		return nil
	}
	for i := 1; i <= cluster.Spec.PGReplicas.HotStandby.Size; i++ {
		err = pgreplica.CreateReplicaResource(c.Client, cluster, i)
		if err != nil {
			return errors.Wrap(err, "create replica")
		}
	}

	return nil
}

func (c *Controller) updateTemplates(newCluster *crv1.PerconaPGCluster) error {
	err := template.UpdatePGBadgerTemplate(c.templates.pgBadgerTemplateData, newCluster, newCluster.Name)
	if err != nil {
		return errors.Wrap(err, "update pgbadger template")
	}
	err = template.UpdateDeploymentTemplate(c.templates.deploymentTemplateData, newCluster, newCluster.Name)
	if err != nil {
		return errors.Wrap(err, "update deployment template")
	}
	err = template.UpdateBackrestRepoTemplate(c.templates.backrestRepoDeploymentTemplateData, newCluster, newCluster.Name)
	if err != nil {
		return errors.Wrap(err, "update backrest repo deployment template")
	}
	err = template.UpdateBackrestJobTemplate(c.templates.backrestJobTemplateData, newCluster)
	if err != nil {
		return errors.Wrap(err, "update backrest job template")
	}
	err = template.UpdateBouncerTemplate(c.templates.bouncerTemplateData, newCluster, newCluster.Name)
	if err != nil {
		return errors.Wrap(err, "update bouncer deployment template")
	}

	return nil
}

// RunWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) RunWorker(stopCh <-chan struct{}, doneCh chan<- struct{}) {
	go c.waitForShutdown(stopCh)
	go c.reconcilePerconaPG(stopCh)
	c.crons = NewCronRegistry()
	c.lockers = newLockStore()

	err := c.setControllerTemplatesData()
	if err != nil {
		log.Printf("set controller template data: %s", err)
		return
	}

	log.Debug("perconapgcluster Contoller: worker queue has been shutdown, writing to the done channel")
	doneCh <- struct{}{}
}

func (c *Controller) setControllerTemplatesData() error {
	deploymentTemplateData, err := ioutil.ReadFile(template.Path + template.ClusterDeploymentTemplateName)
	if err != nil {
		return errors.Wrap(err, "new cluster template data")
	}
	backrestRepodeploymentTemplateData, err := ioutil.ReadFile(template.Path + template.BackrestRepoDeploymentTemplateName)
	if err != nil {
		return errors.Wrap(err, "new backrest repo template data")
	}
	bouncerTemplateData, err := ioutil.ReadFile(template.Path + template.BouncerDeploymentTemplateName)
	if err != nil {
		return errors.Wrap(err, "new bouncer template data")
	}
	pgBadgerTemplateData, err := ioutil.ReadFile(template.Path + template.PGBadgerTemplateName)
	if err != nil {
		return errors.Wrap(err, "new pgBadger template data")
	}
	backrestJobTemplateData, err := ioutil.ReadFile(template.Path + template.BackrestJobTemplateName)
	if err != nil {
		return errors.Wrap(err, "new backrestJob template data")
	}
	c.templates.deploymentTemplateData = deploymentTemplateData
	c.templates.backrestRepoDeploymentTemplateData = backrestRepodeploymentTemplateData
	c.templates.bouncerTemplateData = bouncerTemplateData
	c.templates.pgBadgerTemplateData = pgBadgerTemplateData
	c.templates.backrestJobTemplateData = backrestJobTemplateData

	return nil
}

// waitForShutdown waits for a message on the stop channel and then shuts down the work queue
func (c *Controller) waitForShutdown(stopCh <-chan struct{}) {
	<-stopCh
	c.Queue.ShutDown()
	log.Debug("perconapgcluster Contoller: received stop signal, worker queue told to shutdown")
}

func (c *Controller) reconcilePerconaPG(stopCh <-chan struct{}) {
	for {
		select {
		case <-stopCh:
			return
		default:
			err := c.reconcilePerconaPGClusters()
			if err != nil {
				log.Error(errors.Wrap(err, "reconcile perocnapgclusters"))
			}
			time.Sleep(5 * time.Second)
		}
	}
}

func (c *Controller) getOperatorNamespaceList() ([]string, error) {
	nsOpMode, err := ns.GetNamespaceOperatingMode(c.Client)
	if err != nil {
		return nil, errors.Wrap(err, "get namespaceOperatingMode")
	}
	installationName := os.Getenv("PGO_INSTALLATION_NAME")
	if installationName == "" {
		return nil, errors.New("PGO_INSTALLATION_NAME env var is not set")
	}
	pgoNamespace := os.Getenv("PGO_OPERATOR_NAMESPACE")
	if pgoNamespace == "" {
		return nil, errors.New("PGO_OPERATOR_NAMESPACE env var is not set")
	}
	namespaces, err := ns.GetInitialNamespaceList(c.Client, nsOpMode, installationName, pgoNamespace)
	if err != nil {
		return nil, errors.Wrap(err, "get namespaces list")
	}

	return namespaces, nil
}

func (c *Controller) reconcilePerconaPGClusters() error {
	ctx := context.TODO()
	namespaces, err := c.getOperatorNamespaceList()
	if err != nil {
		return errors.Wrap(err, "get operator ns list")
	}
	for _, namespace := range namespaces {
		perconaPGClusters, err := c.Client.CrunchydataV1().PerconaPGClusters(namespace).List(ctx, metav1.ListOptions{})
		if err != nil && !kerrors.IsNotFound(err) {
			return errors.Wrap(err, "list perconapgclusters")
		} else if err != nil {
			// there is no perconapgclusters, so no need to continue
			continue
		}
		for _, pi := range perconaPGClusters.Items {
			nn := types.NamespacedName{
				Name:      pi.Name,
				Namespace: pi.Namespace,
			}
			l := c.lockers.LoadOrCreate(nn.String())
			l.statusMutex.Lock()

			p, err := c.Client.CrunchydataV1().PerconaPGClusters(pi.Namespace).Get(ctx, pi.Name, metav1.GetOptions{})
			if err != nil && !kerrors.IsNotFound(err) {
				return errors.Wrap(err, "get perconapgluster")
			}

			err = c.reconcileUsers(p)
			if err != nil {
				log.Error(errors.Wrap(err, "reconcile users"))
			}
			err = c.reconcileStatus(p, l.statusMutex)
			if err != nil {
				log.Error(errors.Wrap(err, "reconcile status"))
			}
		}
	}

	return nil
}

func (c *Controller) reconcileStatus(cluster *crv1.PerconaPGCluster, statusMutex *sync.Mutex) error {
	defer statusMutex.Unlock()

	ctx := context.TODO()
	pgCluster, err := c.Client.CrunchydataV1().Pgclusters(cluster.Namespace).Get(ctx, cluster.Name, metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.Wrap(err, "get pgCluster")
	}
	if pgCluster == nil {
		return nil
	}
	pgClusterStatus := pgCluster.Status
	replStatuses := make(map[string]crv1.PgreplicaStatus)

	selector := config.LABEL_PG_CLUSTER + "=" + cluster.Name
	pgReplicas, err := c.Client.CrunchydataV1().Pgreplicas(cluster.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.Wrap(err, "get pgReplicas list")
	}
	if pgReplicas != nil {
		for _, repl := range pgReplicas.Items {
			replStatuses[repl.Name] = repl.Status
		}
	}

	if reflect.DeepEqual(cluster.Status.PGCluster, pgCluster.Status) && reflect.DeepEqual(cluster.Status.PGReplicas, replStatuses) {
		return nil
	}

	cluster.Status = crv1.PerconaPGClusterStatus{
		PGCluster:         pgClusterStatus,
		PGReplicas:        replStatuses,
		Size:              int32(1 + len(replStatuses)),
		LabelSelectorPath: labels.SelectorFromSet(cluster.Labels).String(),
	}
	_, err = c.Client.CrunchydataV1().PerconaPGClusters(cluster.Namespace).UpdateStatus(ctx, cluster, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrap(err, "update perconapgcluster status")
	}

	return nil
}

func (c *Controller) reconcileUsers(cluster *crv1.PerconaPGCluster) error {
	ctx := context.TODO()
	usersSecretName := cluster.Name + "-users"
	if len(cluster.Spec.UsersSecretName) > 0 {
		usersSecretName = cluster.Spec.UsersSecretName
	}
	usersSecret, err := c.Client.CoreV1().Secrets(cluster.Namespace).Get(ctx, usersSecretName, metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.Wrapf(err, "get secret %s", usersSecretName)
	} else if kerrors.IsNotFound(err) {
		return nil
	}
	oldHash := ""
	if hash, ok := usersSecret.Annotations[annotationLastAppliedSecret]; ok {
		oldHash = hash
	}
	secretData, err := json.Marshal(usersSecret.Data)
	if err != nil {
		return errors.Wrap(err, "marshal users secret data")
	}
	newSecretDataHash := sha256Hash(secretData)
	if oldHash == newSecretDataHash {
		return nil
	}
	err = c.UpdateUsers(usersSecret, cluster.Name, cluster.Namespace)
	if err != nil {
		return errors.Wrap(err, "update users")
	}
	if usersSecret.Annotations == nil {
		usersSecret.Annotations = make(map[string]string)
	}
	usersSecret.Annotations[annotationLastAppliedSecret] = newSecretDataHash
	_, err = c.Client.CoreV1().Secrets(cluster.Namespace).Update(ctx, usersSecret, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrapf(err, "update secret %s", usersSecret.Name)
	}
	return nil
}

// onUpdate is called when a perconapgcluster is updated
func (c *Controller) onUpdate(oldObj, newObj interface{}) {
	oldCluster := oldObj.(*crv1.PerconaPGCluster)
	newCluster := newObj.(*crv1.PerconaPGCluster)

	if reflect.DeepEqual(oldCluster.Spec, newCluster.Spec) {
		return
	}
	if oldCluster.Spec.Pause && newCluster.Spec.Pause {
		return
	}

	newCluster.CheckAndSetDefaults()
	err := c.updateTemplates(newCluster)
	if err != nil {
		log.Errorf("update perconapgcluster: update templates: %s", err)
		return
	}
	err = c.handleInternalSecrets(newCluster)
	if err != nil {
		log.Errorf("update perconapgcluster: handle internal secrets: %s", err)
		return
	}
	nn := types.NamespacedName{
		Name:      newCluster.Name,
		Namespace: newCluster.Namespace,
	}
	l := c.lockers.LoadOrCreate(nn.String())
	l.statusMutex.Lock()
	defer l.statusMutex.Unlock()
	defer atomic.StoreInt32(l.updateSync, updateDone)
	err = c.handleTLS(newCluster)
	if err != nil {
		log.Errorf("handle tls: %s", err)
		return
	}
	err = version.EnsureVersion(c.Client, newCluster, version.VersionServiceClient{
		OpVersion: newCluster.ObjectMeta.Labels["pgo-version"],
	})
	if err != nil {
		log.Errorf("update perconapgcluster: ensure version %s", err)
	}
	err = c.updateVersion(oldCluster, newCluster)
	if err != nil {
		log.Errorf("update perconapgcluster:update version: %s", err)
	}
	err = c.scheduleUpdate(newCluster)
	if err != nil {
		log.Errorf("update perconapgcluster: scheduled update: %s", err)
	}

	if !reflect.DeepEqual(oldCluster.Spec.PGPrimary.Expose, newCluster.Spec.PGPrimary.Expose) {
		err = service.CreateOrUpdate(c.Client, newCluster, service.PGPrimaryServiceType)
		if err != nil {
			log.Errorf("update perconapgcluster: handle primary service on update: %s", err)
			return
		}
	}

	err = pgbouncer.Update(c.Client, newCluster, oldCluster)
	if err != nil {
		log.Errorf("update perconapgcluster: update bouncer: %s", err)
		return
	}

	err = c.handleScheduleBackup(newCluster, oldCluster)
	if err != nil {
		log.Errorf("update perconapgcluster: handle schedule: %s", err)
	}
	primary, err := pgcluster.IsPrimary(c.Client, oldCluster)
	if err != nil {
		log.Errorf("update perconapgcluster: check is pgcluster primary: %s", err)
	}
	if primary {
		err = pgreplica.Update(c.Client, newCluster, oldCluster)
		if err != nil {
			log.Errorf("update perconapgcluster: update pgreplica: %s", err)
			return
		}
		err = pgcluster.Update(c.Client, newCluster, oldCluster)
		if err != nil {
			log.Errorf("update perconapgcluster: update pgcluster: %s", err)
			return
		}
	} else {
		err = pgcluster.Update(c.Client, newCluster, oldCluster)
		if err != nil {
			log.Errorf("update perconapgcluster: update pgcluster: %s", err)
			return
		}
		err = pgreplica.Update(c.Client, newCluster, oldCluster)
		if err != nil {
			log.Errorf("update perconapgcluster: update pgreplica: %s", err)
			return
		}
	}
	if oldCluster.Spec.TlSOnly != newCluster.Spec.TlSOnly {
		err = pgcluster.RestartPgBouncer(c.Client, newCluster)
		if err != nil {
			log.Errorf("update perconapgcluster: restart pgbouncer: %s", err)
		}
	}
}

// onDelete is called when a pgcluster is deleted
func (c *Controller) onDelete(obj interface{}) {
	cluster, ok := obj.(*crv1.PerconaPGCluster)
	if !ok {
		log.Errorln("delete cluster: object is not PerconaPGCluster")
		return
	}
	err := pgcluster.Delete(c.Client, cluster)
	if err != nil {
		log.Errorf("delete cluster: %s", err)
	}
	err = c.DeleteSecrets(cluster)
	if err != nil {
		log.Errorf("delete secrets: %s", err)
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

func PrepareForRestore(clientset *kubeapi.Client, clusterName, namespace string) error {
	ctx := context.TODO()
	err := deleteDatabasePods(clientset, clusterName, namespace)
	if err != nil {
		log.Errorf("get pods: %s", err.Error())
	}
	// Delete the DCS and leader ConfigMaps.  These will be recreated during the restore.
	configMaps := []string{
		fmt.Sprintf("%s-config", clusterName),
		fmt.Sprintf("%s-leader", clusterName),
	}
	for _, c := range configMaps {
		if err := clientset.CoreV1().ConfigMaps(namespace).
			Delete(ctx, c, metav1.DeleteOptions{}); err != nil && !kerrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func deleteDatabasePods(clientset *kubeapi.Client, clusterName, namespace string) error {
	ctx := context.TODO()
	pgInstances, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,%s", config.LABEL_PG_CLUSTER, clusterName,
			config.LABEL_PG_DATABASE),
	})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.Wrap(err, "get pods")
	}
	if pgInstances == nil {
		return nil
	}
	// Wait for all primary and replica pods to be removed.
	err = wait.Poll(time.Second/4, time.Minute*3, func() (bool, error) {
		for _, pods := range pgInstances.Items {
			if _, err := clientset.CoreV1().Pods(namespace).
				Get(ctx, pods.GetName(), metav1.GetOptions{}); err == nil || !kerrors.IsNotFound(err) {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return errors.Wrap(err, "wait pods termination")
	}

	return nil
}
