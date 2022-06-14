.. _backups:

Providing Backups
=================

The Operator allows doing backups in two ways.
*Scheduled backups* are configured in the
`deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`_
file to be executed automatically in proper time. *On-demand backups*
can be done manually at any moment.

.. contents:: :local:

.. _backups.pgbackrest:

The Operator uses the open source `pgBackRest <https://pgbackrest.org/>`_ backup
and restore utility. A special *pgBackRest repository* is created by the
Operator along with creating a new PostgreSQL cluster to facilitate the usage of
the pgBackRest features in it.

The Operator can store PostgreSQL backups on Amazon S3, `any S3-compatible
storage <https://en.wikipedia.org/wiki/Amazon_S3#S3_API_and_competing_services>`_
and `Google Cloud Storage <https://cloud.google.com/storage>`_ outside the 
Kubernetes cluster. Storing backups on `Persistent Volume <https://kubernetes.io/docs/concepts/storage/persistent-volumes/>`_
attached to the pgBackRest Pod is also possible. At PostgreSQL cluster creation
time, you can specify a specific Storage Class for the pgBackRest repository.
Additionally, you can also specify the type of the pgBackRest repository that
can be used for backups:

.. _backups.pgbackrest.repo.type:

* ``local``: Uses the storage that is provided by the Kubernetes cluster’s
  Storage Class that you select (for historical reasons this repository type can
  be alternatively named ``posix``),
* ``s3``: Use Amazon S3 or an object storage system that uses the S3 protocol,
* ``local,s3``: Use both the storage that is provided by the Kubernetes
  cluster’s Storage Class that you select AND Amazon S3 (or equivalent object
  storage system that uses the S3 protocol).
* ``gcs``: Use Google Cloud Storage,
* ``local,gcs``: Use both the storage that is provided by the Kubernetes
  cluster’s Storage Class that you select AND Google Cloud Storage.

.. _backups.pgbackrest.repository:

The pgBackRest repository consists of the following Kubernetes objects:

* A Deployment,
* A Secret that contains information that is specific to the PostgreSQL cluster
  that it is deployed with (e.g. SSH keys, AWS S3 keys, etc.),
* A Pod with a number of supporting scripts,
* A Service.

The PostgreSQL primary is automatically configured to use the
``pgbackrest archive-push`` and push the write-ahead log (WAL) archives to the
correct repository.

.. _backups.pgbackrest.backup.type:

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

You can set both backups type and retention policy when :ref:`backups-manual`.

Also you should first configure the backup storage in the ``deploy/cr.yaml``
configuration file to have backups enabled.

.. _backups.configure:

Configuring the S3-compatible backup storage
--------------------------------------------

In order to use S3-compatible storage for backups you need to provide some
S3-related information, such as proper S3 bucket name, endpoint, etc. This
information can be passed to pgBackRest via the following ``deploy/cr.yaml``
options in the ``backup.storages`` subsection:

* ``bucket`` specifies the AWS S3 bucket that should be utilized,
  for example ``my-postgresql-backups-example``,
* ``endpointUrl`` specifies the S3 endpoint that should be utilized,
  for example ``s3.amazonaws.com``,
* ``region`` specifies the AWS S3 region that should be utilized,
  for example ``us-east-1``,
* ``uriStyle`` specifies whether ``host`` or ``path`` style URIs
  should be utilized,
* ``verifyTLS`` should be set to ``true`` to enable TLS verification
  or set to ``false`` to disable it,
* ``type`` should be set to ``s3``.

You also need to supply pgBackRest with base64-encoded AWS S3 key and AWS S3 key
secret stored along with other sensitive information in `Kubernetes Secrets <https://kubernetes.io/docs/concepts/configuration/secret/>`_
(e.g. encoding needed data with the ``echo "string-to-encode" | base64``
command). Edit the ``deploy/backup/cluster1-backrest-repo-config-secret.yaml``
configuration file: set there proper cluster name, AWS S3 key, and key secret:

   .. code:: yaml

      apiVersion: v1
      kind: Secret
      metadata:
        name: <cluster-name>-backrest-repo-config
      type: Opaque
      data:
        aws-s3-key: <base64-encoded-AWS-S3-key>
        aws-s3-key-secret: <base64-encoded-AWS-S3-key-secret>

When done, create the secret as follows:

.. code:: bash

   $ kubectl apply -f deploy/backup/cluster1-backrest-repo-config-secret.yaml

Finally, create or update the cluster:

.. code:: bash

   $ kubectl apply -f deploy/cr.yaml

.. _backups.gcs:

Use Google Cloud Storage for backups
------------------------------------

You can configure `Google Cloud Storage <https://cloud.google.com/storage>`_ as
an object store for backups similarly to :ref:`S3 storage<backups.configure>`.

In order to use Google Cloud Storage (GCS) for backups you need to provide some
GCS-related information, such as a proper GCS bucket name. This
information can be passed to ``pgBackRest`` via the following options in the
``backup.storages`` subsection of the ``deploy/cr.yaml`` configuration file:

