package main

import (
	"context"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"

	pgbruntime "github.com/percona/percona-postgresql-operator/v2/internal/controller/runtime/pgbouncer"
	"github.com/percona/percona-postgresql-operator/v2/internal/pgbouncer/startup"
)

const (
	pauseTimeout       = 30 * time.Second
	pauseRetryInterval = time.Second
	adminHost          = "localhost"
)

func main() {
	f, err := os.OpenFile(startup.LogAbsolutePath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()

	log.SetOutput(io.MultiWriter(os.Stderr, f))

	if err := handlePause(); err != nil {
		log.Fatalf("ERROR: %v", err)
	}
}

// If connections need to be paused, pause them on startup.
// During runtime, the operator runs PAUSE, but this is not persisted across restarts.
// Failure to pause is fatal: the container must not be considered started while
// it is accepting connections the user asked us to hold.
func handlePause() error {
	wanted, err := pauseWanted(startup.PausedFileAbsolutePath)
	if err != nil {
		return errors.Wrap(err, "read paused marker")
	}
	if !wanted {
		return nil
	}

	password := os.Getenv(startup.AdminPasswordEnvVar)
	if password == "" {
		return errors.Errorf("%s is not set, cannot pause", startup.AdminPasswordEnvVar)
	}

	client, err := pgbruntime.NewAdminClient(startup.AdminUser, password, adminHost)
	if err != nil {
		return errors.Wrap(err, "create pgbouncer admin client")
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), pauseTimeout)
	defer cancel()

	if err := pause(ctx, client, pauseRetryInterval); err != nil {
		return errors.Wrap(err, "pause pgbouncer")
	}

	log.Print("paused pgbouncer connections")

	return nil
}

func pauseWanted(path string) (bool, error) {
	content, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return false, nil
	case err != nil:
		return false, errors.WithStack(err)
	}

	return strings.TrimSpace(string(content)) == startup.PausedValue, nil
}

func pause(ctx context.Context, client pgbruntime.AdminClient, retryInterval time.Duration) error {
	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()

	for {
		err := client.Pause(ctx)
		if err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return errors.Wrapf(err, "gave up after %s, last error", pauseTimeout)
		case <-ticker.C:
		}
	}
}
