.. _operator.custom-resource-options:

`Custom Resource options <operator.html#operator-custom-resource-options>`_
===============================================================================

The Operator is configured via the spec section of the
`deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`_ file.

The metadata part of this file contains the following keys:

* ``name`` (``cluster1`` by default) sets the name of your Percona Distribution
  for PostgreSQL Cluster; it should include only `URL-compatible characters <https://datatracker.ietf.org/doc/html/rfc3986#section-2.3>`_, not exceed 22 characters, start with an alphabetic character, and end with an alphanumeric character;

The spec part of the `deploy/cr.yaml <https://github.com/percona/percona-server-mongodb-operator/blob/main/deploy/cr.yaml>`_ file contains the following sections:

.. list-table::
   :widths: 15 15 16 54
   :header-rows: 1

   * - Key
     - Value type
     - Default
     - Description

   * - pause
     - boolean
     - ``false``
     - Pause/resume: setting it to ``true`` gracefully stops the cluster, and
       setting it to ``false`` after shut down starts the cluster back.

   * - pgPrimary
     - :ref:`subdoc<operator-pgprimary-section>`
     -
     - PostgreSQL primary instance section

   * - walStorage
     - :ref:`subdoc<operator-walstorage-section>`
     -
     - Write-ahead Log Storage Section

   * - pmm
     - :ref:`subdoc<operator-pmm-section>`
     - 
     - Percona Monitoring and Management section

   * - backup
     - :ref:`subdoc<operator=backup-section>`
     - 
     - Percona Server for MongoDB backups section

   * - pgBouncer
     - :ref:`subdoc<operator-pgprimary-section>`
     -
     - The `pgBouncer <http://pgbouncer.github.io/>`__ connection pooler section

   * - pgReplicas
     - :ref:`subdoc<operator-pgreplicas-section>`
     -
     - Section required to manage the replicas within a PostgreSQL cluster

   * - pgBadger
     - :ref:`subdoc<operator-pgbadger-section>`
     -
     - The `pgBadger <https://github.com/darold/pgbadger>`__ PostgreSQL log analyzer section

.. tabularcolumns:: |p{2cm}|p{13.6cm}|

+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-database:                                                                        |
|                 |                                                                                           |
| **Key**         | `database <operator.html#spec-database>`_                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``pgdb``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The name of a database that the PostgreSQL user can log into after the PostgreSQL cluster |
|                 | is created                                                                                |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-disableautofail:                                                                 |
|                 |                                                                                           |
| **Key**         | `disableAutofail <operator.html#spec-disableautofail>`_                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | boolean                                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``false``                                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Turns high availability on or off. By default, every cluster can have high availability   |
|                 | if there is at least one replica                                                          |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-name:                                                                            |
|                 |                                                                                           |
| **Key**         | `name <operator.html#spec-name>`_                                                         |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``cluster1``                                                                              |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The name of the PostgreSQL instance that is the primary; on creation, this should be set  |
|                 | to be the same as ``clustername``                                                         |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-image:                                                                         |
|                 |                                                                                           |
| **Key**         | `pgPrimary.image <operator.html#pgprimary-image>`_                                                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``perconalab/percona-postgresql-operator:main-ppg13-postgres-ha``                         |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The Docker image of the                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-volumespec-size:                                                                  |
|                 |                                                                                           |
| **Key**         | `pgPrimary.volumeSpec.size <operator.html#pgprimary-volumespec-size>`_                                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``1G``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes PersistentVolumeClaim                                                     |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#                         |
|                 | persistentvolumeclaims>`_ size for the PostgreSQL cluster primary storage                 |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-volumespec-accessmode:                                                            |
|                 |                                                                                           |
| **Key**         | `pgPrimary.volumeSpec.accessmode <operator.html#pgprimary-volumespec-accessmode>`_                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``ReadWriteOnce``                                                                         |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes PersistentVolumeClaim                                                     |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/                          |
|                 | #persistentvolumeclaims>`_ access modes for the PostgreSQL cluster primary storage        |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-volumespec-storagetype:                                                           |
|                 |                                                                                           |
| **Key**         | `pgPrimary.volumeSpec.storagetype <operator.html#pgprimary-volumespec-storagetype>`_                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``dynamic``                                                                               |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Type of the PostgreSQL cluster primary storage: ``create`` (by default) or ``dynamic``    |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-volumespec-storageclass:                                                          |
|                 |                                                                                           |
| **Key**         | `pgPrimary.volumeSpec.storageclass <operator.html#pgprimary-volumespec-storageclass>`_                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Optionally sets the `Kubernetes storage class                                             |
|                 | <https://kubernetes.io/docs/concepts/storage/storage-classes/>`_ to use with the          |
|                 | PostgreSQL cluster primary storage `PersistentVolumeClaim                                 |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims>`_|
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-volumespec-matchlabels:                                                           |
|                 |                                                                                           |
| **Key**         | `pgPrimary.volumeSpec.matchLabels <operator.html#pgprimary-volumespec-matchlabels>`_                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Labels are key-value pairs attached to objects                                           |
|                 | <https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/>`_             |
+-----------------+-------------------------------------------------------------------------------------------+


.. _operator.walstorage-section:

`Write-ahead Log Storage Section <operator.html#operator-walstorage-section>`_
--------------------------------------------------------------------------------

