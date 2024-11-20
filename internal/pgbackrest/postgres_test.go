// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package pgbackrest

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/percona/percona-postgresql-operator/internal/postgres"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func TestPostgreSQLParameters(t *testing.T) {
	cluster := new(v1beta1.PostgresCluster)
	parameters := new(postgres.Parameters)

	PostgreSQL(cluster, parameters, true)
	assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
		"archive_mode": "on",
		"archive_command": strings.Join([]string{
			`pgbackrest --stanza=db archive-push "%p"`,
			` && timestamp=$(pg_waldump "%p" | grep COMMIT | awk '{print $(NF`,
			`-2) "T" $(NF-1) " " $(NF)}' | sed -E 's/([0-9]{4}-[0-9]{2}-[0-9]`,
			`{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}) (UTC|[\+\-][0-9]{2})/\`,
			`1\2/' | sed 's/UTC/Z/' | tail -n 1 | grep -E '^[0-9]{4}-[0-9]{2}`,
			`-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}(Z|[\+\-][0-9]{2})`,
			"$'); if [ ! -z ${timestamp} ]; then echo ${timestamp} > /pgdata/l",
			"atest_commit_timestamp.txt; fi",
		}, ""),
		"restore_command":        `pgbackrest --stanza=db archive-get %f "%p"`,
		"track_commit_timestamp": "true",
	})

	assert.DeepEqual(t, parameters.Default.AsMap(), map[string]string{
		"archive_timeout": "60s",
	})

	cluster.Spec.Standby = &v1beta1.PostgresStandbySpec{
		Enabled:  true,
		RepoName: "repo99",
	}

	PostgreSQL(cluster, parameters, true)
	assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
		"archive_mode": "on",
		"archive_command": strings.Join([]string{
			`pgbackrest --stanza=db archive-push "%p"`,
			` && timestamp=$(pg_waldump "%p" | grep COMMIT | awk '{print $(NF`,
			`-2) "T" $(NF-1) " " $(NF)}' | sed -E 's/([0-9]{4}-[0-9]{2}-[0-9]`,
			`{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}) (UTC|[\+\-][0-9]{2})/\`,
			`1\2/' | sed 's/UTC/Z/' | tail -n 1 | grep -E '^[0-9]{4}-[0-9]{2}`,
			`-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}(Z|[\+\-][0-9]{2})`,
			"$'); if [ ! -z ${timestamp} ]; then echo ${timestamp} > /pgdata/l",
			"atest_commit_timestamp.txt; fi",
		}, ""),
		"restore_command":        `pgbackrest --stanza=db archive-get %f "%p" --repo=99`,
		"track_commit_timestamp": "true",
	})
}