* ``bucket`` should contain the proper bucket name,

* ``type`` should be set to ``gcs``.

The Operator will also need your service account key to access storage.

#. Create your service account key following the `official Google Cloud instructions <https://cloud.google.com/iam/docs/creating-managing-service-account-keys>`_.

#. Export this key from your Google Cloud account.

   .. |rarr|   unicode:: U+02192 .. RIGHTWARDS ARROW

   You can find your key in the Google Cloud console (select *IAM & Admin*
   |rarr| *Service Accounts* in the left menu panel, then click your account and
   open the *KEYS* tab):

   .. image:: ./assets/images/gcs-service-account.svg
      :align: center

   Click the *ADD KEY* button, chose *Create new key* and chose *JSON* as a key
   type. These actions will result in downloading a file in JSON format with
   your new private key and related information.

#. Now you should use a base64-encoded version of this file and to create the `Kubernetes Secret <https://kubernetes.io/docs/concepts/configuration/secret/>`_. You can encode
   the file with the ``base64 <filename>`` command. When done, create the
   following yaml file with your cluster name and base64-encoded file contents:

   .. code:: yaml

      apiVersion: v1
      kind: Secret
      metadata:
        name: <cluster-name>-backrest-repo-config
      type: Opaque
      data:
        gcs-key: <base64-encoded-json-file-contents>

   When done, create the secret as follows:

   .. code:: bash

      $ kubectl apply -f ./my-gcs-account-secret.yaml

#. Finally, create or update the cluster:

   .. code:: bash

      $ kubectl apply -f deploy/cr.yaml

.. _backups.scheduled:

Scheduling backups
------------------------

Backups schedule is defined in the ``backup`` section of the
`deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__
file. This section contains following subsections:

* ``storages`` subsection contains data needed to access the S3-compatible cloud
  to store backups.
* ``schedule`` subsection allows to actually schedule backups (the schedule is
  specified in crontab format).

Here is an example of `deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__ which uses Amazon S3 storage for backups:

.. code:: yaml

   ...
   backup:
     ...
     schedule:
      - name: "sat-night-backup"
        schedule: "0 0 * * 6"
        keep: 3
        type: full
        storage: s3
     ...

The schedule is specified in crontab format as explained in
:ref:`Custom Resource options<backup-schedule-schedule>`.

.. _backups-manual:

Making on-demand backup
-----------------------

To make an on-demand backup, the user should use a backup configuration file.
The example of the backup configuration file is `deploy/backup/backup.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/backup/backup.yaml>`_.

The following keys are most important in the parameters section of this file:

* ``parameters.backrest-opts`` is the string with command line options which
  will be passed to pgBackRest, for example
  ``--type=full --repo1-retention-full=5``,
* ``parameters.pg-cluster`` is the name of the PostgreSQL cluster to back up,
  for example ``cluster1``.

When the backup options are configured, execute the actual backup command:

.. code:: bash

   $ kubectl apply -f deploy/backup/backup.yaml

.. _backups-list:

List existing backups
--------------------------------------------------

To get list of all existing backups in the pgBackrest repo, use the following
command:

.. code:: bash

   $ kubectl exec <name-of-backrest-shared-repo-pod>  -it -- pgbackrest info

You can find out the appropriate Pod name using the `` kubectl get pods``
command, as usual. Here is an example of the backups list:

.. code:: bash

   $ kubectl exec cluster1-backrest-shared-repo-5ffc465b85-gvhlh -it -- pgbackrest info
   stanza: db
       status: ok
       cipher: none

       db (current)
           wal archive min/max (14): 000000010000000000000001/000000010000000000000003

           full backup: 20220614-104859F
               timestamp start/stop: 2022-06-14 10:48:59 / 2022-06-14 10:49:13
               wal start/stop: 000000010000000000000002 / 000000010000000000000002
               database size: 33.5MB, database backup size: 33.5MB
               repo1: backup set size: 4.3MB, backup size: 4.3MB

In this example there is only one backup named ``20220614-104859F``.

.. _backups-restore:

Restore the cluster from a previously saved backup
--------------------------------------------------

The Operator supports the ability to perform a full restore on a PostgreSQL
cluster as well as a point-in-time-recovery. There are two types of ways to
restore a cluster:

* restore to a new cluster using the :ref:`pgDataSource.restoreFrom<pgdatasource-restorefrom>`
  option (and possibly, :ref:`pgDataSource.restoreOpts<pgdatasource-restoreopts>`
  for custom pgBackRest options),
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
`deploy/backup/restore.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/backup/restore.yaml>`_:

.. code:: bash

   apiVersion: pg.percona.com/v1
   kind: Pgtask
   metadata:
     labels:
       pg-cluster: cluster1
       pgouser: admin
     name: cluster1-backrest-restore
     namespace: pgo
   spec:
     name: cluster1-backrest-restore
     namespace: pgo
     parameters:
       backrest-restore-from-cluster: cluster1
       backrest-restore-opts: --type=time --target="2021-04-16 15:13:32"
       backrest-storage-type: local
     tasktype: restore

