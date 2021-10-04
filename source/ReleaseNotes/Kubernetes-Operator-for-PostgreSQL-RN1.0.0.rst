.. rn:: 1.0.0

================================================================================
*Percona Distribution for PostgreSQL Operator* 1.0.0
================================================================================

:Date: October 6, 2021
:Installation: `Installing Percona Distribution for PostgreSQL Operator <https://www.percona.com/doc/kubernetes-operator-for-postgresql/index.html#installation-guide>`_

**Percona announces the general availability of Percona Distribution for PostgreSQL Operator 1.0.0. This release is now the current GA release in the 1.0 series.**

Release highlights
================================================================================

* It is now possible to :ref:`configure scheduled backups<backups.scheduled>`
  following the declarative approach in the ``deploy/cr.yaml`` file, similar to
  other Percona Kubernetes Operators
* OpenShift compatibility allows :re:`running Percona Distribution for PostgreSQL on Red Hat OpenShift Container Platform<install-openshift>`

New Features and Improvements
================================================================================

* :jirabug:`K8SPG-106`: PostgreSQL Replicas are now managed through the main
   Custom Resource
* :jirabug:`K8SPG-96`: PMM Client container does not cause the crash of the
  whole database Pod if ``pmm-agent`` is not working properly
  made it possible to use :ref:`Google Cloud Storage as an object store for backups<backups.gcs>`
  without using third-party tools
* :jirabug:`K8SPG-95`: Merged upstream CrunchyData Operator vX.Y.X made it
  possible to change backup configuration on the running cluster with no need to
  delete and recreate it
* :jirabug:`K8SPG-86`: The Operator :ref:`is now compatible<install-openshift>`
  with the OpenShift platform
  This also adds the possibility of scaling up/scaling down replicas via the
  'deploy/cr.yaml' configuration file
* :jirabug:`K8SPG-62`: Configuring :ref:`scheduled backups<backups.scheduled>`
  through the main Custom Resource is now supported

Supported Platforms
================================================================================

The following platforms were tested and are officially supported by the
Operator 1.0.0:

* OpenShift 4.6 - 4.8
* Google Kubernetes Engine (GKE) 1.17 - 1.21
* Amazon Elastic Container Service for Kubernetes (EKS) 1.21

This list only includes the platforms that the Operator is specifically tested
on as a part of the release process. Other Kubernetes flavors and versions
depend on the backward compatibility offered by Kubernetes itself.