The ``walStorage`` section in the `deploy/cr.yaml <https://github.com/percona/percona-xtradb-cluster-operator/blob/main/deploy/cr.yaml>`__
file contains configuration options for PostgreSQL `write-ahead logging <https://www.postgresql.org/docs/current/wal-intro.html>`_.

.. tabularcolumns:: |p{2cm}|p{13.6cm}|

+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _walstorage-volumespec-size:                                                                      |
|                 |                                                                                           |
| **Key**         | `walStorage.volumeSpec.size <operator.html#walstorage-volumespec-size>`_                                        |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``1G``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes PersistentVolumeClaim                                                     |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#                         |
|                 | persistentvolumeclaims>`_ size for the PostgreSQL write-ahead log storage                 |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _walstorage-volumespec-accessmode:                                                                |
|                 |                                                                                           |
| **Key**         | `walStorage.volumeSpec.accessmode <operator.html#walstorage-volumespec-accessmode>`_                            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``ReadWriteOnce``                                                                         |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes PersistentVolumeClaim                                                     |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/                          |
|                 | #persistentvolumeclaims>`_ access modes for the PostgreSQL write-ahead log storage        |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _walstorage-storagetype:                                                               |
|                 |                                                                                           |
| **Key**         | `walStorage.volumeSpec.storagetype <operator.html#walstorage-storagetype>`_                          |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``dynamic``                                                                               |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Type of the PostgreSQL write-ahead log storage: ``create`` (by default) or ``dynamic``    |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _walstorage-volumespec-storageclass:                                                              |
|                 |                                                                                           |
| **Key**         | `walStorage.volumeSpec.storageclass <operator.html#walstorage-storageclass>`_                        |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Optionally sets the `Kubernetes storage class                                             |
|                 | <https://kubernetes.io/docs/concepts/storage/storage-classes/>`_ to use with the          |
|                 | PostgreSQL write-ahead log storage `PersistentVolumeClaim                                 |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims>`_|
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _walstorage-volumespec-matchlabels:                                                               |
|                 |                                                                                           |
| **Key**         | `walStorage.volumeSpec.matchLabels <operator.html#walstorage-volumespec-matchlabels>`_                          |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Labels are key-value pairs attached to objects                                           |
|                 | <https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/>`_             |
+-----------------+-------------------------------------------------------------------------------------------+



.. _operator.backup-section:

`Backup Section <operator.html#operator-backup-section>`_
--------------------------------------------------------------------------------

The ``backup`` section in the
`deploy/cr.yaml <https://github.com/percona/percona-xtradb-cluster-operator/blob/main/deploy/cr.yaml>`__
file contains the following configuration options for the regular
Percona Distribution for PostgreSQL backups.

.. tabularcolumns:: |p{2cm}|p{13.6cm}|

