.. rn:: 0.1.0

================================================================================
*Percona Distribution for PostgreSQL Operator* 0.1.0
================================================================================

:Date: May 10, 2021
:Installation: `Installing Percona Distribution for PostgreSQL Operator <https://www.percona.com/doc/kubernetes-operator-for-postgresql/index.html#installation-guide>`_

The Percona Operator is based on best practices for configuration and setup of
a `Percona Distribution for PostgreSQL on Kubernetes <https://www.percona.com/doc/postgresql/LATEST/index.html>`_.
The benefits of the Operator are many, but saving time and delivering a
consistent and vetted environment is key.

Kubernetes provides users with a distributed orchestration system that automates
the deployment, management, and scaling of containerized applications. The
Operator extends the Kubernetes API with a new custom resource for deploying,
configuring, and managing the application through the whole life cycle.
You can compare the Kubernetes Operator to a System Administrator who deploys
the application and watches the Kubernetes events related to it, taking
administrative/operational actions when needed.

**Version 0.1.0 of the Percona Distribution for PostgreSQL Operator is a tech preview release and it is not recommended for production environments.**

You can install *Percona Distribution for PostgreSQL Operator* on Kubernetes,
`Google Kubernetes Engine (GKE) <https://cloud.google.com/kubernetes-engine>`_,
and `Amazon Elastic Kubernetes Service (EKS) <https://aws.amazon.com/eks>`_
clusters. The Operator is based on `Postgres Operator developed by Crunchy Data <https://access.crunchydata.com/documentation/postgres-operator/latest/>`_.

Here are the main differences between v 0.1.0 and the original Operator:

* Percona Distribution for PostgreSQL is now used as the main container image.
* It is possible to specify custom images for all components separately. For
  example, users can easily build and use custom images for one or several
  components (e.g. pgBouncer) while all other images will be the official ones.
  Also, users can build and use all custom images.
* All container images are reworked and simplified. They are built on Red Hat
  Universal Base Image (UBI) 8.
* The Operator has built-in integration with Percona Monitoring and Management
  v2.
* A build/test infrastructure was created, and we have started adding e2e tests
  to be sure that all pieces of the cluster work together as expected.
* We have phased out the ``pgo`` CLI tool, and the Custom Resource UX will be
  completely aligned with other Percona Operators in the following release.

Once Percona Operator is promoted to GA, users would be able to get the full
package of services from Percona teams.

While the Operator is in its very first release, instructions on how to install
and configure it `are already available <https://percona.com/doc/kubernetes-operator-for-postgresql>`_
along with the source code hosted `in our Github repository <https://github.com/percona/percona-postgresql-operator>`_.

Help us improve our software quality by reporting any bugs you encounter using
`our bug tracking system <https://jira.percona.com/secure/Dashboard.jspa>`_.

