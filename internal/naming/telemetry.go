// Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package naming

import "go.opentelemetry.io/otel"

var tracer = otel.Tracer("github.com/percona/percona-postgresql-operator/v2/naming")