+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-image:                                                                   |
|                 |                                                                                           |
| **Key**         | `backup.image <operator.html#backup-backrestimage>`_                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``perconalab/percona-postgresql-operator:main-ppg13-pgbackrest``                          |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The Docker image of the                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-backrestrepoimage:                                                               |
|                 |                                                                                           |
| **Key**         | `backup.backrestRepoImage <operator.html#backup-backrestrepoimage>`_                               |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``perconalab/percona-postgresql-operator:main-ppg13-pgbackrest-repo``                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The Docker image of the                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-resources-requests-memory:                                                        |
|                 |                                                                                           |
| **Key**         | `backup.resources.requests.memory <operator.html#backup-resources-requests-memory>`_                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``48Mi``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes memory requests                                                           |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_                                     |
|                 | for a pgBackRest container                                                                |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-resources-limits-cpu:                                                                  |
|                 |                                                                                           |
| **Key**         | `backup.resources.limits.cpu <operator.html#backup-resources-limits-cpu>`_                                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``1``                                                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Kubernetes CPU limits                                                                    |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_ for a pgBackRest container          |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-resources-limits-memory:                                                       |
|                 |                                                                                           |
| **Key**         | `backup.resources.limits.memory <operator.html#backup-resources-limits-memory>`_                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``64Mi``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes memory limits                                                             |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_                                     |
|                 | for a pgBackRest container                                                                |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-volumespec-size:                                                                 |
|                 |                                                                                           |
| **Key**         | `backup.volumeSpec.size <operator.html#backup-volumespec-size>`_                              |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``1G``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes PersistentVolumeClaim                                                     |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#                         |
|                 | persistentvolumeclaims>`_ size for the pgBackRest Storage                                 |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-volumespec-accessmode:                                                           |
|                 |                                                                                           |
| **Key**         | `backup.volumeSpec.accessmode <operator.html#backup-volumespec-accessmode>`_                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``ReadWriteOnce``                                                                         |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes PersistentVolumeClaim                                                     |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/                          |
|                 | #persistentvolumeclaims>`_ access modes for the pgBackRest Storage                        |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-volumespec-storagetype:                                                          |
|                 |                                                                                           |
| **Key**         | `backup.volumeSpec.storagetype <operator.html#backup-volumespec-storagetype>`_                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``dynamic``                                                                               |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Type of the pgBackRest Storage: ``create`` (by default) or ``dynamic``                    |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-volumespec-storageclass:                                                         |
|                 |                                                                                           |
| **Key**         | `backup.volumeSpec.storageclass <operator.html#backup-volumespec-storageclass>`_              |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Optionally sets the `Kubernetes storage class                                             |
|                 | <https://kubernetes.io/docs/concepts/storage/storage-classes/>`_ to use with the          |
|                 | pgBackRest Storage `PersistentVolumeClaim                                                 |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims>`_|
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-volumespec-matchlabels:                                                          |
|                 |                                                                                           |
| **Key**         | `backup.volumeSpec.matchLabels <operator.html#backup-volumespec-matchlabels>`_                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Labels are key-value pairs attached to objects                                           |
|                 | <https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/>`_             |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-storages-type:                                                                 |
|                 |                                                                                           |
| **Key**         | `backup.storages.<storage-name>.type <operator.html#backup-storages-type>`_               |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``s3``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The cloud storage type used for backups. Only ``s3`` type is supported                    |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-storages-s3-endpointurl:                                                              |
|                 |                                                                                           |
| **Key**         | `backup.storages.s3.<storage-name>.endpointURL <operator.html#backup-storages-s3-endpointurl>`_                             |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``minio-gateway-svc:9000``                                                                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The endpoint URL of the S3-compatible storage to be used for backups (not needed for the  |
|                 | original Amazon S3 cloud)                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-storages-s3-region:                                                                |
|                 |                                                                                           |
| **Key**         | `backup.storages.s3.<storage-name>.region <operator.html#backup-storages-s3-region>`_                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | boolean                                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``us-east-1``                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `AWS region <https://docs.aws.amazon.com/general/latest/gr/rande.html>`_ to use for   |
|                 | Amazon and all S3-compatible storages                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-storages-s3-uristyle:                                                              |
|                 |                                                                                           |
| **Key**         | `backup.storages.s3.<storage-name>.uriStyle <operator.html#backup-storages-s3-uristyle>`_                             |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``path``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Optional parameter that specifies if pgBackRest should use the path or host S3 URI style  |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-storages-s3-verifytls:                                                             |
|                 |                                                                                           |
| **Key**         | `backup.storages.s3.<storage-name>.verifyTLS <operator.html#backup-storages-s3-verifytls>`_                           |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | boolean                                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``false``                                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Enables or disables TLS verification for pgBackRest                                       |
+-----------------+-------------------------------------------------------------------------------------------+









