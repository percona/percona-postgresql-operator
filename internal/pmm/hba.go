package pmm

import (
	"github.com/percona/percona-postgresql-operator/v2/internal/postgres"
	"github.com/percona/percona-postgresql-operator/v2/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

const (
	// MonitoringUser is a Postgres user created by PMM configuration
	MonitoringUser = "monitor"
)

// PostgreSQLHBAs provides the Postgres HBA rules for allowing the monitoring
// exporter to be accessible
func PostgreSQLHBAs(inCluster *v1beta1.PostgresCluster, outHBAs *postgres.HBAs) {
	// Limit the monitoring user to local connections using SCRAM.
	outHBAs.Mandatory = append(outHBAs.Mandatory,
		postgres.NewHBA().TCP().User(MonitoringUser).Method("scram-sha-256").Network("127.0.0.0/8"),
		postgres.NewHBA().TCP().User(MonitoringUser).Method("scram-sha-256").Network("::1/128"),
		postgres.NewHBA().TCP().User(MonitoringUser).Method("reject"))
}
