.. _operator.custom-resource-options:

`Custom Resource options <operator.html#operator-custom-resource-options>`_
===============================================================================

The Cluster is configured via the
`deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__ file.

The metadata part of this file contains the following keys:

* ``name`` (``cluster1`` by default) sets the name of your Percona Distribution
  for PostgreSQL Cluster; it should include only `URL-compatible characters <https://datatracker.ietf.org/doc/html/rfc3986#section-2.3>`_, not exceed 22 characters, start with an alphabetic character, and end with an alphanumeric character;

The spec part of the `deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__ file contains the following sections:

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

   * - upgradeOptions
     - :ref:`subdoc<operator.upgradeoptions-section>`
     -
     - Percona Distribution for PostgreSQL upgrade options section

   * - pgPrimary
     - :ref:`subdoc<operator.pgprimary-section>`
     -
     - PostgreSQL Primary instance options section

   * - walStorage
     - :ref:`subdoc<operator-walstorage-section>`
     -
     - Write-ahead Log Storage Section

   * - pmm
     - :ref:`subdoc<operator-pmm-section>`
     - 
     - Percona Monitoring and Management section

   * - backup
     - :ref:`subdoc<operator-backup-section>`
     - 
     - Section to configure backups and pgBackRest

   * - pgBouncer
     - :ref:`subdoc<operator-pgbouncer-section>`
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

..
   * - port: "5432"
   * - user: pguser
   * - standby: false

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
|                 | .. _spec-tlsonly:                                                                         |
|                 |                                                                                           |
| **Key**         | `tlsOnly <operator.html#spec-tlsonly>`_                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | boolean                                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``false``                                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Enforece Operator to use only Transport Layer Security (TLS) for both internal and        |
|                 | external communications                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-sslca:                                                                           |
|                 |                                                                                           |
| **Key**         | `sslCA <operator.html#spec-sslca>`_                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``cluster1-ssl-ca``                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The name of the secret with TLS :abbr:`CA (Certificate authority)` used for both          |
|                 | connection encryption (external traffic), and replication (internal traffic)              |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-sslsecretname:                                                                   |
|                 |                                                                                           |
| **Key**         | `sslSecretName <operator.html#spec-sslsecretname>`_                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``cluster1-ssl-keypair``                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The name of the secret created to encrypt external communications                         |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-sslreplicationsecretname:                                                        |
|                 |                                                                                           |
| **Key**         | `sslReplicationSecretName <operator.html#spec-sslreplicationsecretname>`_                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``cluster1-ssl-keypair"``                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The name of the secret created to encrypt internal communications                         |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-keepdata:                                                                        |
|                 |                                                                                           |
| **Key**         | `keepData <operator.html#spec-keepdata>`_                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | boolean                                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``true``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | If ``true``, PVCs will be kept after the cluster deletion                                 |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _spec-keepbackups:                                                                     |
|                 |                                                                                           |
| **Key**         | `keepBackups <operator.html#spec-keepbackups>`_                                           |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | boolean                                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``true``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | If ``true``, local backups will be kept after the cluster deletion                        |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgdatasource-restorefrom:                                                             |
|                 |                                                                                           |
| **Key**         | `pgDataSource.restoreFrom <operator.html#pgdatasource-restorefrom>`_                      |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The name of a data source PostgreSQL cluster, which is used                               |
|                 | to :ref:`restore backup to a a new cluster<backups-restore>`                              |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgdatasource-restoreopts:                                                             |
|                 |                                                                                           |
| **Key**         | `pgDataSource.restoreOpts <operator.html#pgdatasource-restoreopts>`_                      |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Custom pgBackRest options to :ref:`restore backup to a a new cluster<backups-restore>`    |
+-----------------+-------------------------------------------------------------------------------------------+

.. _operator.upgradeoptions-section:

`Upgrade Options Section <operator.html#operator-upgradeoptions-section>`_
--------------------------------------------------------------------------------

