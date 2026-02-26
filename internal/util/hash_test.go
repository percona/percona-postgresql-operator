package util

import (
	"io"
	"testing"

	"github.com/pkg/errors"
	"gotest.tools/v3/assert"
)

func TestSafeHash32(t *testing.T) {
	expected := errors.New("whomp")

	_, err := SafeHash32(func(io.Writer) error { return expected })
	assert.Equal(t, err, expected)

	stuff, err := SafeHash32(func(w io.Writer) error {
		_, _ = w.Write([]byte(`some stuff`))
		return nil
	})
	assert.NilError(t, err)
	assert.Equal(t, stuff, "574b4c7d87", "expected alphanumeric")

	same, err := SafeHash32(func(w io.Writer) error {
		_, _ = w.Write([]byte(`some stuff`))
		return nil
	})
	assert.NilError(t, err)
	assert.Equal(t, same, stuff, "expected deterministic hash")
}
