// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package initialize_test

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/percona/percona-postgresql-operator/v2/internal/initialize"
)

func TestFromPointer(t *testing.T) {
	t.Run("bool", func(t *testing.T) {
		assert.Equal(t, initialize.FromPointer((*bool)(nil)), false)
		assert.Equal(t, initialize.FromPointer(new(false)), false)
		assert.Equal(t, initialize.FromPointer(new(true)), true)
	})

	t.Run("int32", func(t *testing.T) {
		assert.Equal(t, initialize.FromPointer((*int32)(nil)), int32(0))
		assert.Equal(t, initialize.FromPointer(new(int32(0))), int32(0))
		assert.Equal(t, initialize.FromPointer(new(int32(-99))), int32(-99))
		assert.Equal(t, initialize.FromPointer(new(int32(42))), int32(42))
	})

	t.Run("int64", func(t *testing.T) {
		assert.Equal(t, initialize.FromPointer((*int64)(nil)), int64(0))
		assert.Equal(t, initialize.FromPointer(new(int64(0))), int64(0))
		assert.Equal(t, initialize.FromPointer(new(int64(-99))), int64(-99))
		assert.Equal(t, initialize.FromPointer(new(int64(42))), int64(42))
	})

	t.Run("string", func(t *testing.T) {
		assert.Equal(t, initialize.FromPointer((*string)(nil)), "")
		assert.Equal(t, initialize.FromPointer(new("")), "")
		assert.Equal(t, initialize.FromPointer(new("sup")), "sup")
	})
}

func TestMap(t *testing.T) {
	t.Run("map[string][]byte", func(t *testing.T) {
		// Ignores nil pointer.
		initialize.Map((*map[string][]byte)(nil))

		var m map[string][]byte

		// Starts nil.
		assert.Assert(t, m == nil)

		// Gets initialized.
		initialize.Map(&m)
		assert.DeepEqual(t, m, map[string][]byte{})

		// Now writable.
		m["x"] = []byte("y")

		// Doesn't overwrite.
		initialize.Map(&m)
		assert.DeepEqual(t, m, map[string][]byte{"x": []byte("y")})
	})

	t.Run("map[string]string", func(t *testing.T) {
		// Ignores nil pointer.
		initialize.Map((*map[string]string)(nil))

		var m map[string]string

		// Starts nil.
		assert.Assert(t, m == nil)

		// Gets initialized.
		initialize.Map(&m)
		assert.DeepEqual(t, m, map[string]string{})

		// Now writable.
		m["x"] = "y"

		// Doesn't overwrite.
		initialize.Map(&m)
		assert.DeepEqual(t, m, map[string]string{"x": "y"})
	})
}
