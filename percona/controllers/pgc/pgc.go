package pgc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"reflect"
	"strings"
	"text/template"
	"time"

	"github.com/percona/percona-postgresql-operator/internal/config"
	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
	"github.com/percona/percona-postgresql-operator/internal/operator"
	"github.com/percona/percona-postgresql-operator/percona/controllers/pgcluster"
	"github.com/percona/percona-postgresql-operator/percona/controllers/pgreplica"
	"github.com/percona/percona-postgresql-operator/percona/controllers/pmm"
	"github.com/percona/percona-postgresql-operator/percona/controllers/service"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
	informers "github.com/percona/percona-postgresql-operator/pkg/generated/informers/externalversions/crunchydata.com/v1"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	deploymentTemplateData      []byte
}

const (
	deploymentTemplateName = "cluster-deployment.json"
	templatePath           = "/"
	defaultSecurityContext = `{"fsGroup": 26,"supplementalGroups": [1001]}`
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
	err = c.updateTemplate(newCluster)
	if err != nil {
		log.Errorf("update deployment template: %s", err)
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

	err = pgcluster.Create(c.Client, newCluster)
	if err != nil {
		log.Errorf("create pgcluster resource: %s", err)
	}

	if newCluster.Spec.PGReplicas != nil {
		err = pgreplica.Create(c.Client, newCluster)
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
	templateData, err = handleSecurityContextTemplate(templateData, newCluster)
	if err != nil {
		return errors.Wrap(err, "handle security context template data")
	}
	t, err := template.New(deploymentTemplateName).Parse(string(templateData))
	if err != nil {
		return errors.Wrap(err, "parse template")
	}

	config.DeploymentTemplate = t

	return nil
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
		if err != nil && !strings.Contains(err.Error(), "not found") {
			return errors.Wrap(err, "list perconapgclusters")
		} else if err != nil {
			// there is no perconapgclusters, so no need to continue
			return nil
		}
		for _, p := range perconaPGClusters.Items {
			pgClusterStatus := crv1.PgclusterStatus{}
			replStatuses := make(map[string]crv1.PgreplicaStatus)
			pgCluster, err := c.Client.CrunchydataV1().Pgclusters(n.Name).Get(ctx, p.Name, metav1.GetOptions{})
			if err != nil && !strings.Contains(err.Error(), "not found") {
				return errors.Wrap(err, "get pgCluster")
			}
			if pgCluster != nil {
				pgClusterStatus = pgCluster.Status
			}

			selector := config.LABEL_PG_CLUSTER + "=" + p.Name
			pgReplicas, err := c.Client.CrunchydataV1().Pgreplicas(n.Name).List(ctx, metav1.ListOptions{LabelSelector: selector})
			if err != nil && !strings.Contains(err.Error(), "not found") {
				return errors.Wrap(err, "get pgReplicas list")
			}
			if pgReplicas != nil {
				for _, repl := range pgReplicas.Items {
					replStatuses[repl.Name] = repl.Status
				}
			}

			if reflect.DeepEqual(p.Status.PGCluster, pgCluster.Status) && reflect.DeepEqual(p.Status.PGReplicas, replStatuses) {
				return nil
			}

			value := crv1.PerconaPGClusterStatus{
				PGCluster:  pgClusterStatus,
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
	oldCluster := oldObj.(*crv1.PerconaPGCluster)
	newCluster := newObj.(*crv1.PerconaPGCluster)

	if reflect.DeepEqual(oldCluster.Spec, newCluster.Spec) {
		return
	}

	err := c.updateTemplate(newCluster)
	if err != nil {
		log.Errorf("update perconapgcluster: update deployment template: %s", err)
	}

	if !reflect.DeepEqual(oldCluster.Spec.PGPrimary.Expose, newCluster.Spec.PGPrimary.Expose) {
		err = service.CreateOrUpdate(c.Client, newCluster, service.PGPrimaryServiceType)
		if err != nil {
			log.Errorf("update perconapgcluster: handle primary service on update: %s", err)
			return
		}
	}
	if !reflect.DeepEqual(oldCluster.Spec.PGBouncer.Expose, newCluster.Spec.PGBouncer.Expose) {
		if newCluster.Spec.PGBouncer.Size > 0 {
			err = service.CreateOrUpdate(c.Client, newCluster, service.PGBouncerServiceType)
			if err != nil {
				log.Errorf("update perconapgcluster: handle bouncer service on update: %s", err)
				return
			}
		}
	}

	primary, err := pgcluster.IsPrimary(c.Client, oldCluster)
	if err != nil {
		log.Errorf("update perconapgcluster: check is pgcluster primary: %s", err)
	}

	if primary {
		err = pgreplica.Update(c.Client, newCluster, oldCluster)
		if err != nil {
			log.Errorf("update perconapgcluster: update pgreplica: %s", err)
		}
		err = pgcluster.Update(c.Client, newCluster, oldCluster)
		if err != nil {
			log.Errorf("update perconapgcluster: update pgcluster: %s", err)
			return
		}
		return
	}
	err = pgcluster.Update(c.Client, newCluster, oldCluster)
	if err != nil {
		log.Errorf("update perconapgcluster: update pgcluster: %s", err)
		return
	}
	err = pgreplica.Update(c.Client, newCluster, oldCluster)
	if err != nil {
		log.Errorf("update perconapgcluster: update pgreplica: %s", err)
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
		log.Errorf("delete pgcluster: %s", err)
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

func handleSecurityContextTemplate(template []byte, cluster *crv1.PerconaPGCluster) ([]byte, error) {
	if cluster.Spec.SecurityContext == nil {
		if operator.Pgo.DisableFSGroup() {
			return bytes.Replace(template, []byte("<securityContext>"), []byte(`{"supplementalGroups": [1001]}`), -1), nil
		}
		return bytes.Replace(template, []byte("<securityContext>"), []byte(defaultSecurityContext), -1), nil
	}
	securityContextBytes, err := getSecurityContextJSON(cluster)
	if err != nil {
		return nil, errors.Wrap(err, "get security context json: %s")
	}

	return bytes.Replace(template, []byte("<securityContext>"), securityContextBytes, -1), nil
}

func getSecurityContextJSON(cluster *crv1.PerconaPGCluster) ([]byte, error) {
	if operator.Pgo.DisableFSGroup() && cluster.Spec.SecurityContext != nil {
		cluster.Spec.SecurityContext.FSGroup = nil
	}
	return json.Marshal(cluster.Spec.SecurityContext)
}