+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _replicastorage-accessmode:                                                            |
|                 |                                                                                           |
| **Key**         | `ReplicaStorage.accessmode <operator.html#replicastorage-accessmode>`_                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``ReadWriteOnce``                                                                         |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes PersistentVolumeClaim                                                     |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/                          |
|                 | #persistentvolumeclaims>`_ access modes for the PostgreSQL Replica storage                |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _replicastorage-matchlabels:                                                           |
|                 |                                                                                           |
| **Key**         | `ReplicaStorage.matchLabels <operator.html#replicastorage-matchlabels>`_                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Labels are key-value pairs attached to objects                                           |
|                 | <https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/>`_             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _replicastorage-name:                                                                  |
|                 |                                                                                           |
| **Key**         | `ReplicaStorage.name <operator.html#replicastorage-name>`_                                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The PostgreSQL Replica storage name                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _replicastorage-size:                                                                  |
|                 |                                                                                           |
| **Key**         | `ReplicaStorage.size <operator.html#replicastorage-size>`_                                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``1G``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes PersistentVolumeClaim                                                     |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#                         |
|                 | persistentvolumeclaims>`_ size for the PostgreSQL Replica storage                         |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _replicastorage-storageclass:                                                          |
|                 |                                                                                           |
| **Key**         | `ReplicaStorage.storageclass <operator.html#replicastorage-storageclass>`_                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Optionally sets the `Kubernetes storage class                                             |
|                 | <https://kubernetes.io/docs/concepts/storage/storage-classes/>`_ to use with the          |
|                 | PostgreSQL Replica storage `PersistentVolumeClaim                                         |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims>`_|
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _replicastorage-storagetype:                                                           |
|                 |                                                                                           |
| **Key**         | `ReplicaStorage.storagetype <operator.html#replicastorage-storagetype>`_                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``dynamic``                                                                               |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Type of the PostgreSQL Replica storage: ``create`` (by default) or ``dynamic``            |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _replicastorage-supplementalgroups:                                                    |
|                 |                                                                                           |
| **Key**         | `ReplicaStorage.supplementalgroups <operator.html#replicastorage-supplementalgroups>`_    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Supplemental groups for the PostgreSQL Replica storage                                    |
+-----------------+-------------------------------------------------------------------------------------------+

.. _operator.pmm-section:

`PMM Section <operator.html#operator-pmm-section>`_
--------------------------------------------------------------------------------

The ``pmm`` section in the `deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__
file contains configuration options for Percona Monitoring and Management.

.. tabularcolumns:: |p{2cm}|p{13.6cm}|

+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pmm-enabled:                                                                          |
|                 |                                                                                           |
| **Key**         | `pmm.enabled <operator.html#pmm-enabled>`_                                                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | boolean                                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``false``                                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Enables or disables `monitoring Percona Distribution for PostgreSQL cluster with PMM      |
|                 | <https://www.percona.com/doc/percona-monitoring-and-management/2.x/setting-up/            |
|                 | client/postgresql.html>`_                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pmm-image:                                                                            |
|                 |                                                                                           |
| **Key**         | `pmm.image <operator.html#pmm-image>`_                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``percona/pmm-client:{{{pmm2recommended}}}``                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | PMM client Docker image to use                                                            |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pmm-serverhost:                                                                       |
|                 |                                                                                           |
| **Key**         | `pmm.serverHost <operator.html#pmm-serverhost>`_                                          |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       |  string                                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     |  ``monitoring-service``                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Address of the PMM Server to collect data from the cluster                                |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pmm-serveruser:                                                                       |
|                 |                                                                                           |
| **Key**         | `pmm.serverUser <operator.html#pmm-serveruser>`_                                          |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``admin``                                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `PMM Server User                                                                      |
|                 | <https://www.percona.com/doc/percona-monitoring-and-management/glossary.option.html>`_.   |
|                 | The PMM Server password should be configured using Secrets                                |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pmm-pmmsecret:                                                                        |
|                 |                                                                                           |
| **Key**         | `pmm.pmmSecret <operator.html#pmm-pmmsecret>`_                                            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``cluster1-pmm-secret``                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Name of the `Kubernetes Secret object                                                     |
|                 | <https://kubernetes.io/docs/concepts/configuration/secret/#using-imagepullsecrets>`_ for  |
|                 | the PMM Server password                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pmm-resources-requests-memory:                                                                 |
|                 |                                                                                           |
| **Key**         | `pmm.resources.requests.memory <operator.html#pmm-resources-requests-memory>`_                              |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``200M``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes memory requests                                                           |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_                                     |
|                 | for a PMM container                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pmm-resources-requests-cpu:                                                                    |
|                 |                                                                                           |
| **Key**         | `pmm.resources.requests.cpu <operator.html#pmm-resources-requests-cpu>`_                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``500m``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Kubernetes CPU requests                                                                  |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_ for a PMM container                 |
+-----------------+-------------------------------------------------------------------------------------------+