The ``upgradeOptions`` section in the `deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__ file contains various configuration options to control Percona Distribution for PostgreSQL upgrades.

.. tabularcolumns:: |p{2cm}|p{13.6cm}|

+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _upgradeoptions-versionserviceendpoint:                                                |
|                 |                                                                                           |
| **Key**         | `upgradeOptions.versionServiceEndpoint                                                    |
|                 | <operator.html#upgradeoptions-versionserviceendpoint>`_                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``https://check.percona.com``                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The Version Service URL used to check versions compatibility for upgrade                  |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _upgradeoptions-apply:                                                                 |
|                 |                                                                                           |
| **Key**         | `upgradeOptions.apply <operator.html#upgradeoptions-apply>`_                              |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``14-recommended``                                                                        |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Specifies how :ref:`updates are processed<operator-update-smartupdates>` by the Operator. |
|                 | ``Never`` or ``Disabled`` will completely disable automatic upgrades, otherwise it can be |
|                 | set to ``Latest`` or ``Recommended`` or to a specific version number of Percona           |
|                 | Distribution for PostgreSQL to have it version-locked (so that the user can               |
|                 | control the version running, but use automatic upgrades to move between them).            |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _upgradeoptions-schedule:                                                              |
|                 |                                                                                           |
| **Key**         | `upgradeOptions.schedule <operator.html#upgradeoptions-schedule>`_                        |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``0 2 * * *``                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Scheduled time to check for updates, specified in the                                     |
|                 | `crontab format <https://en.wikipedia.org/wiki/Cron>`_                                    |
+-----------------+-------------------------------------------------------------------------------------------+


.. _operator.pgprimary-section:

`pgPrimary Section <operator.html#operator-pgprimary-section>`_
--------------------------------------------------------------

The pgPrimary section controls the PostgreSQL Primary instance.

.. tabularcolumns:: |p{2cm}|p{13.6cm}|