The following keys are the most important in the parameters section of this file:

* ``parameters.backrest-restore-cluster`` specifies the name of a
  PostgreSQL cluster which will be restored (this option had name
  ``parameters.backrest-restore-from-cluster`` before the Operator 1.2.0).
  It includes stopping the database and recreating a new primary with the
  restored data (for example, ``cluster1``),
* ``parameters.backrest-restore-opts`` passes through additional options for
  pgBackRest,
* ``parameters.backrest-storage-type`` the type of the pgBackRest repository,
  (for example, ``local``).

The actual restoration process can be started as follows:

.. code:: bash

   $ kubectl apply -f deploy/backup/restore.yaml

.. seealso:: :ref:`faq-skip-tls`

To create a new PostgreSQL cluster from either the active  one, or a former cluster
whose pgBackRest repository still exists,  use the :ref:`pgDataSource.restoreFrom<pgdatasource-restorefrom>` 
option. 

The following example will create a new cluster named ``cluster2`` from an
existing one named``cluster1``.

#. First, create the ``cluster2-config-secrets.yaml`` configuration file with
   the following content:

   .. code:: yaml

      apiVersion: v1
      data:
        password: <base64-encoded-password-for-pguser->
        username: <base64-encoded-pguser-user-name>
      kind: Secret
      metadata:
        labels:
          pg-cluster: cluster2
          vendor: crunchydata
        name: cluster2-pguser-secret
      type: Opaque
      ---
      apiVersion: v1
      data:
        password: <base64-encoded-password-for-primaryuser>
        username: <base64-encoded-primaryuser-user-name>
      kind: Secret
      metadata:
        labels:
          pg-cluster: cluster2
          vendor: crunchydata
        name: cluster2-primaryuser-secret
      type: Opaque
      ---
      apiVersion: v1
      data:
        password: <base64-encoded-password-for-postgres-user>
        username: <base64-encoded-pguser-postgres-name>
      kind: Secret
      metadata:
        labels:
          pg-cluster: cluster2
          vendor: crunchydata
        name: cluster2-postgres-secret
      type: Opaque

#. When done, create the secrets as follows:

   .. code:: bash

      $ kubectl apply -f ./cluster2-config-secrets.yaml

#. Edit the ``deploy/cr.yaml`` configuration file:

   * set a new cluster name (``cluster2``),
   * set the option :ref:`pgDataSource.restoreFrom<pgdatasource-restorefrom>` to
     ``cluster1``.

#. Create the cluster as follows:

   .. code:: bash

      $ kubectl apply -f deploy/cr.yaml

.. _backups-restore-pitr:

Restore the cluster with point-in-time recovery
-------------------------------------------------

Point-in-time recovery functionality allows users to revert the database back to
a state before an unwanted change had occurred.

You can set up a point-in-time recovery using the normal restore command of
pgBackRest with few additional options specified in the
``parameters.backrest-restore-opts`` key in the `backup restore configuration file <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/backup/restore.yaml>`_:

.. code:: yaml

   ...
   spec:
     name: cluster1-backrest-restore
     namespace: pgo
     parameters:
       backrest-restore-from-cluster: cluster1
       backrest-restore-opts: --type=time --target="2021-04-16 15:13:32-04"

* set ``--type`` option to ``time``,
* set ``--target`` to a specific time you would like to restore to. You can use
  the typical string formatted as ``<YYYY-MM-DD HH:MM:DD>``, optionally followed
  by a timezone offset: ``"2021-04-16 15:13:32-04"`` (``-04`` here means Eastern
  Daylight Time Zone, or EDT),
* optional ``--set`` argument allows you to choose the backup which will be the
  starting point for point-in-time recovery (:ref:`look through the available backups<backups-list>`
  to find out the proper backup name). This option must be specified if the target is
  one or more backups away from the current moment.

After setting these options in the *backup restore* configuration file,
follow the :ref:`standard restore instructions<backups-restore>`.

.. note:: Make sure you have a backup that is older than your desired point in
   time. You obviously can’t restore from a time where you do not have a backup.
   All relevant write-ahead log files must be successfully pushed before you
   make the restore.

.. _backups-delete:

Delete a previously saved backup
--------------------------------------------------

The maximum amount of stored backups is controlled by the
:ref:`backup.schedule.keep<backup-schedule-keep>` option (only successful
backups are counted). Older backups are automatically deleted, so that amount of
stored backups do not exceed this number.

If you want to delete some backup manually, you need to delete both the
``pgtask`` object and the corresponding job itself. Deletion of the backup
object can be done using the same YAML file which was used for the on-demand
backup:  

.. code:: bash

   $ kubectl delete -f deploy/backup/backup.yaml

Deletion of the job which corresponds to the backup can be done using
``kubectl delete jobs`` command with the backup name:

.. code:: bash

   $ kubectl delete jobs cluster1-backrest-full-backup
