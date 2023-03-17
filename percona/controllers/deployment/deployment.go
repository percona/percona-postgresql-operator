package deployment

import (
	"context"
	"encoding/json"
	"time"

	"github.com/percona/percona-postgresql-operator/internal/config"
	"github.com/percona/percona-postgresql-operator/internal/kubeapi"
	"github.com/percona/percona-postgresql-operator/internal/operator"
	util "github.com/percona/percona-postgresql-operator/internal/util"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"
	"github.com/pkg/errors"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ContainerDatabase  = "database"
	ContainerPGBadger  = "pgbadger"
	ContainerPGBouncer = "pgbouncer"
)

func UpdateSpecTemplateSpecSecurityContext(cl *crv1.PerconaPGCluster, deployment *appsv1.Deployment) {
	if cl.Spec.SecurityContext == nil {
		return
	}
	if operator.Pgo.DisableFSGroup() {
		cl.Spec.SecurityContext.FSGroup = nil
	}
	deployment.Spec.Template.Spec.SecurityContext = cl.Spec.SecurityContext
}

func UpdateSpecTemplateAnnotations(annotations map[string]string, deployment *appsv1.Deployment) {
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	for k, v := range annotations {
		deployment.Spec.Template.Annotations[k] = v
	}

	return
}

func UpdateSpecTemplateLabels(labels map[string]string, deployment *appsv1.Deployment) {
	if deployment.Spec.Template.Labels == nil {
		deployment.Spec.Template.Labels = make(map[string]string)
	}
	for k, v := range labels {
		deployment.Spec.Template.Labels[k] = v
	}
}

func UpdateSpecTemplateAffinity(deployment *appsv1.Deployment, deploymentType crv1.PodAntiAffinityDeployment, affinity crv1.Affinity, cluster *crv1.Pgcluster) {
	if affinity.Advanced != nil {
		deployment.Spec.Template.Spec.Affinity = affinity.Advanced
		return
	}

	if affinity.NodeLabel != nil {
		var nodeAffinityType crv1.NodeAffinityType
		switch affinity.NodeAffinityType {
		case "preferred":
			nodeAffinityType = crv1.NodeAffinityTypePreferred
		case "required":
			nodeAffinityType = crv1.NodeAffinityTypeRequired
		}
		for key, val := range affinity.NodeLabel {
			deployment.Spec.Template.Spec.Affinity.NodeAffinity = util.GenerateNodeAffinity(nodeAffinityType, key, []string{val})
		}
	}

	var podAntiAffinity struct {
		PodAntiAffinity *v1.PodAntiAffinity `json:"podAntiAffinity"`
	}

	antiAffinityJSON := operator.GetPodAntiAffinity(cluster, deploymentType, affinity.AntiAffinityType)
	err := json.Unmarshal([]byte(antiAffinityJSON), &podAntiAffinity)
	if err != nil {
		return
	}

	deployment.Spec.Template.Spec.Affinity.PodAntiAffinity = podAntiAffinity.PodAntiAffinity
}

func UpdateDeploymentContainer(deployment *appsv1.Deployment, containerName, image, pullPolicy string) {
	containers := []v1.Container{}
	for _, c := range deployment.Spec.Template.Spec.Containers {
		if c.Name == containerName {
			c.Image = image
			c.ImagePullPolicy = v1.PullPolicy(pullPolicy)
		}
		containers = append(containers, c)
	}

	deployment.Spec.Template.Spec.Containers = containers
}

func UpdateDeploymentVersionLabels(deployment *appsv1.Deployment, cluster *crv1.PerconaPGCluster) {
	if deployment.Labels == nil {
		deployment.Labels = make(map[string]string)
	}
	if deployment.Spec.Template.Labels == nil {
		deployment.Spec.Template.Labels = make(map[string]string)
	}
	if cluster.Labels != nil {
		deployment.Labels[config.LABEL_PGO_VERSION] = cluster.Labels[config.LABEL_PGO_VERSION]
		deployment.Spec.Template.Labels[config.LABEL_PGO_VERSION] = cluster.Labels[config.LABEL_PGO_VERSION]
	}
}

func Wait(client kubeapi.Interface, deploymentName, namespace string) error {
	ctx := context.TODO()
	for i := 0; i <= 30; i++ {
		time.Sleep(5 * time.Second)
		primaryDepl, err := client.AppsV1().Deployments(namespace).Get(ctx,
			deploymentName, metav1.GetOptions{})
		if err != nil && !kerrors.IsNotFound(err) {
			return errors.Wrap(err, "get deployment")
		}
		if primaryDepl.Status.Replicas == primaryDepl.Status.AvailableReplicas {
			break
		}
	}

	return nil
}