+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-image:                                                                      |
|                 |                                                                                           |
| **Key**         | `pgPrimary.image <operator.html#pgprimary-image>`_                                        |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``perconalab/percona-postgresql-operator:main-ppg13-postgres-ha``                         |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The Docker image of the PostgreSQL Primary instance                                       |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-imagepullpolicy:                                                            |
|                 |                                                                                           |
| **Key**         | `pgPrimary.imagePullPolicy <operator.html#pgprimary-imagepullpolicy>`_                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``Always``                                                                                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | This option is used to set the `policy                                                    |
|                 | <https://kubernetes.io/docs/concepts/containers/images/#updating-images>`_ for updating   |
|                 | pgPrimary and pgReplicas images                                                           |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-resources-requests-memory:                                                  |
|                 |                                                                                           |
| **Key**         | `pgPrimary.resources.requests.memory <operator.html#pgprimary-resources-requests-memory>`_|
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``256Mi``                                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes memory requests                                                           |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_                                     |
|                 | for a PostgreSQL Primary container                                                        |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-resources-requests-cpu:                                                     |
|                 |                                                                                           |
| **Key**         | `pgPrimary.resources.requests.cpu <operator.html#pgprimary-resources-requests-cpu>`_      |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``500m``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Kubernetes CPU requests                                                                  |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_ for a PostgreSQL Primary container  |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-resources-limits-cpu:                                                       |
|                 |                                                                                           |
| **Key**         | `pgPrimary.resources.limits.cpu <operator.html#pgprimary-resources-limits-cpu>`_          |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``500m``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Kubernetes CPU limits                                                                    |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_ for a PostgreSQL Primary container  |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-resources-limits-memory:                                                    |
|                 |                                                                                           |
| **Key**         | `pgPrimary.resources.limits.memory <operator.html#pgprimary-resources-limits-memory>`_    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``256Mi``                                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes memory limits                                                             |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_                                     |
|                 | for a PostgreSQL Primary container                                                        |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-tolerations:                                                                |
|                 |                                                                                           |
| **Key**         | `pgPrimary.tolerations <operator.html#pgprimary-tolerations>`_                            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | subdoc                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``node.alpha.kubernetes.io/unreachable``                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Kubernetes Pod tolerations                                                               |
|                 | <https://kubernetes.io/docs/concepts/configuration/taint-and-toleration/>`_               |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-volumespec-size:                                                            |
|                 |                                                                                           |
| **Key**         | `pgPrimary.volumeSpec.size <operator.html#pgprimary-volumespec-size>`_                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``1G``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes PersistentVolumeClaim                                                     |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#                         |
|                 | persistentvolumeclaims>`_ size for the PostgreSQL Primary storage                         |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-volumespec-accessmode:                                                      |
|                 |                                                                                           |
| **Key**         | `pgPrimary.volumeSpec.accessmode <operator.html#pgprimary-volumespec-accessmode>`_        |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``ReadWriteOnce``                                                                         |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes PersistentVolumeClaim                                                     |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/                          |
|                 | #persistentvolumeclaims>`_ access modes for the PostgreSQL Primary storage                |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-volumespec-storagetype:                                                     |
|                 |                                                                                           |
| **Key**         | `pgPrimary.volumeSpec.storagetype <operator.html#pgprimary-volumespec-storagetype>`_      |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``dynamic``                                                                               |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Type of the PostgreSQL Primary storage provisioning: ``create`` (the default variant;     |
|                 | used if storage is provisioned, e.g. using hostpath) or ``dynamic`` (for a dynamic        |
|                 | storage provisioner, e.g. via a StorageClass)                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-volumespec-storageclass:                                                    |
|                 |                                                                                           |
| **Key**         | `pgPrimary.volumeSpec.storageclass <operator.html#pgprimary-volumespec-storageclass>`_    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Optionally sets the `Kubernetes storage class                                             |
|                 | <https://kubernetes.io/docs/concepts/storage/storage-classes/>`_ to use with the          |
|                 | PostgreSQL Primary storage `PersistentVolumeClaim                                         |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims>`_|
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-volumespec-matchlabels:                                                     |
|                 |                                                                                           |
| **Key**         | `pgPrimary.volumeSpec.matchLabels <operator.html#pgprimary-volumespec-matchlabels>`_      |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | A PostgreSQL Primary storage `label selector                                              |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#selector>`__             |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgprimary-customconfig:                                                               |
|                 |                                                                                           |
| **Key**         | `pgPrimary.customconfig <operator.html#pgprimary-customconfig>`_                          |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Name of the :ref:`Custom configuration options ConfigMap <operator-configmaps>` for       |
|                 | PostgreSQL cluster                                                                        |
+-----------------+-------------------------------------------------------------------------------------------+

.. _operator-walstorage-section:

`Write-ahead Log Storage Section <operator.html#operator-walstorage-section>`_
--------------------------------------------------------------------------------

The ``walStorage`` section in the `deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__
file contains configuration options for PostgreSQL `write-ahead logging <https://www.postgresql.org/docs/current/wal-intro.html>`_.

.. tabularcolumns:: |p{2cm}|p{13.6cm}|

+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _walstorage-volumespec-size:                                                           |
|                 |                                                                                           |
| **Key**         | `walStorage.volumeSpec.size <operator.html#walstorage-volumespec-size>`_                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``1G``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes PersistentVolumeClaim                                                     |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#                         |
|                 | persistentvolumeclaims>`_ size for the PostgreSQL Write-ahead Log storage                 |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _walstorage-volumespec-accessmode:                                                     |
|                 |                                                                                           |
| **Key**         | `walStorage.volumeSpec.accessmode <operator.html#walstorage-volumespec-accessmode>`_      |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``ReadWriteOnce``                                                                         |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes PersistentVolumeClaim                                                     |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/                          |
|                 | #persistentvolumeclaims>`_ access modes for the PostgreSQL Write-ahead Log storage        |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _walstorage-volumespec-storagetype:                                                    |
|                 |                                                                                           |
| **Key**         | `walStorage.volumeSpec.storagetype <operator.html#walstorage-volumespec-storagetype>`_    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``dynamic``                                                                               |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Type of the PostgreSQL Write-ahead Log storage provisioning: ``create`` (the default      |
|                 | variant; used if storage is provisioned, e.g. using hostpath) or ``dynamic`` (for a       |
|                 | dynamic storage provisioner, e.g. via a StorageClass)                                     |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _walstorage-volumespec-storageclass:                                                   |
|                 |                                                                                           |
| **Key**         | `walStorage.volumeSpec.storageclass <operator.html#walstorage-storageclass>`_             |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Optionally sets the `Kubernetes storage class                                             |
|                 | <https://kubernetes.io/docs/concepts/storage/storage-classes/>`_ to use with the          |
|                 | PostgreSQL Write-ahead Log storage `PersistentVolumeClaim                                 |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims>`_|
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _walstorage-volumespec-matchlabels:                                                    |
|                 |                                                                                           |
| **Key**         | `walStorage.volumeSpec.matchLabels <operator.html#walstorage-volumespec-matchlabels>`_    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | A PostgreSQL Write-ahead Log storage `label selector                                      |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#selector>`__             |
+-----------------+-------------------------------------------------------------------------------------------+



