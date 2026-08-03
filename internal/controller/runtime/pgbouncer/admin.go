package pgbouncer

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
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
	Pod      *corev1.Pod
}

func (o *AdminClientOptions) validate() error {
	if o.User == "" {
		return errors.New("user is required")
	}
	if o.Password == "" {
		return errors.New("password is required")
	}
	if o.Pod == nil && o.Host == "" {
		return errors.New("either pod or host is required")
	}
	return nil
}

func (o *AdminClientOptions) host() string {
	if o.Host != "" {
		return o.Host
	}
	return fmt.Sprintf("%s.%s", o.Pod.Name, o.Pod.Namespace)
}

func NewAdminClient(opts AdminClientOptions) (AdminClient, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=pgbouncer sslmode=require",
		opts.host(), opts.User, opts.Password)
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
// Not idempotent, must be handled by caller.
func (c *adminClient) Pause(ctx context.Context) error {
	_, err := c.db.ExecContext(ctx, "PAUSE")
	if err != nil {
		return errors.Wrap(err, "pause pgbouncer")
	}
	return nil
}

// Resume pgbouncer connections.
// Not idempotent, must be handled by caller.
func (c *adminClient) Resume(ctx context.Context) error {
	_, err := c.db.ExecContext(ctx, "RESUME")
	if err != nil {
		return errors.Wrap(err, "resume pgbouncer")
	}
	return nil
}

func (c *adminClient) Close() error {
	return c.db.Close()
}
