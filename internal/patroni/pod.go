package patroni

import (
	"encoding/json"

	gover "github.com/hashicorp/go-version"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
)

func GetVersionFromPod(pod *corev1.Pod) (*gover.Version, error) {
	patroniJson, ok := pod.Annotations["status"]
	if !ok {
		return nil, errors.New("pod doesn't have status annotation")
	}
	patroniStatus := make(map[string]any)
	if err := json.Unmarshal([]byte(patroniJson), patroniStatus); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal patroni status")
	}
	versionI, ok := patroniStatus["version"]
	if !ok {
		return nil, errors.New("status doesn't have version field")
	}

	versionStr, ok := versionI.(string)
	if !ok {
		return nil, errors.New("version is not string")
	}

	version, err := gover.NewVersion(versionStr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create new version from string")
	}

	return version, nil
}