.. _operator-backup-section:

`Backup Section <operator.html#operator-backup-section>`_
--------------------------------------------------------------------------------

The ``backup`` section in the
`deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__
file contains the following configuration options for the regular
Percona Distribution for PostgreSQL backups.

.. tabularcolumns:: |p{2cm}|p{13.6cm}|

+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-image:                                                                         |
|                 |                                                                                           |
| **Key**         | `backup.image <operator.html#backup-backrestimage>`_                                      |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``perconalab/percona-postgresql-operator:main-ppg13-pgbackrest``                          |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The Docker image for :ref:`pgBackRest<backups.pgbackrest>`                                |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-backrestrepoimage:                                                             |
|                 |                                                                                           |
| **Key**         | `backup.backrestRepoImage <operator.html#backup-backrestrepoimage>`_                      |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``perconalab/percona-postgresql-operator:main-ppg13-pgbackrest-repo``                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The Docker image for the :ref:`BackRest repository<backups.pgbackrest.repository>`        |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-resources-requests-cpu:                                                        |
|                 |                                                                                           |
| **Key**         | `backup.resources.requests.cpu <operator.html#backup-resources-requests-cpu>`_            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``500m``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Kubernetes CPU requests                                                                  |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_ for a pgBackRest container          |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-resources-requests-memory:                                                     |
|                 |                                                                                           |
| **Key**         | `backup.resources.requests.memory <operator.html#backup-resources-requests-memory>`_      |
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
|                 | .. _backup-resources-limits-cpu:                                                          |
|                 |                                                                                           |
| **Key**         | `backup.resources.limits.cpu <operator.html#backup-resources-limits-cpu>`_                |
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
| **Key**         | `backup.resources.limits.memory <operator.html#backup-resources-limits-memory>`_          |
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
|                 | .. _backup-volumespec-size:                                                               |
|                 |                                                                                           |
| **Key**         | `backup.volumeSpec.size <operator.html#backup-volumespec-size>`_                          |
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
|                 | .. _backup-volumespec-accessmode:                                                         |
|                 |                                                                                           |
| **Key**         | `backup.volumeSpec.accessmode <operator.html#backup-volumespec-accessmode>`_              |
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
|                 | .. _backup-volumespec-storagetype:                                                        |
|                 |                                                                                           |
| **Key**         | `backup.volumeSpec.storagetype <operator.html#backup-volumespec-storagetype>`_            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``dynamic``                                                                               |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Type of the pgBackRest storage provisioning: ``create`` (the default                      |
|                 | variant; used if storage is provisioned, e.g. using hostpath) or ``dynamic`` (for a       |
|                 | dynamic storage provisioner, e.g. via a StorageClass)                                     |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-volumespec-storageclass:                                                       |
|                 |                                                                                           |
| **Key**         | `backup.volumeSpec.storageclass <operator.html#backup-volumespec-storageclass>`_          |
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
|                 | .. _backup-volumespec-matchlabels:                                                        |
|                 |                                                                                           |
| **Key**         | `backup.volumeSpec.matchLabels <operator.html#backup-volumespec-matchlabels>`_            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | A pgBackRest storage `label selector                                                      |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#selector>`__             |
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
| **Description** | Type of the storage used for backups                                                      |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-storages-endpointurl:                                                          |
|                 |                                                                                           |
| **Key**         | `backup.storages.<storage-name>.endpointURL                                               |
|                 | <operator.html#backup-storages-endpointurl>`_                                             |
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
|                 | .. _backup-storages-bucket:                                                               |
|                 |                                                                                           |
| **Key**         | `backup.storages.<storage-name>.bucket <operator.html#backup-storages-bucket>`_           |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Amazon S3 bucket <https://docs.aws.amazon.com/AmazonS3/latest/dev/UsingBucket.html>`_|
|                 | or                                                                                        |
|                 | `Google Cloud Storage bucket <https://cloud.google.com/storage/docs/key-terms#buckets>`_  |
|                 | name used for backups                                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-storages-region:                                                               |
|                 |                                                                                           |
| **Key**         | `backup.storages.<storage-name>.region <operator.html#backup-storages-region>`_           |
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
|                 | .. _backup-storages-uristyle:                                                             |
|                 |                                                                                           |
| **Key**         | `backup.storages.<storage-name>.uriStyle <operator.html#backup-storages-uristyle>`_       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``path``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Optional parameter that specifies if pgBackRest should use the path or host S3 URI style  |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-storages-verifytls:                                                            |
|                 |                                                                                           |
| **Key**         | `backup.storages.<storage-name>.verifyTLS                                                 |
|                 | <operator.html#backup-storages-verifytls>`_                                               |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | boolean                                                                                   |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``false``                                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Enables or disables TLS verification for pgBackRest                                       |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-storagetypes:                                                                  |
|                 |                                                                                           |
| **Key**         | `backup.storageTypes                                                                      |
|                 | <operator.html#backup-storagetypes>`_                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | array                                                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``[ "s3" ]``                                                                              |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The backup storage types for the pgBackRest repository                                    |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-repopath:                                                                      |
|                 |                                                                                           |
| **Key**         | `backup.repoPath                                                                          |
|                 | <operator.html#backup-repopath>`_                                                         |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Custom path for pgBackRest repository backups                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-schedule-name:                                                                 |
|                 |                                                                                           |
| **Key**         | `backup.schedule.name <operator.html#backup-schedule-name>`_                              |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``sat-night-backup``                                                                      |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The backup name                                                                           |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-schedule-schedule:                                                             |
|                 |                                                                                           |
| **Key**         | `backup.schedule.schedule <operator.html#backup-schedule-schedule>`_                      |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``0 0 * * 6``                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Scheduled time to make a backup specified in the                                          |
|                 | `crontab format <https://en.wikipedia.org/wiki/Cron>`_                                    |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-schedule-keep:                                                                 |
|                 |                                                                                           |
| **Key**         | `backup.schedule.keep <operator.html#backup-schedule-keep>`_                              |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``3``                                                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The amount of most recent backups to store. Older backups are automatically deleted.      |
|                 | Set ``keep`` to zero or completely remove it to disable automatic deletion of backups     |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-schedule-type:                                                                 |
|                 |                                                                                           |
| **Key**         | `backup.schedule.type <operator.html#backup-schedule-type>`_                              |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``full``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The :ref:`type<backups.pgbackrest.backup.type>` of the pgBackRest backup                  |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _backup-schedule-storage:                                                              |
|                 |                                                                                           |
| **Key**         | `backup.schedule.storage <operator.html#backup-schedule-storage>`_                        |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``local``                                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
|| **Description** | The :ref:`type<backups.pgbackrest.repo.type>` of the pgBackRest repository               |
+-----------------+-------------------------------------------------------------------------------------------+

