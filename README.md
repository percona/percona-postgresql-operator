# Percona Operator for PostgreSQL

![Percona Kubernetes Operators](kubernetes.svg)

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
![Docker Pulls](https://img.shields.io/docker/pulls/percona/percona-postgresql-operator)
![Docker Image Size (tag)](https://img.shields.io/docker/image-size/percona/percona-postgresql-operator/2)
![GitHub tag (latest by SemVer)](https://img.shields.io/github/v/tag/percona/percona-postgresql-operator?include_prereleases&sort=semver)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/percona/percona-postgresql-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/percona/percona-postgresql-operator)](https://goreportcard.com/report/github.com/percona/percona-postgresql-operator)

## Introduction

[Percona Operator for PostgreSQL](https://docs.percona.com/percona-operator-for-postgresql/2.0/index.html) automates and simplifies deploying and managing open source PostgreSQL clusters on Kubernetes. It is based on [Postgres Operator](https://github.com/CrunchyData/postgres-operator) developed by Crunchy Data.

Whether you need to get a simple PostgreSQL cluster up and running, need to deploy a high availability, fault tolerant cluster in production, or are running your own database-as-a-service, the Operator provides the essential features you need to keep your clusters healthy:

- PostgreSQL cluster provisioning
- High availability and disaster recovery
- Automated user management with password rotation
- Automated updates
- Support for both asynchronous and synchronous replication
- Scheduled and manual backups
- Integrated monitoring with [Percona Monitoring and Management](https://www.percona.com/software/database-tools/percona-monitoring-and-management)

While the Percona Operator is primarily managed through the command line, you can also use **[Percona Everest](https://docs.percona.com/everest/index.html)** for a web-based user interface. This open-source tool provides a streamlined experience for provisioning and managing your databases, simplifying day-to-day tasks and reducing administrative overhead. Learn more about Percona Everest in the [documentation](https://docs.percona.com/everest/index.html) or jump right in with the [quickstart guide](https://docs.percona.com/everest/quickstart-guide/quick-install.html).

## Architecture

Percona Operators are based on the [Operator SDK](https://github.com/operator-framework/operator-sdk) and leverage Kubernetes primitives to follow best CNCF practices.

Learn more about [architecture and design decisions](https://docs.percona.com/percona-operator-for-postgresql/2.0/architecture.html).

# Documentation

To learn more about the Operator, check the [Percona Operator for PostgreSQL documentation](https://docs.percona.com/percona-operator-for-postgresql/2.0/index.html). 

## Quickstart installation

Ready to try out the Operator? Check the [Quickstart tutorial](https://docs.percona.com/percona-operator-for-postgresql/2.0/quickstart.html) for easy-to follow steps. 

Below is one of the ways to deploy the Operator using `kubectl`.

### kubectl

1. Deploy the operator from `deploy/bundle.yaml`

```sh
kubectl apply --server-side -f https://raw.githubusercontent.com/percona/percona-postgresql-operator/main/deploy/bundle.yaml
```

2. Deploy the database cluster itself from `deploy/cr.yaml`

```sh
kubectl apply -f https://raw.githubusercontent.com/percona/percona-postgresql-operator/main/deploy/cr.yaml
```

# Need help?

**Commercial Support**  | **Community Support** |
:-: | :-: |
| <br/>Enterprise-grade assistance for your mission-critical PostgreSQL deployments with the Percona Operator for PostgreSQL. Get expert guidance for complex tasks like multi-cloud replication, database migration and building platforms.<br/><br/>  | <br/>Connect with our engineers and fellow users for general questions, troubleshooting, and sharing feedback and ideas.<br/><br/>  | 
| **[Get Percona Support](https://hubs.ly/Q02ZTH9s0)** | **[Visit our Forum](https://forums.percona.com/c/postgresql/percona-kubernetes-operator-for-postgresql/68)** |

# Contributing

Percona welcomes and encourages community contributions to help improve Percona Operator for PostgreSQL.

See the [Contribution Guide](CONTRIBUTING.md) on how you can contribute.

## Roadmap

We have a public roadmap which can be found [here](https://github.com/orgs/percona/projects/10). Please feel free to contribute and propose new features by following the roadmap [guidelines](https://github.com/percona/roadmap).

## Submitting Bug Reports

If you find a bug in Percona Docker Images or in one of the related projects, please submit a report to that project's [JIRA](https://jira.percona.com/browse/K8SPG) issue tracker or [create a GitHub issue](https://docs.github.com/en/issues/tracking-your-work-with-issues/creating-an-issue#creating-an-issue-from-a-repository) in this repository. 

Learn more about submitting bugs, new features ideas and improvements in the [Contribution Guide](CONTRIBUTING.md).
