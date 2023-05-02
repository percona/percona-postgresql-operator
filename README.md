# Percona Operator for PostgreSQL

![Percona Kubernetes Operators](kubernetes.png)

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
![Docker Pulls](https://img.shields.io/docker/pulls/percona/percona-postgresql-operator)
![Docker Image Size (tag)](https://img.shields.io/docker/image-size/percona/percona-postgresql-operator/2)
![GitHub tag (latest by SemVer)](https://img.shields.io/github/v/tag/percona/percona-postgresql-operator?include_prereleases&sort=semver)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/percona/percona-postgresql-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/percona/percona-postgresql-operator)](https://goreportcard.com/report/github.com/percona/percona-postgresql-operator)

## Introduction

Percona Operator for PostgreSQL automates and simplifies deploying and managing open source PostgreSQL clusters on Kubernetes. Percona Operator for PostgreSQL is based on [Postgres Operator](https://crunchydata.github.io/postgres-operator/latest/) developed by Crunchy Data.

Whether you need to get a simple PostgreSQL cluster up and running, need to deploy a high availability, fault tolerant cluster in production, or are running your own database-as-a-service, the Operator provides the essential features you need to keep your clusters healthy:

- PostgreSQL cluster provisioning
- High availability and disaster recovery
- Automated user management with password rotation
- Automated updates
- Support for both asynchronous and synchronous replication
- Scheduled and manual backups
- Integrated monitoring with [Percona Monitoring and Management](https://www.percona.com/software/database-tools/percona-monitoring-and-management)

## Status

**This project is in the tech preview state right now. Don't use it on production.**

# Architecture

Percona Operators are based on the [Operator SDK](https://github.com/operator-framework/operator-sdk) and leverage Kubernetes primitives to follow best CNCF practices.

Please read more about architecture and design decisions [here](https://docs.percona.com/percona-operator-for-postgresql/2.0/architecture.html).

## Quickstart installation

### kubectl

Quickly making the Operator up and running with cloud native PostgreSQL includes two main steps:

Deploy the operator from `deploy/bundle.yam`

```sh
kubectl apply --server-side -f https://raw.githubusercontent.com/percona/percona-postgresql-operator/main/deploy/bundle.yaml
```

Deploy the database cluster itself from `deploy/cr.yaml`

```sh
kubectl apply -f https://raw.githubusercontent.com/percona/percona-postgresql-operator/main/deploy/cr.yaml
```

# Contributing

Percona welcomes and encourages community contributions to help improve Percona Operator for PostgreSQL.

See the [Contribution Guide](CONTRIBUTING.md) for more information.

## Roadmap

We have an experimental public roadmap which can be found [here](https://github.com/percona/roadmap/projects/1). Please feel free to contribute and propose new features by following the roadmap [guidelines](https://github.com/percona/roadmap).

# Submitting Bug Reports

If you find a bug in Percona Docker Images or in one of the related projects, please submit a report to that project's [JIRA](https://jira.percona.com/browse/K8SPG) issue tracker. Learn more about submitting bugs, new features ideas and improvements in the [Contribution Guide](CONTRIBUTING.md).