.. _operator-pmm-section:

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
| **Description** | `Percona Monitoring and Management (PMM) Client <https://www.percona.com/doc/             |
|                 | percona-monitoring-and-management/2.x/details/architecture.html#pmm-client>`_ Docker image|
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
|                 | .. _pmm-resources-requests-memory:                                                        |
|                 |                                                                                           |
| **Key**         | `pmm.resources.requests.memory <operator.html#pmm-resources-requests-memory>`_            |
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
|                 | .. _pmm-resources-requests-cpu:                                                           |
|                 |                                                                                           |
| **Key**         | `pmm.resources.requests.cpu <operator.html#pmm-resources-requests-cpu>`_                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``500m``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Kubernetes CPU requests                                                                  |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_ for a PMM container                 |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pmm-resources-limits-cpu:                                                             |
|                 |                                                                                           |
| **Key**         | `pmm.resources.limits.cpu <operator.html#pmm-resources-limits-cpu>`_                      |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``500m``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Kubernetes CPU limits                                                                    |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_ for a PMM container                 |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pmm-resources-limits-memory:                                                          |
|                 |                                                                                           |
| **Key**         | `pmm.resources.limits.memory <operator.html#pmm-resources-limits-memory>`_                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``200M``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes memory limits                                                             |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_                                     |
|                 | for a PMM container                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+

