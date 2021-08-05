.. rn:: 0.2.0

================================================================================
*Percona Distribution for PostgreSQL Operator* 0.2.0
================================================================================

:Date: August 9, 2021
:Installation: `Installing Percona Distribution for PostgreSQL Operator <https://www.percona.com/doc/kubernetes-operator-for-postgresql/index.html#installation-guide>`_

**Version 0.2.0 of the Percona Distribution for PostgreSQL Operator is a tech preview release and it is not recommended for production environments.**

New Features
================================================================================

* :jirabug:`K8SPG-41`: The Operator can now do fully automatic upgrades to the
  newer versions of Percona Distribution for PostgreSQL within the method named
  Smart Updates

Improvements
================================================================================

* :jirabug:`K8SPG-80`: Create wrapper for operator
* :jirabug:`K8SPG-54`: Rework Custom Resource to align with other Operators
* :jirabug:`K8SPG-53`: Add support for Google Cloud Storage for backups
* :jirabug:`K8SPG-42`: Improve backup task
* :jirabug:`K8SPG-30`: Add the possibility of scaling up/scaling down replicas via 'deploy/cr.yaml'
