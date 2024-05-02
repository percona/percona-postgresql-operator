package pgbackrest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"

	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/percona/clientcmd"
)

const (
	AnnotationBackupName = "percona.com/backup-name"
	AnnotationJobName    = "percona.com/backup-job-name"
	AnnotationJobType    = "percona.com/backup-job-type"
)

type InfoOutput []InfoStanza

type InfoBackup struct {
	Annotation map[string]string `json:"annotation,omitempty"`
	Label      string            `json:"label,omitempty"`
}

type InfoStanza struct {
	Name   string       `json:"name,omitempty"`
	Backup []InfoBackup `json:"backup,omitempty"`
	Status struct {
		Message string  `json:"message,omitempty"`
		Code    float64 `json:"code,omitempty"`
		Lock    struct {
			Backup struct {
				Held bool `json:"held,omitempty"`
			} `json:"backup,omitempty"`
		} `json:"lock,omitempty"`
	} `json:"status,omitempty"`
}

func GetInfo(ctx context.Context, pod *corev1.Pod, repoName string) (InfoOutput, error) {
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)

	c, err := clientcmd.NewClient()
	if err != nil {
		return InfoOutput{}, errors.Wrap(err, "failed to create client")
	}

	if err := c.Exec(ctx, pod, naming.ContainerDatabase, nil, stdout, stderr, "pgbackrest", "info", "--output=json", "--repo="+strings.TrimPrefix(repoName, "repo")); err != nil {
		return InfoOutput{}, errors.Wrapf(err, "exec: %s", stderr.String())
	}

	out := InfoOutput{}

	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return InfoOutput{}, errors.Wrap(err, "failed to unmarshal pgBackRest info output")
	}

	for _, elem := range out {
		if elem.Status.Code != 0 {
			return InfoOutput{}, errors.Errorf("pgBackRest info command failed with code %d: %s", int(elem.Status.Code), elem.Status.Message)
		}
	}

	return out, nil
}

func SetAnnotationsToBackup(ctx context.Context, pod *corev1.Pod, stanza string, backupSet string, repoName string, annotations map[string]string) error {
	stderr := new(bytes.Buffer)

	c, err := clientcmd.NewClient()
	if err != nil {
		return errors.Wrap(err, "failed to create client")
	}

	annotationsOpts := []string{}
	for k, v := range annotations {
		annotationsOpts = append(annotationsOpts, fmt.Sprintf(`--annotation=%s=%s`, k, v))
	}

	cmd := []string{"pgbackrest", fmt.Sprintf(`--stanza=%s`, stanza), "--set=" + backupSet, "--repo=" + strings.TrimPrefix(repoName, "repo")}
	cmd = append(cmd, annotationsOpts...)
	cmd = append(cmd, "annotate")

	if err := c.Exec(ctx, pod, naming.ContainerDatabase, nil, nil, stderr, cmd...); err != nil {
		return errors.Wrapf(err, "exec: %s", stderr.String())
	}

	return nil
}
