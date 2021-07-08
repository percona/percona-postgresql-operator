package pgc

import (
	"context"
	"crypto/md5"
	"fmt"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

	templateData, err := pmm.HandlePMMTemplate(c.deploymentTemplateData, newCluster)
	if err != nil {
		log.Printf("handle pmm template data: %s", err)
		return
	}

	t, err := template.New(deploymentTemplateName).Parse(string(templateData))
	if err != nil {
		log.Errorf("new template: %s", err)
		return
	}

	config.DeploymentTemplate = t

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
	cluster.Spec.BackrestStorage = crv1.PgStorageSpec{
		AccessMode:  "ReadWriteOnce",
		Size:        "1G",
		StorageType: "dynamic",
	}
	cluster.Spec.PrimaryStorage = crv1.PgStorageSpec{
		AccessMode:  "ReadWriteOnce",
		Size:        "1G",
		StorageType: "dynamic",
	}
	cluster.Spec.ReplicaStorage = crv1.PgStorageSpec{
		AccessMode:  "ReadWriteOnce",
		Size:        "1G",
		StorageType: "dynamic",
	}

	cluster.Spec.BackrestS3VerifyTLS = "true"
	cluster.Spec.ClusterName = pgc.Name
	cluster.Spec.PGImage = pgc.Spec.PGPrimary.Image
	cluster.Spec.BackrestImage = pgc.Spec.Backup.Image
	cluster.Spec.BackrestRepoImage = pgc.Spec.Backup.BackrestRepoImage
	cluster.Spec.BackrestResources = pgc.Spec.Backup.Resources.Requests
	cluster.Spec.BackrestLimits = pgc.Spec.Backup.Resources.Limits
	cluster.Spec.DisableAutofail = false
	cluster.Spec.Name = pgc.Name
	cluster.Spec.Database = pgc.Spec.Database
	cluster.Spec.PGBadger = false
	cluster.Spec.PgBouncer.Image = pgc.Spec.PGBouncer.Image
	cluster.Spec.PgBouncer.Replicas = pgc.Spec.PGBouncer.Size
	cluster.Spec.PgBouncer.Resources = pgc.Spec.PGBouncer.Resources.Requests
	cluster.Spec.PgBouncer.Limits = pgc.Spec.PGBouncer.Resources.Limits
	cluster.Spec.PGOImagePrefix = "perconalab/percona-postgresql-operator"
	cluster.Spec.PodAntiAffinity = crv1.PodAntiAffinitySpec{
		Default:    "preferred",
		PgBackRest: "preferred",
		PgBouncer:  "preferred",
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

	return cluster
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

	oldPGCluster, err := c.Client.CrunchydataV1().Pgclusters(oldCluster.Namespace).Get(ctx, oldCluster.Name, metav1.GetOptions{})
	if err != nil {
		log.Errorf("get old pgcluster resource: %s", err)
		return
	}

	pgCluster := getPGCLuster(newCluster, oldPGCluster)

	if oldCluster.Spec.PMM.Enabled != newCluster.Spec.PMM.Enabled {
		if pgCluster.Spec.Annotations.Global == nil {
			pgCluster.Spec.Annotations.Global = make(map[string]string)
		}
		pmmString := fmt.Sprintln(newCluster.Spec.PMM)
		hash := fmt.Sprintf("%x", md5.Sum([]byte(pmmString)))
		pgCluster.Spec.Annotations.Global["pmm-sidecar"] = hash
	}

	_, err = c.Client.CrunchydataV1().Pgclusters(keyNamespace).Update(ctx, pgCluster, metav1.UpdateOptions{})
	if err != nil {
		log.Errorf("update pgcluster resource: %s", err)
		return
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
	err = replica.Update(c.Client, newCluster, oldCluster)
	if err != nil {
		log.Errorf("update pgreplicas: %s", err)
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
