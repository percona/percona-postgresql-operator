Providing Backups
=================

The Operator uses the open source `pgBackRest <https://pgbackrest.org/>`_ backup
and restore utility. A special *pgBackRest repository* is created by the
Operator along with creating a new PostgreSQL cluster to facilitate the usage of
the pgBackRest features in it.

The Operator usually stores PostgreSQL backups on `Amazon S3 or S3-compatible
storage <https://en.wikipedia.org/wiki/Amazon_S3#S3_API_and_competing_services>`_
outside the Kubernetes cluster. But storing backups on `Persistent Volume <https://kubernetes.io/docs/concepts/storage/persistent-volumes/>`_
attached to the pgBackRest Pod is also possible. At PostgreSQL cluster creation
time, you can specify a specific Storage Class for the pgBackRest repository.
Additionally, you can also specify the type of the pgBackRest repository that
can be used, including:

* ``local``: Uses the storage that is provided by the Kubernetes cluster’s
  Storage Class that you select,
* ``s3``: Use Amazon S3 or an object storage system that uses the S3 protocol,
* ``local,s3``: Use both the storage that is provided by the Kubernetes
  cluster’s Storage Class that you select AND Amazon S3 (or equivalent object
  storage system that uses the S3 protocol).

The pgBackRest repository consists of the following Kubernetes objects:

* A Deployment,
* A Secret that contains information that is specific to the PostgreSQL cluster
  that it is deployed with (e.g. SSH keys, AWS S3 keys, etc.),
* A Service.

The PostgreSQL primary is automatically configured to use the
``pgbackrest archive-push`` and push the write-ahead log (WAL) archives to the
correct repository.

The PostgreSQL Operator supports three types of pgBackRest backups:

* Full (``full``): A full backup of all the contents of the PostgreSQL cluster,
* Differential (``diff``): A backup of only the files that have changed since
  the last full backup,
* Incremental (``incr``): A backup of only the files that have changed since the
  last full or differential backup. Incremental backup is the default choice.

The Operator also supports setting pgBackRest retention policies for backups.
Backup retention can be controlled by the following pgBackRest options:

* ``--repo1-retention-full`` the number of full backups to retain,
* ``--repo1-retention-diff`` the number of differential backups to retain,
* ``--repo1-retention-archive`` how many sets of write-ahead log archives to
  retain alongside the full and differential backups that are retained.

.. _backups.configure:

Configuring the backup storage
------------------------------

To have backups enabled, the user should first configure the backup storage
in the ``deploy/cr.yaml`` configuration file.

In order to use S3-compatible storage for backups you need to provide some
S3-related information, such as proper S3 bucket name, endpoint, etc. This
information can be passed to pgBackRest via the following ``deploy/cr.yaml``
options:

* ``BackrestS3Bucket`` specifies the AWS S3 bucket that should be utilized,
  for example ``my-postgresql-backups-example``,
* ``BackrestS3Endpoint`` specifies the S3 endpoint that should be utilized,
  for example ``s3.amazonaws.com``,
* ``BackrestS3Region`` specifies the AWS S3 region that should be utilized,
  for example ``us-east-1``,
* ``BackrestS3URIStyle`` specifies whether ``host`` or ``path`` style URIs
  should be utilized,
    --pgbackrest-s3-verify-tls - set this value to “true” 
* ``BackrestS3VerifyTLS`` should be set to ``true`` to enable TLS verification
  or set to ``false`` to disable it.

You also need to feed pgBackRest with the AWS S3 key and the AWS S3 key secret
stored along with other sensitive information in Kubernetes Secrets. You can do
it by editing the ``deploy/backup/cluster1-backrest-repo-config-secret.yaml``
configuration file and applying it as usual:

.. code:: bash

   kubectl apply -f deploy/backup/cluster1-backrest-repo-config-secret.yaml


.. _backups-manual:

Making on-demand backup
-----------------------

To make an on-demand backup, the user should use a backup configuration file.
The example of the backup configuration file is `deploy/backup/backup.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/backup/backup.yaml>`_.

The most important keys in the parameters section of this file are the
following:

* ``parameters.backrest-opts`` is the string with command line options which
  will be passed to pgBackRest, for example
  ``--type=full --repo1-retention-full=5``,
* ``parameters.pg-cluster`` is the name of the PostgreSQL cluster to back up,
  for example ``cluster1``,
* ``parameters.podname`` is the name of the pgBackRest repository Pod, for
  example ``cluster1-backrest-shared-repo-5f84955754-xd52d``.

When the backup options are configured, the actual backup command is executed:

.. code:: bash

   kubectl apply -f deploy/backup/backup.yaml

.. _backups-restore:

Restore the cluster from a previously saved backup
--------------------------------------------------

The Operator supports the ability to perform a full restore on a PostgreSQL
cluster as well as a point-in-time-recovery. There are two types of ways to
restore a cluster:

* restore to a new cluster using the ``--restore-from`` option,
* restore in-place, to an existing cluster (note that this is destructive).

Restoring to a new PostgreSQL cluster allows you to take a backup and create a
new PostgreSQL cluster that can run alongside an existing one. There are several
scenarios where using this technique is helpful:

* Creating a copy of a PostgreSQL cluster that can be used for other purposes.
  Another way of putting this is *creating a clone*.
* Restore to a point-in-time and inspect the state of the data without affecting
  the current cluster.

To restore the previously saved backup the user should use a *backup restore*
configuration file. The example of the backup configuration file is
`deploy/backup/restore.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/backup/restore.yaml>`_.

The most important keys in the parameters section of this file are the
following:

* ``parameters.backrest-restore-from-cluster`` specifies the name of a
  PostgreSQL cluster (either one that is active, or a former cluster whose
  pgBackRest repository still exists) to restore from (for example,
  ``cluster1``),
* ``parameters.backrest-restore-opts`` specifies additional options for
  pgBackRest (for example, ``--type=time --target="2021-04-16 15:13:32"`` to
  perform a point-in-time-recovery),
* ``parameters.backrest-storage-type`` the type of the pgBackRest repository,
  (for example, ``local``).

The actual restoration process can be started as follows:

   .. code:: bash

      kubectl apply -f deploy/backup/restore.yaml

