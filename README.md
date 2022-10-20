# Percona Operator for PostgreSQL

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

## Introduction

Percona Operator for PostgreSQL automates and simplifies deploying and managing open source PostgreSQL clusters on Kubernetes. Percona Operator for PostgreSQL is based on [Postgres Operator](https://crunchydata.github.io/postgres-operator/latest/) developed by Crunchy Data.

Whether you need to get a simple PostgreSQL cluster up and running, need to deploy a high availability, fault tolerant cluster in production, or are running your own database-as-a-service, the Operator provides the essential features you need to keep your clusters healthy:

- **PostgreSQL Cluster Provisioning**

  Create, Scale, & Delete PostgreSQL clusters with ease, while fully customizing your Pods and PostgreSQL configuration.

- **High Availability**

  Safe, automated failover backed by a distributed consensus based high-availability solution. Uses Pod Anti-Affinity to help resiliency; you can configure how much enforced this can be. Failed primaries automatically heal, allowing for faster recovery time. Support for standby PostgreSQL clusters that work both within and across multiple Kubernetes clusters.

- **Disaster Recovery**

  Backups and restores leverage the open source pgBackRest utility and includes support for full, incremental, and differential backups as well as efficient delta restores. Set how long you want your backups retained for. Works great with very large databases!

- **Communication Security**

  Secure communication between your applications and data servers by enabling TLS for your PostgreSQL servers, including the ability to enforce all of your connections to use TLS.

- **PostgreSQL User Management**

  Quickly add and remove users from your PostgreSQL clusters with powerful commands. Manage password expiration policies or use your preferred PostgreSQL authentication scheme.

- **Updates Management**

  Safely apply PostgreSQL updates with minimal availability impact to your PostgreSQL clusters.

- **Advanced Replication Support**

  Choose between asynchronous replication and synchronous replication for workloads that are sensitive to losing transactions.

- **Clone**

  Create new clusters from your existing clusters or backups.

- **Connection Pooling**

  Use [pgBouncer](https://access.crunchydata.com/documentation/postgres-operator/latest/tutorial/pgbouncer/) for connection pooling

- **Affinity and Tolerations**

  Have your PostgreSQL clusters deployed to Kubernetes Nodes of your preference with node affinity, or designate which nodes Kubernetes can schedule PostgreSQL instances to with Kubernetes tolerations.

- **Scheduled Backups**

  Choose the type of backup (full, incremental, differential) and how frequently you want it to occur on each PostgreSQL cluster.

- **Backup to S3**

  Store your backups in Amazon S3 or any object storage system that supports the S3 protocol. The Operator can backup, restore, and create new clusters from these backups.

- **Multi-Namespace Support**

  You can control how the Operator leverages Kubernetes Namespaces with several different deployment models:

  - Deploy the Operator and all PostgreSQL clusters to the same namespace
  - Deploy the Operator to one Namespace, and all PostgreSQL clusters to a different Namespace
  - Deploy the Operator to one Namespace, and have your PostgreSQL clusters managed across multiple Namespaces

- **Full Customizability**

  The Operator not only makes it easy to get PostgreSQL up and running on Kubernetes-enabled platforms, but also allows you to further customize your deployments, including:

  - Selecting different storage classes for your primary, replica, and backup storage
  - Select your own container resources class for each PostgreSQL cluster deployment; differentiate between resources applied for primary and replica clusters
  - Use your own container image repository, including `imagePullSecrets` and private repositories support
  - Customize your PostgreSQL configuration
  - Bring your own trusted certificate authority (CA) for use with the Operator API server
  - Override your PostgreSQL configuration for each cluster
  - Use your own custom images, re-define the image for each container separately

## Deployment Requirements

The Operator is validated for deployment on Kubernetes, GKE and EKS clusters. The Operator is cloud native and storage agnostic, working with a wide variety of storage classes, hostPath, and NFS.

The Operator includes various components that get deployed to your Kubernetes cluster as shown in the following diagram and detailed in the Design section of the documentation for the version you are running.

### Supported Platforms

The following platforms were tested and are officially supported by the Operator:

- Google Kubernetes Engine (GKE) 1.21 - 1.24

Other Kubernetes platforms may also work but have not been tested.

### Storage

The Operator is tested with a variety of different types of Kubernetes storage and Storage Classes, as well as hostPath and NFS.
The variety of different Storage Classes available for Kubernetes is too wide to verify the Operator functionality in each one. With that said, the Operator is designed to be storage class agnostic and has been demonstrated to work with additional Storage Classes.

## Quickstart installation

### kubectl

Quickly making the Operator up and running with cloud native PostgreSQL includes two main steps:

Deploy the operator from `deploy/bundle.yam`

```sh
kubectl apply --server-side -f https://raw.githubusercontent.com/percona/percona-postgresql-operator/pg_2.0/deploy/bundle.yaml
```

Deploy the database cluster itself from `deploy/cr.yaml`

```sh
kubectl apply -f https://raw.githubusercontent.com/percona/percona-postgresql-operator/pg_2.0/deploy/cr.yaml
```

### Backups/restores

Backups could be triggered via the cron-like string and on-demand. The common step for both ways is to create secret for accessing the remote object storage

```sh
printf "[global]\nrepo1-s3-key=%s\nrepo1-s3-key-secret=%s\n" 'YOUR_S3_ACCESS_KEY' 'YOUR_S3_SECRET_KEY' > /tmp/repo1-secret.ini
kubectl create secret generic your-cluster-pgbackrest-secrets --from-file=s3.conf=/tmp/repo1-secret.ini
```

If you need more than one object storage, you can increase repo index by 1 and add the incremented `repoN` options to the ini file.
Also GCS, Azure have own suffixes for `repoN` options and they should be preserved.

Now the focus should be switched to CR part.

```yaml
spec:
  backups:
    pgbackrest:
      manual:
        repoName: repo1
        options:
          - --type=full
      image: perconalab/percona-postgresql-operator:main-ppg14-pgbackrest
      configuration:
        - secret:
            name: your-cluster-pgbackrest-secrets
      global:
        repo1-path: /backrestrepo/postgres-operator/your-cluster/repo1
      repos:
        - name: repo1
          s3:
            bucket: "your-bucket"
            endpoint: "s3.amazonaws.com"
            region: "us-east-1"
```

The listed options do allow the user to force the operator to make on-demand backup by calling:

```sh
kubectl annotate perconapgclusters.pg.percona.com your-cluster --overwrite pg.percona.com/pgbackrest-backup="$(date)"
```

Alternatively, backup schedule could be used:

```yaml
spec:
  backups:
    pgbackrest:
      repos:
        - name: repo1
          schedules:
            full: "0 1 * * 0"
            differential: "0 1 * * 1-6"
```

As for restores, the actions are pretty much the same. Add the restore section to already running CR first

```yaml
spec:
  backups:
    pgbackrest:
      restore:
        enabled: true
        repoName: repo1
        options:
          - --type=time
          - --target="2022-10-03 14:15:11+03"
```

where target is the time to be restored at by the backup available. You may use `kubectl patch` or `kubectl edit` for this purpose.

Second, put the already familiar annotation with one small difference:

```sh
kubectl annotate perconapgclusters.pg.percona.com your-cluster --overwrite pg.percona.com/pgbackrest-restore=id1
```

Once the restore is done, please switch `enabled: true` to `enabled: false`.

### Start cluster from previously saved backup

It's quite a common request to be able to start the cluster up from some others cluster backup.
Here is an example CR for that.

```yaml
apiVersion: pg.percona.com/v2beta1
kind: PerconaPGCluster
metadata:
  name: your-cluster-rev2
spec:
  image: perconalab/percona-postgresql-operator:main-ppg14-postgres
  postgresVersion: 14
  dataSource:
    pgbackrest:
      stanza: db
      configuration:
        - secret:
            name: your-cluster-pgbackrest-secrets
      global:
        repo1-path: /backrestrepo/postgres-operator/your-cluster/repo1/
      repo:
        name: repo1
        s3:
          bucket: "your-bucket"
          endpoint: "s3.amazonaws.com"
          region: "us-east-1"
  instances:
    - dataVolumeClaimSpec:
        accessModes:
          - "ReadWriteOnce"
        resources:
          requests:
            storage: 1Gi
  backups:
    pgbackrest:
      image: perconalab/percona-postgresql-operator:main-ppg14-pgbackrest
      configuration:
        - secret:
            name: your-cluster-rev2-pgbackrest-secrets
      global:
        repo1-path: /backrestrepo/postgres-operator/your-cluster-rev2/repo1
      repos:
        - name: repo1
          s3:
            bucket: "your-other-bucket"
            endpoint: "s3.amazonaws.com"
            region: "us-east-1"
  proxy:
    pgBouncer:
      image: perconalab/percona-postgresql-operator:main-ppg14-pgbouncer
```

As it may be noticed, `dataSource` section is a key to populate already existing backup to the newly created cluster. Other options stay pretty much the same as before.

### Start standby cluster with cloudbased repo source

Simple example for getting the matter online.

```yaml
apiVersion: pg.percona.com/v2beta1
kind: PerconaPGCluster
metadata:
  name: cluster-standby
spec:
  backups:
    pgbackrest:
      configuration:
        - secret:
            name: pgo-gcs-creds
      global:
        repo1-path: /pgbackrest/postgres-operator/data-source-gcs/repo1
      image: perconalab/percona-postgresql-operator:main-ppg14-pgbackrest
      repos:
        - gcs:
            bucket: some-bucket
          name: repo1
  image: perconalab/percona-postgresql-operator:main-ppg14-postgres
  instances:
    - dataVolumeClaimSpec:
        accessModes:
          - ReadWriteOnce
        resources:
          requests:
            storage: 1Gi
      name: ""
      replicas: 1
  port: 5432
  postgresVersion: 14
  standby:
    enabled: true
    repoName: repo1
```

Please note that `data-source-gcs` is the upstream cluster which provides data changes for our
standby.

Feel free to read the [upstream](https://access.crunchydata.com/documentation/postgres-operator/v5/tutorial/backup-management/) doc for more information.
