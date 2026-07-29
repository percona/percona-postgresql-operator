package util

import (
	"fmt"
	"hash/fnv"
	"io"

	"k8s.io/apimachinery/pkg/util/rand"
)

// safeHash32 runs content and returns a short alphanumeric string that
// represents everything written to w. The string is unlikely to have bad words
// and is safe to store in the Kubernetes API. This is the same algorithm used
// by ControllerRevision's "controller.kubernetes.io/hash".
func SafeHash32(content func(w io.Writer) error) (string, error) {
	hash := fnv.New32()
	if err := content(hash); err != nil {
		return "", err
	}
	return rand.SafeEncodeString(fmt.Sprint(hash.Sum32())), nil
}
