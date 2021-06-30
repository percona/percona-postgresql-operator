package pgc

import (
	"context"
	"crypto/md5"
	"encoding/json"
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
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
	if cluster.Labels != nil {
		for k, v := range cluster.Labels {
			metaLabels[k] = v
		}
	}
	userLabels := make(map[string]string)
	if pgc.Labels != nil {
		for k, v := range pgc.Labels {
			userLabels[k] = v
		}
		for k, v := range pgc.Spec.UserLabels {
			userLabels[k] = v
		}
	}
	syncReplication := false
	replicas := ""
	if pgc.Spec.PGReplicas != nil {
		syncReplication = pgc.Spec.PGReplicas.HotStandby.EnableSyncStandby
		replicas = strconv.Itoa(pgc.Spec.PGReplicas.HotStandby.Size)
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
	cluster.Spec.BackrestResources = v1.ResourceList{
		v1.ResourceMemory: resource.MustParse("48Mi"),
	}
	cluster.Spec.BackrestS3VerifyTLS = "true"
	cluster.Spec.ClusterName = pgc.Name
	cluster.Spec.PGImage = pgc.Spec.PGPrimary.Image
	cluster.Spec.BackrestImage = "perconalab/percona-postgresql-operator:main-ppg13-pgbackrest"
	cluster.Spec.BackrestRepoImage = "perconalab/percona-postgresql-operator:main-ppg13-pgbackrest-repo"
	cluster.Spec.DisableAutofail = false
	cluster.Spec.Name = pgc.Name
	cluster.Spec.Database = pgc.Spec.Database
	cluster.Spec.PGBadger = false
	cluster.Spec.PgBouncer = crv1.PgBouncerSpec{
		Image:    "perconalab/percona-postgresql-operator:main-ppg13-pgbouncer",
		Replicas: 1,
		Resources: v1.ResourceList{
			v1.ResourceMemory: resource.MustParse("128Mi"),
			v1.ResourceCPU:    resource.MustParse("1"),
		},
		Limits: v1.ResourceList{
			v1.ResourceMemory: resource.MustParse("512Mi"),
			v1.ResourceCPU:    resource.MustParse("2"),
		},
	}
	cluster.Spec.PGOImagePrefix = "perconalab/percona-postgresql-operator"
	cluster.Spec.PodAntiAffinity = crv1.PodAntiAffinitySpec{
		Default:    "preferred",
		PgBackRest: "preferred",
		PgBouncer:  "preferred",
	}
	cluster.Spec.Port = pgc.Spec.Port
	cluster.Spec.Resources = v1.ResourceList{
		v1.ResourceMemory: resource.MustParse("128Mi"),
	}
	cluster.Spec.User = pgc.Spec.User
	cluster.Spec.UserLabels = pgc.Spec.UserLabels
	cluster.Spec.SyncReplication = &syncReplication
	cluster.Spec.UserLabels = userLabels
	cluster.Spec.Replicas = replicas

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
				fmt.Printf("handle statuses: %s", err)
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
			return errors.Wrap(err, "get percona clusters list")
		}
		for _, p := range perconaPGClusters.Items {
			replStatuses := make(map[string]crv1.PgreplicaStatus)
			selector := config.LABEL_PG_CLUSTER + "=" + p.Name
			pgReplicas, err := c.Client.CrunchydataV1().Pgreplicas(n.Name).List(ctx, metav1.ListOptions{LabelSelector: selector})
			if err != nil {
				return errors.Wrap(err, "get pgReplicas list")
			}
			for _, repl := range pgReplicas.Items {
				replStatuses[repl.Name] = repl.Status
			}
			pgCluster, err := c.Client.CrunchydataV1().Pgclusters(n.Name).Get(ctx, p.Name, metav1.GetOptions{})
			if err != nil {
				return errors.Wrap(err, "get pgCluster")
			}
			patch, err := json.Marshal(map[string]interface{}{
				"status": crv1.PerconaPGClusterStatus{
					PGCluster:  pgCluster.Status,
					PGReplicas: replStatuses,
				},
			})
			if err != nil {
				return errors.Wrap(err, "marshal percona status")
			}

			_, err = c.Client.CrunchydataV1().PerconaPGClusters(p.Namespace).
				Patch(ctx, p.Name, types.MergePatchType, patch, metav1.PatchOptions{})
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
		if pgCluster.Annotations == nil {
			pgCluster.Annotations = make(map[string]string)
		}
		pmmString := fmt.Sprintln(newCluster.Spec.PMM)
		hash := fmt.Sprintf("%x", md5.Sum([]byte(pmmString)))
		pgCluster.Annotations["pmm-sidecar"] = hash
	}
	_, err = c.Client.CrunchydataV1().Pgclusters(keyNamespace).Update(ctx, pgCluster, metav1.UpdateOptions{})
	if err != nil {
		log.Errorf("update pgcluster resource: %s", err)
		return
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

func UpdateDeployment(clientset kubeapi.Interface, cluster *crv1.Pgcluster, deployment *appsv1.Deployment) error {
	ctx := context.TODO()
	cl, err := clientset.CrunchydataV1().PerconaPGClusters(cluster.Namespace).Get(ctx, cluster.Name, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, "get perconapgcluster resource: %s")
	}
	replica.UpdateAnnotations(cl, deployment)
	replica.UpdateLabels(cl, deployment)
	err = pmm.AddPMMSidecar(cl, cluster, deployment)
	if err != nil {
		return errors.Wrap(err, "add pmm resources: %s")
	}
	err = replica.UpdateResources(cl, deployment)
	if err != nil {
		return errors.Wrap(err, "update replica resources resource: %s")
	}

	return nil
}