.. _operator.pgbouncer-section:

`pgBouncer Section <operator.html#operator-pgbouncer-section>`_
--------------------------------------------------------------------------------

The ``pgBouncer`` section in the `deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__
file contains configuration options for the `pgBouncer <http://pgbouncer.github.io/>`__ connection pooler for PostgreSQL.

.. tabularcolumns:: |p{2cm}|p{13.6cm}|

+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbouncer-image:                                                                            |
|                 |                                                                                           |
| **Key**         | `pgBouncer.image <operator.html#pgbouncer-image>`_                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``perconalab/percona-postgresql-operator:main-ppg13-pgbouncer``                           |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | pgBouncer Docker image to use                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbouncer-size:                                                                  |
|                 |                                                                                           |
| **Key**         | `pgBouncer.size <operator.html#pgbouncer-size>`_                                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``1G``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The number of the pgBouncer Pods `to provide connection pooling                           |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbouncer-resources-requests-cpu:                                                                  |
|                 |                                                                                           |
| **Key**         | `pgBouncer.resources.requests.cpu <operator.html#pgbouncer-resources-requests-cpu>`_      |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``1``                                                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Kubernetes CPU requests                                                                  |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_ for a pgBouncer container           |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbouncer-resources-requests-memory:                                                        |
|                 |                                                                                           |
| **Key**         | `pgBouncer.resources.requests.memory <operator.html#pgbouncer-resources-requests-memory>`_|
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``128Mi``                                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes memory requests                                                           |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_                                     |
|                 | for a pgBouncer container                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbouncer-resources-limits-cpu:                                                       |
|                 |                                                                                           |
| **Key**         | `pgBouncer.resources.limits.cpu <operator.html#pgbouncer-resources-limits-cpu>`_                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``2``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Kubernetes CPU limits                                                                    |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_ for a pgBouncer container           |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbouncer-resources-limits-memory:                                                       |
|                 |                                                                                           |
| **Key**         | `pgBouncer.resources.limits.memory <operator.html#pgbouncer-resources-limits-memory>`_                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``512Mi``                                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes memory limits                                                             |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_                                     |
|                 | for a pgBouncer container                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbouncer-expose-servicetype:                                                                 |
|                 |                                                                                           |
| **Key**         | `pgBouncer.expose.serviceType <operator.html#pgbouncer-expose-servicetype>`_                              |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``ClusterIP``                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Specifies the type of `Kubernetes Service                                                 |
|                 | <https://kubernetes.io/docs/concepts/services-networking/service/                         |
|                 | #publishing-services-service-types>`_ for pgBouncer                                       |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbouncer-expose-loadbalancersourceranges:                                                    |
|                 |                                                                                           |
| **Key**         | `pgBouncer.expose.loadBalancerSourceRanges <operator.html#pgbouncer-expose-loadbalancersourceranges>`_    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The range of client IP addresses from which the load balancer should be reachable         |
|                 | (if not set, there is no limitations)                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbouncer-expose-annotations:                                                                 |
|                 |                                                                                           |
| **Key**         | `pgBouncer.expose.annotations <operator.html#pgbouncer-expose-annotations>`_                              |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | label                                                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``pg-cluster-annot: cluster1``                                                            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes annotations                                                               |
|                 | <https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/>`_        |
|                 | metadata for pgBouncer                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbouncer-expose-labels:                                                                      |
|                 |                                                                                           |
| **Key**         | `pgBouncer.expose.labels <operator.html#pgbouncer-expose-labels>`_                                        |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | label                                                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``pg-cluster-label: cluster1``                                                            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Labels are key-value pairs attached to objects                                           |
|                 | <https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/>`_             |
+-----------------+-------------------------------------------------------------------------------------------+

.. _operator.pgbadger-section:

`pgBadger Section <operator.html#operator-pgbadger-section>`_
--------------------------------------------------------------------------------

The ``pgBadger`` section in the `deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__
file contains configuration options for the `pgBadger PostgreSQL log analyzer <https://github.com/darold/pgbadger>`__.

.. tabularcolumns:: |p{2cm}|p{13.6cm}|

