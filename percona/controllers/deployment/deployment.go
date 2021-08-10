package deployment

import (
	"github.com/percona/percona-postgresql-operator/internal/operator"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"

	appsv1 "k8s.io/api/apps/v1"
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

	return
}
