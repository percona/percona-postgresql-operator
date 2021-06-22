package pgc

import (
	"context"
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"reflect"
	"strings"
	"text/template"

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
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// Controller holds the connections for the controller
type Controller struct {
	Client                      *kubeapi.Client
	Queue                       workqueue.RateLimitingInterface
	Informer                    informers.PerconaPGClusterInformer
	PerconaPGClusterWorkerCount int
}

const (
	deploymentTemplateName = "cluster-deployment.json"
	templatePath           = "/"
)

// onAdd is called when a pgcluster is added
func (c *Controller) onAdd(obj interface{}) {
	ctx := context.TODO()
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err == nil {
		log.Debugf("percona cluster putting key in queue %s", key)
		c.Queue.Add(key)
	}

	newCluster := obj.(*crv1.PerconaPGCluster)
	fmt.Println("My name is", newCluster.Name)

	templateData, err := ioutil.ReadFile(templatePath + deploymentTemplateName)
	if err != nil {
		fmt.Printf("new template data: %s", err)
	}

	templateData, err = pmm.HandlePMMTemplate(templateData, newCluster)
	if err != nil {
		fmt.Printf("handle pmm template data: %s", err)
	}

	t, err := template.New(deploymentTemplateName).Parse(string(templateData))
	if err != nil {
		fmt.Printf("new template: %s", err)
	}

	config.DeploymentTemplate = t

	cluster := getPGCLuster(newCluster)

	_, err = c.Client.CrunchydataV1().Pgclusters(newCluster.Namespace).Create(ctx, &cluster, metav1.CreateOptions{})
	if err != nil {
		log.Errorf("create pgcluster resource: %s", err)
	}

	if newCluster.Spec.PGReplicas != nil {
		err = replica.Create(c.Client, newCluster)
		if err != nil {
			log.Errorf("create pgreplicas: %s", err)
		}
	}

}

func getPGCLuster(pgc *crv1.PerconaPGCluster) crv1.Pgcluster {
	metaAnnotations := map[string]string{
		"current-primary": pgc.Name,
	}
	metaLabels := map[string]string{
		"crunchy-pgha-scope": pgc.Name,
		"deployment-name":    pgc.Name,
		"name":               pgc.Name,
		"pg-cluster":         pgc.Name,
		"pgo-version":        "0.1.0",
		"pgouser":            "admin",
	}
	syncReplication := false
	if pgc.Spec.PGReplicas != nil {
		syncReplication = pgc.Spec.PGReplicas.HotStandby.EnableSyncStandby
	}
	return crv1.Pgcluster{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: metaAnnotations,
			Labels:      metaLabels,
			Name:        pgc.Name,
			Namespace:   pgc.Spec.Namespace,
		},
		Spec: crv1.PgclusterSpec{
			BackrestStorage: crv1.PgStorageSpec{
				AccessMode:  "ReadWriteOnce",
				Size:        "1G",
				StorageType: "dynamic",
			},
			PrimaryStorage: crv1.PgStorageSpec{
				AccessMode:  "ReadWriteOnce",
				Size:        "1G",
				StorageType: "dynamic",
			},
			ReplicaStorage: crv1.PgStorageSpec{
				AccessMode:  "ReadWriteOnce",
				Size:        "1G",
				StorageType: "dynamic",
			},
			BackrestResources: v1.ResourceList{
				v1.ResourceMemory: resource.MustParse("48Mi"),
			},
			BackrestS3VerifyTLS: "true",
			ClusterName:         pgc.Name,
			PGImage:             pgc.Spec.PGPrimary.Image,
			BackrestImage:       "perconalab/percona-postgresql-operator:main-ppg13-pgbackrest",
			BackrestRepoImage:   "perconalab/percona-postgresql-operator:main-ppg13-pgbackrest-repo",
			DisableAutofail:     false,
			Name:                pgc.Name,
			Database:            pgc.Spec.Database,
			PGBadger:            false,
			PgBouncer: crv1.PgBouncerSpec{
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
			},
			PGOImagePrefix: "perconalab/percona-postgresql-operator",
			PodAntiAffinity: crv1.PodAntiAffinitySpec{
				Default:    "preferred",
				PgBackRest: "preferred",
				PgBouncer:  "preferred",
			},
			Port: pgc.Spec.Port,
			Resources: v1.ResourceList{
				v1.ResourceMemory: resource.MustParse("128Mi"),
			},
			User:            pgc.Spec.User,
			UserLabels:      pgc.Spec.UserLabels,
			SyncReplication: &syncReplication,
		},
	}
}

// RunWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) RunWorker(stopCh <-chan struct{}, doneCh chan<- struct{}) {
	go c.waitForShutdown(stopCh)

	for c.processNextItem() {
	}

	log.Debug("perconapgcluster Contoller: worker queue has been shutdown, writing to the done channel")
	doneCh <- struct{}{}
}

// waitForShutdown waits for a message on the stop channel and then shuts down the work queue
func (c *Controller) waitForShutdown(stopCh <-chan struct{}) {
	<-stopCh
	c.Queue.ShutDown()
	log.Debug("perconapgcluster Contoller: received stop signal, worker queue told to shutdown")
}

func (c *Controller) processNextItem() bool {
	// Wait until there is a new item in the working queue
	key, quit := c.Queue.Get()
	if quit {
		return false
	}

	log.Debugf("working on %s", key.(string))
	keyParts := strings.Split(key.(string), "/")
	keyNamespace := keyParts[0]
	keyResourceName := keyParts[1]

	log.Debugf("cluster add queue got key ns=[%s] resource=[%s]", keyNamespace, keyResourceName)

	// Tell the queue that we are done with processing this key. This unblocks the key for other workers
	// This allows safe parallel processing because two pods with the same key are never processed in
	// parallel.
	defer c.Queue.Done(key)

	return true
}

// onUpdate is called when a pgcluster is updated
func (c *Controller) onUpdate(oldObj, newObj interface{}) {
	ctx := context.TODO()

	oldCluster := oldObj.(*crv1.PerconaPGCluster)
	newCluster := newObj.(*crv1.PerconaPGCluster)
	fmt.Println(oldCluster.CreationTimestamp.Minute(), newCluster.CreationTimestamp.Minute())
	if reflect.DeepEqual(oldCluster.Spec, newCluster.Spec) {
		return
	}
	key, err := cache.MetaNamespaceKeyFunc(newObj)
	if err == nil {
		log.Debugf("percona cluster putting key in queue %s", key)

	}

	keyParts := strings.Split(key, "/")
	keyNamespace := keyParts[0]

	cl, err := c.Client.CrunchydataV1().Pgclusters(keyNamespace).Get(ctx, oldCluster.Name, metav1.GetOptions{})
	if err != nil {
		log.Errorf("update pgcluster resource: %s", err)
	}

	cluster := getPGCLuster(newCluster)
	cluster.ResourceVersion = cl.ResourceVersion
	if oldCluster.Spec.PMM.Enabled != newCluster.Spec.PMM.Enabled {
		if cluster.Annotations == nil {
			cluster.Annotations = make(map[string]string)
		}
		pmmString := fmt.Sprintln(newCluster.Spec.PMM)
		hash := fmt.Sprintf("%x", md5.Sum([]byte(pmmString)))
		cluster.Annotations["pmm-sidecar"] = hash
	}
	_, err = c.Client.CrunchydataV1().Pgclusters(keyNamespace).Update(ctx, &cluster, metav1.UpdateOptions{})
	if err != nil {
		log.Errorf("update pgcluster resource: %s", err)
	}

	err = replica.Update(c.Client, newCluster, oldCluster)
	if err != nil {
		log.Errorf("update pgreplicas: %s", err)
	}
}

// onDelete is called when a pgcluster is deleted
func (c *Controller) onDelete(obj interface{}) {
	ctx := context.TODO()
	fmt.Println("the type is:", reflect.TypeOf(obj))

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

// AddPGClusterEventHandler adds the pgcluster event handler to the pgcluster informer
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

	err = pmm.AddPMMSidecar(cl, cluster, deployment)
	if err != nil {
		return errors.Wrap(err, "add pmm resources: %s")
	}
	err = replica.UpdateResources(cl, cluster, deployment)
	if err != nil {
		return errors.Wrap(err, "update replica resources resource: %s")
	}
	return nil
}