+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbadger-enabled:                                                                          |
|                 |                                                                                           |
| **Key**         | `pgBadger.enabled <operator.html#pgbadger-enabled>`_                                                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | boolean                                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``false``                                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Enables or disables the                                                                   |
|                 | `pgBadger PostgreSQL log analyzer <https://github.com/darold/pgbadger>`__                 |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbadger-image:                                                                            |
|                 |                                                                                           |
| **Key**         | `pgBadger.image <operator.html#pgbadger-image>`_                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``perconalab/percona-postgresql-operator:main-ppg13-pgbadger``                            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | pgBadger Docker image to use                                                            |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbadger-serverhost:                                                                       |
|                 |                                                                                           |
| **Key**         | `pgBadger.port <operator.html#pgbadger-port>`_                                          |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     |  ``10000``                                                                                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The port number for pgBadger                                                              |
+-----------------+-------------------------------------------------------------------------------------------+





|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-shutdown:                                                                        |
|                 |                                                                                           |
| **Key**         | `shutdown <operator.html#spec-shutdown>`_                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | boolean                                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``false``                                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Pause/resume: setting it to ``true`` gracefully stops the cluster, and setting it to      |
|                 | ``false`` after shut down starts the cluster                                              |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-standby:                                                                         |
|                 |                                                                                           |
| **Key**         | `standby <operator.html#spec-standby>`_                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | boolean                                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``false``                                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | If ``true``, indicates that the PostgreSQL cluster is a *standby* cluster, i.e. it is in  |
|                 | read-only mode                                                                            |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-pgprimary-name:                                                                  |
|                 |                                                                                           |
| **Key**         | `pgPrimary.name <operator.html#spec-pgprimary-name>`_                                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``cluster1``                                                                              |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The PostgreSQL cluster primary storage name                                               |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-pgprimary-supplementalgroups:                                                    |
|                 |                                                                                           |
| **Key**         | `pgPrimary.supplementalgroups <operator.html#spec-pgprimary-supplementalgroups>`_    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Supplemental groups for the PostgreSQL cluster primary storage                            |
+-----------------+-------------------------------------------------------------------------------------------+

|                 | .. _walstorage-name:                                                                      |
|                 |                                                                                           |
| **Key**         | `walStorage.volumeSpec.name <operator.html#walstorage-name>`_                                        |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The PostgreSQL write-ahead log storage name                                               |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _walstorage-supplementalgroups:                                                        |
|                 |                                                                                           |
| **Key**         | `walStorage.volumeSpec.supplementalgroups <operator.html#walstorage-supplementalgroups>`_            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Supplemental groups for the PostgreSQL write-ahead log storage                            |
+-----------------+-------------------------------------------------------------------------------------------+




|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-customconfig:                                                                    |
|                 |                                                                                           |
| **Key**         | `customconfig <operator.html#spec-customconfig>`_                                         |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Custom ConfigMap to use when bootstrapping a PostgreSQL cluster                           |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-nodeaffinity-default:                                                            |
|                 |                                                                                           |
| **Key**         | `nodeAffinity.advanced <operator.html#spec-nodeaffinity-default>`_                        |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | subdoc                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``null``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `The Kubernetes Pod Affinity                                                              |
|                 | <https://kubernetes.io/docs/concepts/configuration/assign-pod-node/                       |
|                 | #affinity-and-anti-affinity>`_ constraint                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
|                 |                                                                                           |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-backrestconfig:                                                                  |
|                 |                                                                                           |
| **Key**         | `backrestConfig <operator.html#spec-backrestconfig>`_                                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | subdoc                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``null``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Optional references to pgBackRest configuration files                                     |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-backrestrepopath:                                                                |
|                 |                                                                                           |
| **Key**         | `backrestRepoPath <operator.html#spec-backrestrepopath>`_                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Optional reference to the location of the pgBackRest repository                           |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-backrests3bucket:                                                                |
|                 |                                                                                           |
| **Key**         | `backrestS3Bucket <operator.html#spec-backrests3bucket>`_                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Amazon S3 bucket <https://docs.aws.amazon.com/AmazonS3/latest/dev/UsingBucket.html>`_|
|                 | name for backups                                                                          |
+-----------------+-------------------------------------------------------------------------------------------+
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backreststorage-name:                                                                 |
|                 |                                                                                           |
| **Key**         | `BackrestStorage.name <operator.html#backreststorage-name>`_                              |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The pgBackRest Storage name                                                               |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backreststorage-supplementalgroups:                                                   |
|                 |                                                                                           |
| **Key**         | `BackrestStorage.supplementalgroups <operator.html#backreststorage-supplementalgroups>`_  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Supplemental groups for the pgBackRest Storage                                            |
+-----------------+-------------------------------------------------------------------------------------------+

