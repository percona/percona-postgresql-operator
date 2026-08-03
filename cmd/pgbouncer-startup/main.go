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
	"github.com/percona/percona-postgresql-operator/v2/internal/pgbouncer"
)

const (
	pauseTimeout       = 30 * time.Second
	pauseRetryInterval = time.Second
	adminHost          = "localhost"
)

func main() {
	f, err := os.OpenFile(pgbouncer.StartupLogAbsolutePath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()

	log.SetOutput(io.MultiWriter(os.Stderr, f))

	handlePause()
}

// If connections need to be paused, pause them on startup.
// During runtime, the operator runs PAUSE, but this is not persisted across restarts.
// This is best-effort, failure to pause should not block startup.
func handlePause() {
	wanted, err := pauseWanted(pgbouncer.PausedFileAbsolutePath)
	if err != nil {
		log.Printf("ERROR: read paused marker: %v", err)
		return
	}
	if !wanted {
		return
	}

	password := os.Getenv(pgbouncer.AdminPasswordEnvVar)
	if password == "" {
		log.Printf("ERROR: %s is not set, cannot pause", pgbouncer.AdminPasswordEnvVar)
		return
	}

	client, err := pgbruntime.NewAdminClient(pgbouncer.AdminUser, password, adminHost)
	if err != nil {
		log.Printf("ERROR: create pgbouncer admin client: %v", err)
		return
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), pauseTimeout)
	defer cancel()

	if err := pause(ctx, client, pauseRetryInterval); err != nil {
		log.Printf("ERROR: pause pgbouncer: %v", err)
		return
	}

	log.Print("paused pgbouncer connections")
}

func pauseWanted(path string) (bool, error) {
	content, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return false, nil
	case err != nil:
		return false, errors.WithStack(err)
	}

	return strings.TrimSpace(string(content)) == pgbouncer.PausedValue, nil
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
