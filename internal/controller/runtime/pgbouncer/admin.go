package pgbouncer

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/lib/pq"
	"github.com/pkg/errors"
)

type AdminClient interface {
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
	Close() error
}

type AdminClientOptions struct {
	User     string
	Password string
	Host     string
}

func NewAdminClient(user, password, host string) (AdminClient, error) {
	if user == "" {
		return nil, errors.New("user is required")
	}
	if password == "" {
		return nil, errors.New("password is required")
	}
	if host == "" {
		return nil, errors.New("host is required")
	}

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=pgbouncer sslmode=require",
		host, user, password)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, errors.Wrap(err, "open pgbouncer connection")
	}
	return &adminClient{db: db}, nil
}

type adminClient struct {
	db *sql.DB
}

// Pause pgbouncer connections.
func (c *adminClient) Pause(ctx context.Context) error {
	_, err := c.db.ExecContext(ctx, "PAUSE")
	if err != nil {
		if strings.Contains(err.Error(), "already suspended/paused") {
			return nil
		}
		return errors.Wrap(err, "pause pgbouncer")
	}
	return nil
}

// Resume pgbouncer connections.
func (c *adminClient) Resume(ctx context.Context) error {
	_, err := c.db.ExecContext(ctx, "RESUME")
	if err != nil {
		if strings.Contains(err.Error(), "pooler is not paused/suspended") {
			return nil
		}
		return errors.Wrap(err, "resume pgbouncer")
	}
	return nil
}

func (c *adminClient) Close() error {
	return c.db.Close()
}
