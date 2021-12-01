.. _K8SPG-1.1.0:

================================================================================
*Percona Kubernetes Operator for PostgreSQL* 1.1.0
================================================================================

:Date: December 7, 2021
:Installation: `Installing Percona Distribution for PostgreSQL Operator <https://www.percona.com/doc/kubernetes-operator-for-postgresql/index.html#installation-guide>`_

Release Highlights
================================================================================

* A Kubernetes-native horizontal scaling capability was added to the Custom Resource to unblock Horizontal Pod Autoscaler and KEDA usage
* Smart Upgarde functionality along with the Version Service allows users to automatically get the latest version of the software compatible with the Operator and apply it safely.

New Features
================================================================================

* :jirabug:`K8SPG-101`: Add support for Kubernetes horizontal scaling
* :jirabug:`K8SPG-77`: Add support for PostgreSQL 14 in the Operator
* :jirabug:`K8SPG-75`: Add possibility to change passwords via secrets
* :jirabug:`K8SPG-71`: Add Smart Upgrade functionality 

Improvements
================================================================================

* :jirabug:`K8SPG-96`: PMM container should not cause the crash of the whole database Pod if pmm-agent is not working properly
* :jirabug:`K8SPG-86`: The Operator is now certified to run on Openshift. Openshift certification provides guarantee to Enterprise users that our Operator is production ready and can safely run on Openshift. This improvement does not require code changes, but pass through a rigorous certification process.

Bugs Fixed
================================================================================

* :jirabug:`K8SPG-120`: The Operator default behavior is now to keep backups and PVCs when the cluster is deleted