.. _operator-pgbouncer-section:

`pgBouncer Section <operator.html#operator-pgbouncer-section>`_
--------------------------------------------------------------------------------

The ``pgBouncer`` section in the `deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__
file contains configuration options for the `pgBouncer <http://pgbouncer.github.io/>`__ connection pooler for PostgreSQL.

.. tabularcolumns:: |p{2cm}|p{13.6cm}|

+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbouncer-image:                                                                      |
|                 |                                                                                           |
| **Key**         | `pgBouncer.image <operator.html#pgbouncer-image>`_                                        |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``perconalab/percona-postgresql-operator:main-ppg13-pgbouncer``                           |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Docker image for the `pgBouncer <http://pgbouncer.github.io/>`__ connection pooler        |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbouncer-size:                                                                       |
|                 |                                                                                           |
| **Key**         | `pgBouncer.size <operator.html#pgbouncer-size>`_                                          |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``1G``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The number of the pgBouncer Pods to provide connection pooling                            |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbouncer-resources-requests-cpu:                                                     |
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
|                 | .. _pgbouncer-resources-requests-memory:                                                  |
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
| **Key**         | `pgBouncer.resources.limits.cpu <operator.html#pgbouncer-resources-limits-cpu>`_          |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``2``                                                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Kubernetes CPU limits                                                                    |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_ for a pgBouncer container           |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbouncer-resources-limits-memory:                                                    |
|                 |                                                                                           |
| **Key**         | `pgBouncer.resources.limits.memory <operator.html#pgbouncer-resources-limits-memory>`_    |
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
|                 | .. _pgbouncer-expose-servicetype:                                                         |
|                 |                                                                                           |
| **Key**         | `pgBouncer.expose.serviceType <operator.html#pgbouncer-expose-servicetype>`_              |
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
|                 | .. _pgbouncer-expose-loadbalancersourceranges:                                            |
|                 |                                                                                           |
| **Key**         | `pgBouncer.expose.loadBalancerSourceRanges                                                |
|                 | <operator.html#pgbouncer-expose-loadbalancersourceranges>`_                               |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``"10.0.0.0/8"``                                                                          |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The range of client IP addresses from which the load balancer should be reachable         |
|                 | (if not set, there is no limitations)                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbouncer-expose-annotations:                                                         |
|                 |                                                                                           |
| **Key**         | `pgBouncer.expose.annotations <operator.html#pgbouncer-expose-annotations>`_              |
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
|                 | .. _pgbouncer-expose-labels:                                                              |
|                 |                                                                                           |
| **Key**         | `pgBouncer.expose.labels <operator.html#pgbouncer-expose-labels>`_                        |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | label                                                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``pg-cluster-label: cluster1``                                                            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Set `labels <https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/>`_ |
|                 | for the pgBouncer Service                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+

.. _operator-pgreplicas-section:

`pgReplicas Section <operator.html#operator-pgreplicas-section>`_
--------------------------------------------------------------------------------

