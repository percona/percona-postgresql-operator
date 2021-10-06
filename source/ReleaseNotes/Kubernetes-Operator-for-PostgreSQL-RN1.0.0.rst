.. rn:: 1.0.0

================================================================================
*Percona Distribution for PostgreSQL Operator* 1.0.0
================================================================================

:Date: October 6, 2021
:Installation: `Installing Percona Distribution for PostgreSQL Operator <https://www.percona.com/doc/kubernetes-operator-for-postgresql/index.html#installation-guide>`_

**Percona announces the general availability of Percona Distribution for PostgreSQL Operator 1.0.0.**

The Percona Distribution for PostgreSQL Operator automates the lifecycle, simplifies deploying and managing open source PostgreSQL clusters on Kubernetes.

The Operator follows best practices for configuration and setup of the `Percona Distribution for PostgreSQL <https://www.percona.com/doc/postgresql/LATEST/index.html>`_. The Operator provides a consistent way to package, deploy, manage, and perform a backup and a restore for a Kubernetes application. Operators deliver automation advantages in cloud-native applications.

The advantages are the following:

* Deploy a Percona Distribution for PostgreSQL with no single point of failure
  and environment which can span multiple availability zones
* Modify the Percona Distribution for PostgreSQL size parameter to add or remove
  PostgreSQL instances
* Use single Custom Resource as a universal entry point to configure the
  cluster, similar to other Percona Operators
* Carry on semi-automatic upgrades of the Operator and PostgreSQL to newer
  versions
* Integrate with Percona Monitoring and Management (PMM) to seamlessly monitor
  your Percona Distribution for PostgreSQL
* Automate backups or perform on-demand backups as needed with support for
  performing an automatic restore
* Use cloud storage with S3-compatible APIs or Google Cloud for backups
* Use Transport Layer Security (TLS) for the replication and client traffic
* Support advanced Kubernetes features such as pod disruption budgets, node
  selector, constraints, tolerations, priority classes, and
  affinity/anti-affinity

 Percona Distribution for PostgreSQL Operator is based on `Postgres Operator <https://crunchydata.github.io/postgres-operator/latest/>`_ developed by Crunchy Data.

Release Highlights
================================================================================

* It is now possible to :ref:`configure scheduled backups<backups.scheduled>`
  following the declarative approach in the ``deploy/cr.yaml`` file, similar to
  other Percona Kubernetes Operators
* OpenShift compatibility allows :ref:`running Percona Distribution for PostgreSQL on Red Hat OpenShift Container Platform<install-openshift>`
* For the first time, the main functionality of the Operator is covered by
  functional tests, which ensure the overall quality and stability

New Features and Improvements
================================================================================

* :jirabug:`K8SPG-96`: PMM Client container does not cause the crash of the
  whole database Pod if ``pmm-agent`` is not working properly
* :jirabug:`K8SPG-86`: The Operator :ref:`is now compatible<install-openshift>`
  with the OpenShift platform
* :jirabug:`K8SPG-62`: Configuring :ref:`scheduled backups<backups.scheduled>`
  through the main Custom Resource is now supported
* :jirabug:`K8SPG-99`, :jirabug:`K8SPG-131`: The Operator documentation was
  substantially improved, and now it covers among other things the usage of
  Transport Layer Security (TLS) for internal and external communications, and
  cluster upgrades

Supported Platforms
================================================================================

The following platforms were tested and are officially supported by Operator
1.0.0:

* `OpenShift <https://www.redhat.com/en/technologies/cloud-computing/openshift>`_ 4.6 - 4.8
* `Google Kubernetes Engine (GKE) <https://cloud.google.com/kubernetes-engine>`_ 1.17 - 1.21
* `Amazon Elastic Container Service for Kubernetes (EKS) <https://aws.amazon.com>`_ 1.21

This list only includes the platforms that the Operator is specifically tested
on as a part of the release process. Other Kubernetes flavors and versions
depend on the backward compatibility offered by Kubernetes itself.

