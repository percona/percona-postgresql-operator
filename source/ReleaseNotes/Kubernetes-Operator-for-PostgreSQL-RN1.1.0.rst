.. rn:: 1.1.0

================================================================================
*Percona Distribution for PostgreSQL Operator* 1.1.0
================================================================================

:Date: December 7, 2021
:Installation: `Installing Percona Distribution for PostgreSQL Operator <https://www.percona.com/doc/kubernetes-operator-for-postgresql/index.html#installation-guide>`_

Release Highlights
================================================================================

* :ref:`A Kubernetes-native horizontal scaling<operator-scale>` capability was added to the Custom Resource to unblock Horizontal Pod Autoscaler and Kubernetes Event-driven Autoscaling (KEDA) usage
* The :ref:`Smart Upgrade functionality<operator-update-smartupdates>` along with the technical preview of the Version Service allows users to automatically get the latest version of the software compatible with the Operator and apply it safely
* Percona Distribution for PostgreSQL Operator now supports PostgreSQL 14

New Features
================================================================================

* :jirabug:`K8SPG-101`: Add support for Kubernetes horizontal scaling to set the number of Replicas dynamically via the ``kubectl scale`` command or Horizontal Pod Autoscaler
* :jirabug:`K8SPG-77`: Add support for PostgreSQL 14 in the Operator
* :jirabug:`K8SPG-75`: :ref:`Manage Operator’s system users<users>` hrough a single Secret resource even after cluster creation
* :jirabug:`K8SPG-71`: Add Smart Upgrade functionality to automate Percona Distribution for PostgreSQL upgrades

Improvements
================================================================================

* :jirabug:`K8SPG-96`: PMM container does not cause the crash of the whole database Pod if pmm-agent is not working properly

Bugs Fixed
================================================================================

* :jirabug:`K8SPG-120`: The Operator default behavior is now to keep backups and PVCs when the cluster is deleted

Supported platforms
-------------------

The following platforms were tested and are officially supported by the Operator
1.1.0:

* `Google Kubernetes Engine (GKE) <https://cloud.google.com/kubernetes-engine>`_ 1.19 - 1.22
* `Amazon Elastic Container Service for Kubernetes (EKS) <https://aws.amazon.com>`_ 1.18 - 1.21
* `OpenShift <https://www.redhat.com/en/technologies/cloud-computing/openshift>`_ 4.7 - 4.9

This list only includes the platforms that the Percona Operators are specifically tested on as part of the release process. Other Kubernetes flavors and versions depend on the backward compatibility offered by Kubernetes itself.
