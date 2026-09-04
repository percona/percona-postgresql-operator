package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/mock"
	"gotest.tools/v3/assert"

	pgbmock "github.com/percona/percona-postgresql-operator/v3/internal/controller/runtime/pgbouncer/mock"
	"github.com/percona/percona-postgresql-operator/v3/internal/pgbouncer/startup"
)

func TestPauseWanted(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "pgbouncer-paused")
		assert.NilError(t, os.WriteFile(path, []byte(content), 0o600))
		return path
	}

	t.Run("Missing", func(t *testing.T) {
		// The marker is projected optionally, so absence is not an error.
		wanted, err := pauseWanted(filepath.Join(t.TempDir(), "does-not-exist"))
		assert.NilError(t, err)
		assert.Assert(t, !wanted)
	})

	t.Run("Paused", func(t *testing.T) {
		wanted, err := pauseWanted(write(t, startup.PausedValue))
		assert.NilError(t, err)
		assert.Assert(t, wanted)
	})

	t.Run("TrailingNewline", func(t *testing.T) {
		wanted, err := pauseWanted(write(t, startup.PausedValue+"\n"))
		assert.NilError(t, err)
		assert.Assert(t, wanted)
	})

	t.Run("Empty", func(t *testing.T) {
		wanted, err := pauseWanted(write(t, ""))
		assert.NilError(t, err)
		assert.Assert(t, !wanted)
	})

	t.Run("OtherContent", func(t *testing.T) {
		wanted, err := pauseWanted(write(t, "0"))
		assert.NilError(t, err)
		assert.Assert(t, !wanted)
	})

	t.Run("Unreadable", func(t *testing.T) {
		// A directory is readable by stat but not by ReadFile.
		_, err := pauseWanted(t.TempDir())
		assert.Assert(t, err != nil)
	})
}

func TestPause(t *testing.T) {
	t.Parallel()

	t.Run("Succeeds", func(t *testing.T) {
		client := pgbmock.NewAdminClient(t)
		client.On("Pause", mock.Anything).Return(nil).Once()

		assert.NilError(t, pause(context.Background(), client, time.Millisecond))
	})

	t.Run("RetriesUntilReady", func(t *testing.T) {
		// PgBouncer refuses connections until it finishes starting up.
		client := pgbmock.NewAdminClient(t)
		client.On("Pause", mock.Anything).
			Return(errors.New("connection refused")).Twice()
		client.On("Pause", mock.Anything).Return(nil).Once()

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		assert.NilError(t, pause(ctx, client, time.Millisecond))
	})

	t.Run("GivesUpWhenContextDone", func(t *testing.T) {
		client := pgbmock.NewAdminClient(t)
		client.On("Pause", mock.Anything).Return(errors.New("connection refused"))

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		err := pause(ctx, client, time.Millisecond)
		assert.ErrorContains(t, err, "connection refused")
	})
}
