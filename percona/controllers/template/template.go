package template

import (
	"bytes"
	"encoding/json"
	"text/template"

	"github.com/percona/percona-postgresql-operator/internal/config"
	"github.com/percona/percona-postgresql-operator/internal/operator"
	"github.com/percona/percona-postgresql-operator/percona/controllers/pmm"
	crv1 "github.com/percona/percona-postgresql-operator/pkg/apis/crunchydata.com/v1"

	"github.com/pkg/errors"
)

const (
	ClusterDeploymentTemplateName      = "cluster-deployment.json"
	BackrestRepoDeploymentTemplateName = "pgo-backrest-repo-template.json"
	BackrestJobTemplateName            = "backrest-job.json"
	BouncerDeploymentTemplateName      = "pgbouncer-template.json"
	PGBadgerTemplateName               = "pgbadger.json"
	Path                               = "/"
	defaultSecurityContext             = `{"fsGroup": 26,"supplementalGroups": [1001]}`
)

func UpdateBackrestJobTemplate(backrestJobTemplateData []byte, newCluster *crv1.PerconaPGCluster) error {
	templateData := handleImagePullPolicy(backrestJobTemplateData, []byte(newCluster.Spec.Backup.ImagePullPolicy))

	t, err := template.New(BackrestJobTemplateName).Parse(string(templateData))
	if err != nil {
		return errors.Wrap(err, "parse template")
	}

	config.BackrestjobTemplate = t

	return nil
}

func UpdateDeploymentTemplate(deploymentTemplateData []byte, newCluster *crv1.PerconaPGCluster, nodeName string) error {
	templateData, err := handlePMMTemplate(deploymentTemplateData, newCluster, nodeName)
	if err != nil {
		return errors.Wrap(err, "handle pmm template data")
	}
	templateData, err = handleSecurityContextTemplate(templateData, newCluster)
	if err != nil {
		return errors.Wrap(err, "handle security context template data")
	}
	templateData = handleImagePullPolicy(templateData, []byte(newCluster.Spec.PGPrimary.ImagePullPolicy))

	t, err := template.New(ClusterDeploymentTemplateName).Parse(string(templateData))
	if err != nil {
		return errors.Wrap(err, "parse template")
	}

	config.DeploymentTemplate = t

	return nil
}

func UpdateBackrestRepoTemplate(backrestRepoDeploymentTemplateData []byte, newCluster *crv1.PerconaPGCluster, nodeName string) error {
	templateData := handleImagePullPolicy(backrestRepoDeploymentTemplateData, []byte(newCluster.Spec.Backup.ImagePullPolicy))

	t, err := template.New(BackrestRepoDeploymentTemplateName).Parse(string(templateData))
	if err != nil {
		return errors.Wrap(err, "parse template")
	}

	config.PgoBackrestRepoTemplate = t

	return nil
}

func UpdateBouncerTemplate(bouncerDeploymentTemplateData []byte, newCluster *crv1.PerconaPGCluster, nodeName string) error {
	templateData := handleImagePullPolicy(bouncerDeploymentTemplateData, []byte(newCluster.Spec.PGBouncer.ImagePullPolicy))

	t, err := template.New(BouncerDeploymentTemplateName).Parse(string(templateData))
	if err != nil {
		return errors.Wrap(err, "parse template")
	}

	config.PgbouncerTemplate = t

	return nil
}

func UpdatePGBadgerTemplate(pgBadgerTemplateData []byte, newCluster *crv1.PerconaPGCluster, nodeName string) error {
	templateData := handleImagePullPolicy(pgBadgerTemplateData, []byte(newCluster.Spec.PGBadger.ImagePullPolicy))

	t, err := template.New(PGBadgerTemplateName).Parse(string(templateData))
	if err != nil {
		return errors.Wrap(err, "parse template")
	}

	config.BadgerTemplate = t

	return nil
}

func handlePMMTemplate(template []byte, cluster *crv1.PerconaPGCluster, nodeName string) ([]byte, error) {
	if !cluster.Spec.PMM.Enabled {
		return bytes.Replace(template, []byte("<pmmContainer>"), []byte(""), -1), nil
	}
	pmmContainerBytes, err := GetPMMContainerJSON(cluster, nodeName)
	if err != nil {
		return nil, errors.Wrap(err, "get pmm container json: %s")
	}

	return bytes.Replace(template, []byte("<pmmContainer>"), append([]byte(", "), pmmContainerBytes...), -1), nil
}
func GetPMMContainerJSON(pgc *crv1.PerconaPGCluster, nodeName string) ([]byte, error) {
	c := pmm.GetPMMContainer(pgc, pgc.Name, "{{.Name}}")
	b, err := json.Marshal(c)
	if err != nil {
		return nil, errors.Wrap(err, "marshal container")
	}

	return b, nil
}

func handleImagePullPolicy(template, imagePullPolicy []byte) []byte {
	return bytes.Replace(template, []byte("<imagePullPolicy>"), imagePullPolicy, -1)
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
