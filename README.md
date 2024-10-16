# Percona Operator for PostgreSQL

![Percona Kubernetes Operators](kubernetes.svg)

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

You interact with Percona Operator mostly via the command line tool. If you feel more comfortable with operating the Operator and database clusters via the web interface, there is [Percona Everest](https://docs.percona.com/everest/index.html) - an open-source web-based database provisioning tool available for you. It automates day-to-day database management operations for you, reducing the overall administrative overhead. [Get started with Percona Everest](https://docs.percona.com/everest/quickstart-guide/quick-install.html).

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

## Contributing

Percona welcomes and encourages community contributions to help improve Percona Operator for PostgreSQL.

See the [Contribution Guide](CONTRIBUTING.md) on how you can contribute.

## Communication

We would love to hear from you! Reach out to us on [Forum](https://forums.percona.com/c/postgresql/percona-kubernetes-operator-for-postgresql/68) with your questions, feedback and ideas

## Join Percona Kubernetes Squad!                                                                             
```                                                                                     
                    %                        _____                
                   %%%                      |  __ \                                          
                 ###%%%%%%%%%%%%*           | |__) |__ _ __ ___ ___  _ __   __ _             
                ###  ##%%      %%%%         |  ___/ _ \ '__/ __/ _ \| '_ \ / _` |            
              ####     ##%       %%%%       | |  |  __/ | | (_| (_) | | | | (_| |            
             ###        ####      %%%       |_|   \___|_|  \___\___/|_| |_|\__,_|           
           ,((###         ###     %%%        _      _          _____                       _
          (((( (###        ####  %%%%       | |   / _ \       / ____|                     | | 
         (((     ((#         ######         | | _| (_) |___  | (___   __ _ _   _  __ _  __| | 
       ((((       (((#        ####          | |/ /> _ </ __|  \___ \ / _` | | | |/ _` |/ _` |
      /((          ,(((        *###         |   <| (_) \__ \  ____) | (_| | |_| | (_| | (_| |
    ////             (((         ####       |_|\_\\___/|___/ |_____/ \__, |\__,_|\__,_|\__,_|
   ///                ((((        ####                                  | |                  
 /////////////(((((((((((((((((########                                 |_|   Join @ percona.com/k8s   
```

You can get early access to new product features, invite-only ”ask me anything” sessions with Percona Kubernetes experts, and monthly swag raffles. Interested? Fill in the form at [percona.com/k8s](https://www.percona.com/k8s).

## Roadmap

We have an experimental public roadmap which can be found [here](https://github.com/percona/roadmap/projects/1). Please feel free to contribute and propose new features by following the roadmap [guidelines](https://github.com/percona/roadmap).

## Submitting Bug Reports

If you find a bug in Percona Docker Images or in one of the related projects, please submit a report to that project's [JIRA](https://jira.percona.com/browse/K8SPG) issue tracker or [create a GitHub issue](https://docs.github.com/en/issues/tracking-your-work-with-issues/creating-an-issue#creating-an-issue-from-a-repository) in this repository. 

Learn more about submitting bugs, new features ideas and improvements in the [Contribution Guide](CONTRIBUTING.md).