The ``pgReplicas`` section in the `deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__
file stores information required to manage the replicas within a PostgreSQL cluster.

.. tabularcolumns:: |p{2cm}|p{13.6cm}|

+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgreplicas-size:                                                                      |
|                 |                                                                                           |
| **Key**         | `pgReplicas.<replica-name>.size <operator.html#pgreplicas-size>`_                         |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``1G``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The number of the PostgreSQL Replica Pods                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgreplicas-resources-requests-cpu:                                                    |
|                 |                                                                                           |
| **Key**         | `pgReplicas.<replica-name>.resources.requests.cpu                                         |
|                 | <operator.html#pgreplicas-resources-requests-cpu>`_                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``500m``                                                                                  |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Kubernetes CPU requests                                                                  |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_ for a PostgreSQL Replica container  |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgreplicas-resources-requests-memory:                                                 |
|                 |                                                                                           |
| **Key**         | `pgReplicas.<replica-name>.resources.requests.memory                                      |
|                 | <operator.html#pgreplicas-resources-requests-memory>`_                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``256Mi``                                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes memory requests                                                           |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_                                     |
|                 | for a PostgreSQL Replica container                                                        |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgreplicas-resources-limits-cpu:                                                      |
|                 |                                                                                           |
| **Key**         | `pgReplicas.<replica-name>.resources.limits.cpu                                           |
|                 | <operator.html#pgreplicas-resources-limits-cpu>`_                                         |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``500m``                                                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `Kubernetes CPU limits                                                                    |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_ for a PostgreSQL Replica container  |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgreplicas-resources-limits-memory:                                                   |
|                 |                                                                                           |
| **Key**         | `pgReplicas.<replica-name>.resources.limits.memory                                        |
|                 | <operator.html#pgreplicas-resources-limits-memory>`_                                      |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``256Mi``                                                                                 |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes memory limits                                                             |
|                 | <https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/    |
|                 | #resource-requests-and-limits-of-pod-and-container>`_                                     |
|                 | for a PostgreSQL Replica container                                                        |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgreplicas-volumespec-accessmode:                                                     |
|                 |                                                                                           |
| **Key**         | `pgReplicas.<replica-name>.volumeSpec.accessmode                                          |
|                 | <operator.html#pgreplicas-volumespec-accessmode>`_                                        |
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
|                 | .. _pgreplicas-volumespec-size:                                                           |
|                 |                                                                                           |
| **Key**         | `pgReplicas.<replica-name>.volumeSpec.size <operator.html#pgreplicas-volumespec-size>`_   |
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
|                 | .. _pgreplicas-volumespec-storagetype:                                                    |
|                 |                                                                                           |
| **Key**         | `pgReplicas.<replica-name>.volumeSpec.storagetype                                         |
|                 | <operator.html#pgreplicas-volumespec-storagetype>`_                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``dynamic``                                                                               |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Type of the PostgreSQL Replica storage provisioning: ``create`` (the default              |
|                 | variant; used if storage is provisioned, e.g. using hostpath) or ``dynamic`` (for a       |
|                 | dynamic storage provisioner, e.g. via a StorageClass)                                     |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgreplicas-volumespec-storageclass:                                                   |
|                 |                                                                                           |
| **Key**         | `pgReplicas.<replica-name>.volumeSpec.storageclass                                        |
|                 | <operator.html#pgreplicas-volumespec-storageclass>`_                                      |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``standard``                                                                              |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Optionally sets the `Kubernetes storage class                                             |
|                 | <https://kubernetes.io/docs/concepts/storage/storage-classes/>`_ to use with the          |
|                 | PostgreSQL Replica storage `PersistentVolumeClaim                                         |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims>`_|
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgreplicas-volumespec-matchlabels:                                                    |
|                 |                                                                                           |
| **Key**         | `pgReplicas.<replica-name>.volumeSpec.matchLabels                                         |
|                 | <operator.html#pgreplicas-volumespec-matchlabels>`_                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``""``                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | A PostgreSQL Replica storage `label selector                                              |
|                 | <https://kubernetes.io/docs/concepts/storage/persistent-volumes/#selector>`__             |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgreplicas-labels:                                                                    |
|                 |                                                                                           |
| **Key**         | `pgReplicas.<replica-name>.labels <operator.html#pgbouncer-labels>`_                      |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | label                                                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``pg-cluster-label: cluster1``                                                            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Set `labels <https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/>`_ |
|                 | for PostgreSQL Replica Pods                                                               |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgreplicas-annotations:                                                               |
|                 |                                                                                           |
| **Key**         | `pgReplicas.<replica-name>.annotations <operator.html#pgreplicas-annotations>`_           |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | label                                                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``pg-cluster-annot: cluster1-1``                                                          |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes annotations                                                               |
|                 | <https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/>`_        |
|                 | metadata for PostgreSQL Replica                                                           |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgreplicas-expose-servicetype:                                                        |
|                 |                                                                                           |
| **Key**         | `pgReplicas.<replica-name>.expose.serviceType                                             |
|                 | <operator.html#pgreplicas-expose-servicetype>`_                                           |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``ClusterIP``                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Specifies the type of `Kubernetes Service                                                 |
|                 | <https://kubernetes.io/docs/concepts/services-networking/service/                         |
|                 | #publishing-services-service-types>`_ for for PostgreSQL Replica                          |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgreplicas-expose-loadbalancersourceranges:                                           |
|                 |                                                                                           |
| **Key**         | `pgReplicas.<replica-name>.expose.loadBalancerSourceRanges                                |
|                 | <operator.html#pgreplicas-expose-loadbalancersourceranges>`_                              |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``"10.0.0.0/8"``                                                                          |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The range of client IP addresses from which the load balancer should be reachable         |
|                 | (if not set, there is no limitations)                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgreplicas-expose-annotations:                                                        |
|                 |                                                                                           |
| **Key**         | `pgReplicas.<replica-name>.expose.annotations                                             |
|                 | <operator.html#pgreplicas-expose-annotations>`_                                           |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | label                                                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``pg-cluster-annot: cluster1``                                                            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The `Kubernetes annotations                                                               |
|                 | <https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/>`_        |
|                 | metadata for PostgreSQL Replica                                                           |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgreplicas-expose-labels:                                                             |
|                 |                                                                                           |
| **Key**         | `pgReplicas.<replica-name>.expose.labels <operator.html#pgbouncer-expose-labels>`_        |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | label                                                                                     |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``pg-cluster-label: cluster1``                                                            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | Set `labels <https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/>`_ |
|                 | for the PostgreSQL Replica Service                                                        |
+-----------------+-------------------------------------------------------------------------------------------+

