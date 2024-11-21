/*
 Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

 http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

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

	PostgreSQL(cluster, parameters)
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

	PostgreSQL(cluster, parameters)
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
		"restore_command":        "/bin/true",
		"track_commit_timestamp": "true",
	})

	cluster.Spec.Standby = &v1beta1.PostgresStandbySpec{
		Enabled:  true,
		RepoName: "repo99",
	}
	cluster.Spec.Patroni.DynamicConfiguration = nil

	PostgreSQL(cluster, parameters)
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
