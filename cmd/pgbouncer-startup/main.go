package main

import (
	"context"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"

	pgbruntime "github.com/percona/percona-postgresql-operator/v3/internal/controller/runtime/pgbouncer"
	"github.com/percona/percona-postgresql-operator/v3/internal/pgbouncer/startup"
)

const (
	pauseTimeout       = startup.PauseTimeoutSeconds * time.Second
	pauseRetryInterval = time.Second
	adminHost          = "localhost"
)

func main() {
	f, err := os.OpenFile(startup.LogAbsolutePath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()

	log.SetOutput(io.MultiWriter(os.Stderr, f))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := handlePause(ctx); err != nil {
		log.Fatalf("ERROR: %v", err)
	}
}

// If connections need to be paused, pause them on startup.
// During runtime, the operator runs PAUSE, but this is not persisted across restarts.
// Failure to pause is fatal: the container must not be considered started while
// it is accepting connections the user asked us to hold.
func handlePause(ctx context.Context) error {
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

	client, err := pgbruntime.NewAdminClient(pgbruntime.AdminClientOptions{
		Host:     adminHost,
		User:     startup.AdminUser,
		Password: password,
		Port:     os.Getenv(startup.PortEnvVar),
	})
	if err != nil {
		return errors.Wrap(err, "create pgbouncer admin client")
	}
	defer func() { _ = client.Close() }()

	pctx, cancel := context.WithTimeout(ctx, pauseTimeout)
	defer cancel()

	if err := pause(pctx, client, pauseRetryInterval); err != nil {
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