.. _operator-pgbadger-section:

`pgBadger Section <operator.html#operator-pgbadger-section>`_
--------------------------------------------------------------------------------

The ``pgBadger`` section in the `deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__
file contains configuration options for the `pgBadger PostgreSQL log analyzer <https://github.com/darold/pgbadger>`__.

.. tabularcolumns:: |p{2cm}|p{13.6cm}|

+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbadger-enabled:                                                                     |
|                 |                                                                                           |
| **Key**         | `pgBadger.enabled <operator.html#pgbadger-enabled>`_                                      |
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
|                 | .. _pgbadger-image:                                                                       |
|                 |                                                                                           |
| **Key**         | `pgBadger.image <operator.html#pgbadger-image>`_                                          |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | string                                                                                    |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     | ``perconalab/percona-postgresql-operator:main-ppg13-pgbadger``                            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | `pgBadger PostgreSQL log analyzer <https://github.com/darold/pgbadger>`__ Docker image    |
+-----------------+-------------------------------------------------------------------------------------------+
|                                                                                                             |
+-----------------+-------------------------------------------------------------------------------------------+
|                 | .. _pgbadger-port:                                                                        |
|                 |                                                                                           |
| **Key**         | `pgBadger.port <operator.html#pgbadger-port>`_                                            |
+-----------------+-------------------------------------------------------------------------------------------+
| **Value**       | int                                                                                       |
+-----------------+-------------------------------------------------------------------------------------------+
| **Example**     |  ``10000``                                                                                |
+-----------------+-------------------------------------------------------------------------------------------+
| **Description** | The port number for pgBadger                                                              |
+-----------------+-------------------------------------------------------------------------------------------+
