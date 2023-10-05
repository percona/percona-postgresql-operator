## Percona is Cloud Native


Percona Operator for PostgreSQL automates and simplifies deploying and managing
open source PostgreSQL clusters on Kubernetes. Percona Operator for PostgreSQL is based on Postgres Operator developed by Crunchy Data.

Whether you need to get a simple PostgreSQL cluster up and running, need to deploy a high availability,
fault tolerant cluster in production, or are running your own database-as-a-service, the Operator provides the essential
features you need to keep your clusters healthy.

Consult the
[documentation](https://www.percona.com/doc/kubernetes-operator-for-postgresql/index.html)
on the Percona Operator for PostgreSQL for complete details on capabilities
and options.

### Supported Features

* **PostgreSQL Cluster Provisioning**

Create, Scale, & Delete PostgreSQL clusters with ease, while fully customizing your Pods and PostgreSQL configuration.

* **High Availability**

    Safe, automated failover backed by a distributed consensus based high-availability solution. Uses Pod Anti-Affinity to help resiliency; you can configure how much
    enforced this can be. Failed primaries automatically heal, allowing for faster recovery time. Support for standby PostgreSQL clusters that work both within and
    across multiple Kubernetes clusters.

* **Disaster Recovery**

    Backups and restores leverage the open source pgBackRest utility and includes support for full, incremental, and differential backups as well as efficient delta
    restores. Set how long you want your backups retained for. Works great with very large databases!

* **Communication Security**

    Secure communication between your applications and data servers by enabling TLS for your PostgreSQL servers, including the ability to enforce all of your connections
    to use TLS.

* **Monitoring**

    Track the health of your PostgreSQL clusters with Percona Monitoring and Management (PMM).

* **PostgreSQL User Management**

    Quickly add and remove users from your PostgreSQL clusters with powerful commands. Manage password expiration policies or use your preferred PostgreSQL authentication
    scheme.

* **Updates Management**

    Safely apply PostgreSQL updates with minimal availability impact to your PostgreSQL clusters.

* **Advanced Replication Support**

    Choose between asynchronous replication and synchronous replication for workloads that are sensitive to losing transactions.

* **Clone**

    Create new clusters from your existing clusters or backups.

* **Connection Pooling**

    Use [pgBouncer](https://access.crunchydata.com/documentation/postgres-operator/latest/tutorial/pgbouncer/) for connection pooling

* **Affinity and Tolerations**

    Have your PostgreSQL clusters deployed to Kubernetes Nodes of your preference with node affinity, or designate which nodes Kubernetes can schedule PostgreSQL instances
    to with Kubernetes  tolerations.

* **Scheduled Backups**

    Choose the type of backup (full, incremental, differential) and how frequently you want it to occur on each PostgreSQL cluster.

* **Backup to S3**

    Store your backups in Amazon S3 or any object storage system that supports the S3 protocol. The Operator can backup, restore, and create new clusters from these backups.

* **Multi-Namespace Support**

    You can control how the Operator leverages Kubernetes Namespaces with several different deployment models:

    * Deploy the Operator and all PostgreSQL clusters to the same namespace
    * Deploy the Operator to one Namespace, and all PostgreSQL clusters to a different Namespace
    * Deploy the Operator to one Namespace, and have your PostgreSQL clusters managed across multiple Namespaces

* **Full Customizability**

    The Operator not only makes it easy to get PostgreSQL up and running on Kubernetes-enabled platforms, but also allows you to further customize your deployments:

    * Selecting different storage classes for your primary, replica, and backup storage
    * Select your own container resources class for each PostgreSQL cluster deployment; differentiate between resources applied for primary and replica clusters
    * Use your own container image repository, including `imagePullSecrets` and private repositories  support
    * Customize your PostgreSQL configuration
    * Bring your own trusted certificate authority (CA) for use with the Operator API server
    * Override your PostgreSQL configuration for each cluster
    * Use your own custom images, re-define the image for each container separately
