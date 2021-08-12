.. rn:: 0.2.0

================================================================================
*Percona Distribution for PostgreSQL Operator* 0.2.0
================================================================================

:Date: August 12, 2021
:Installation: `Installing Percona Distribution for PostgreSQL Operator <https://www.percona.com/doc/kubernetes-operator-for-postgresql/index.html#installation-guide>`_

**Version 0.2.0 of the Percona Distribution for PostgreSQL Operator is a Beta release, and it is not recommended for production environments.**

New Features and Improvements
================================================================================

* :jirabug:`K8SPG-80`: The Custom Resource structure was reworked to provide the
  same look and feel as in other Percona Operators. Read more about Custom
  Resource options in the :ref:`documentation<operator.custom-resource-options>`
  and review the default  ``deploy/cr.yaml`` configuration file on `GitHub <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`_.
* :jirabug:`K8SPG-53`: Merged upstream `CrunchyData Operator v4.7.0 <https://github.com/CrunchyData/postgres-operator/releases/tag/v4.7.0>`_
  made it possible to use :ref:`Google Cloud Storage as an object store for backups<backups-gcs>`
  without using third-party tools
* :jirabug:`K8SPG-42`: There is no need to specify the name of the pgBackrest
  Pod in the backup manifest anymore as it is detected automatically by the
  Operator
* :jirabug:`K8SPG-30`: Replicas management is now performed through a main
  Custom Resource manifest instead of creating separate Kubernetes resources.
  This also adds the possibility of scaling up/scaling down replicas via the
  'deploy/cr.yaml' configuration file
* :jirabug:`K8SPG-66`: Helm chart is now :ref:`officially provided with the Operator<install-helm>`




