// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package pgbackrest

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"k8s.io/utils/ptr"

	"github.com/percona/percona-postgresql-operator/internal/postgres"
	"github.com/percona/percona-postgresql-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func TestPostgreSQLParameters(t *testing.T) {
	cluster := new(v1beta1.PostgresCluster)
	parameters := new(postgres.Parameters)

	PostgreSQL(cluster, parameters, true)
	assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
		"archive_mode":    "on",
		"archive_command": `pgbackrest --stanza=db archive-push "%p"`,
		"restore_command": `pgbackrest --stanza=db archive-get %f "%p"`,
	})

	assert.DeepEqual(t, parameters.Default.AsMap(), map[string]string{
		"archive_timeout": "60s",
	})

	dynamic := map[string]any{
		"postgresql": map[string]any{
			"parameters": map[string]any{
				"restore_command": "/bin/true",
			},
		},
	}
	if cluster.Spec.Patroni == nil {
		cluster.Spec.Patroni = &v1beta1.PatroniSpec{}
	}
	cluster.Spec.Patroni.DynamicConfiguration = dynamic

	PostgreSQL(cluster, parameters, true)
	assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
		"archive_mode":    "on",
		"archive_command": `pgbackrest --stanza=db archive-push "%p"`,
		"restore_command": "/bin/true",
	})

	cluster.Spec.Standby = &v1beta1.PostgresStandbySpec{
		Enabled:  true,
		RepoName: "repo99",
	}
	cluster.Spec.Patroni.DynamicConfiguration = nil

	PostgreSQL(cluster, parameters, true)
	assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
		"archive_mode":    "on",
		"archive_command": `pgbackrest --stanza=db archive-push "%p"`,
		"restore_command": `pgbackrest --stanza=db archive-get %f "%p" --repo=99`,
	})

	cluster.Spec.Standby = nil
	cluster.Spec.Patroni.DynamicConfiguration = nil
	cluster.Spec.Backups.TrackLatestRestorableTime = ptr.To(true)

	PostgreSQL(cluster, parameters, true)
	assert.DeepEqual(t, parameters.Mandatory.AsMap(), map[string]string{
		"archive_mode": "on",
		"archive_command": strings.Join([]string{
			`pgbackrest --stanza=db archive-push "%p" `,
			`&& timestamp=$(pg_waldump "%p" | `,
			`grep -oP "COMMIT \K[^;]+" | `,
			`sed -E "s/([0-9]{4}-[0-9]{2}-[0-9]{2}) ([0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}) (UTC|[\\+\\-][0-9]{2})/\1T\2\3/" | `,
			`sed "s/UTC/Z/" | `,
			"tail -n 1 | ",
			`grep -E "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}(Z|[\+\-][0-9]{2})$"); `,
			"if [ ! -z ${timestamp} ]; then echo ${timestamp} > /pgdata/latest_commit_timestamp.txt; fi",
		}, ""),
		"restore_command":        `pgbackrest --stanza=db archive-get %f "%p"`,
		"track_commit_timestamp": "true",
	})
}
