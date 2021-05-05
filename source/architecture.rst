Design overview
===============

The Percona Distribution for PostgreSQL Operator automates and simplifies
deploying and managing open source PostgreSQL clusters on Kubernetes.
The Operator is based on `Postgres Operator <https://crunchydata.github.io/postgres-operator/latest/>`_
developed by Crunchy Data.

.. image:: ./assets/images/pgo.png
   :align: center

PostgreSQL containers deployed with the PostgreSQL Operator include the following components:

* The `PostgreSQL <https://www.postgresql.org/>`_ database management system, including:
  * `PostgreSQL Additional Supplied Modules <https://www.postgresql.org/docs/current/contrib.html>`_,
  * `pgAudit <https://www.pgaudit.org/>`_ PostgreSQL auditing extension,
  * `PostgreSQL set_user Extension Module <https://github.com/pgaudit/set_user>`_,
  * `wal2json output plugin <https://github.com/eulerto/wal2json>`_,
* The `pgBackRest <https://pgbackrest.org/>`_ Backup & Restore utility,
* The `pgBouncer <http://pgbouncer.github.io/>`_ connection pooler for PostgreSQL,
* The PostgreSQL high-availability implementation based on the `Patroni template <https://patroni.readthedocs.io/>`_,
* the `pg_stat_monitor <https://github.com/percona/pg_stat_monitor/>`_ PostgreSQL Query Performance Monitoring utility,
* LLVM (for JIT compilation).

To provide high availability the Operator involves `node affinity <https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#affinity-and-anti-affinity>`_
to run PostgreSQL Cluster instances on separate worker nodes if possible. If
some node fails, the Pod with it is automatically re-created on another node.

.. image:: ./assets/images/operator.png
   :align: center

To provide data storage for stateful applications, Kubernetes uses
Persistent Volumes. A *PersistentVolumeClaim* (PVC) is used to implement
the automatic storage provisioning to pods. If a failure occurs, the
Container Storage Interface (CSI) should be able to re-mount storage on
a different node.

The Operator functionality extends the Kubernetes API with
*PerconaXtraDBCluster* object, and it is implemented as a golang
application. Each *PerconaXtraDBCluster* object maps to one separate Percona
XtraDB Cluster setup. The Operator listens to all events on the created objects.
When a new PerconaXtraDBCluster object is created, or an existing one undergoes
some changes or deletion, the operator automatically
creates/changes/deletes all needed Kubernetes objects with the
appropriate settings to provide a proper Percona PostgreSQL Cluster operation.
