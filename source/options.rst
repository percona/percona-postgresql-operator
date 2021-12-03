.. _operator-configmaps:

Changing PostgreSQL Options
===========================

You may require a configuration change for your application. PostgreSQL
allows customizing the database with configuration files.
You can use a `ConfigMap <https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/#create-a-configmap>`__
to provide the PostgreSQL configuration options specific to the following
configuration files:

* PostgreSQL main configuration, `postgresql.conf <https://www.postgresql.org/docs/current/config-setting.html>`_,
* client authentication configuration, `pg_hba.conf <https://www.postgresql.org/docs/current/auth-pg-hba-conf.html>`_,
* user name configuration, `pg_ident.conf <https://www.postgresql.org/docs/current/auth-username-maps.html>`_.

Configuration options may be applied in two ways:

* globally to all database servers in the cluster via `Patroni Distributed Configuration Store (DCS) <https://patroni.readthedocs.io/en/latest/dynamic_configuration.html>`_,
* locally to each database server (Primary and Replica) within the cluster.

.. note:: PostgreSQL cluster is managed by the Operator, and so there is no need
   to set custom configuration options in common usage scenarios. Also, changing
   certain options may cause PostgreSQL cluster malfunction. Do not customize
   configuration unless you know what you are doing!

Use the ``kubectl`` command to create the ConfigMap from external
resources, for more information, see `Configure a Pod to use a
ConfigMap <https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/#create-a-configmap>`__.

You can either create a PostgreSQL Cluster With Custom Configuration, or
use ConfigMap to set options for the already existing cluster.

To create a cluster with custom options, you should first place these options
in a ``postgres-ha.yaml`` file under specific ``bootstrap`` section, then
use ``kubectl create configmap`` command with this file to create a ConfigMap,
and finally put the ConfigMap name to :ref:`pgPrimary.customconfig<pgprimary-customconfig>`
key in the ``deploy/cr.yaml`` configuration file.

To change options for an existing cluster, you can do the same but put options
in a ``postgres-ha.yaml`` file directly, without the ``bootstrap`` section.

.. _operator-configmaps-create:

Creating a cluster with custom options
--------------------------------------

For example, you can create a cluster with a custom ``max_connections`` option
in a ``proxysql.conf`` configuration file using the following ``postgres-ha.yaml``
contents:

.. code:: yaml

   ---
   bootstrap:
     dcs:
       postgresql:
         parameters:
           max_connections: 30

..note:: ``dsc.postgresql`` subsection means that option will be applied globally to
         ``postgresql.conf`` of all database servers.

You can create a ConfigMap from this file. The syntax for ``kubectl create configmap`` command is:

.. code:: bash

   kubectl -n <namespace> create configmap <configmap-name> --from-file=postgres-ha.yaml

ConfigMap name should include your cluster name and a dash as a prefix
(``cluster1-`` by default). 

The following example defines ``cluster1-custom-config`` as the ConfigMap name:

.. code:: bash

   $ kubectl create -n pgo configmap cluster1-custom-config --from-file=postgres-ha.yaml

To view the created ConfigMap, use the following command:

.. code:: bash

   $ kubectl describe configmaps cluster1-custom-config

Don't forget to put the name of your ConfigMap to the ``deploy/cr.yaml``
configuration file:

.. code:: yaml

   spec:
     ...
     pgPrimary:
       ...
         customconfig: "cluster1-custom-config"

Now you can create the cluster following the :ref:`regular installation instructions<_operator-install>`.

.. _operator-configmaps-change:

Modifying options for the existing cluster
------------------------------------------

For example, you can change ``max_connections`` option in a ``proxysql.conf``
configuration file with the following ``postgres-ha.yaml`` contents:

.. code:: yaml

   ---
   dcs:
     postgresql:
       parameters:
         max_connections: 50

..note:: ``dsc.postgresql`` subsection means that option will be applied globally to
         ``postgresql.conf`` of all database servers.

You can create a ConfigMap from this file. The syntax for ``kubectl create configmap`` command is:

.. code:: bash

   kubectl -n <namespace> create configmap <configmap-name> --from-file=postgres-ha.yaml

ConfigMap name should include your cluster name and a dash as a prefix
(``cluster1-`` by default). 

The following example defines ``cluster1-custom-config`` as the ConfigMap name:

.. code:: bash

   $ kubectl create -n pgo configmap cluster1-custom-config --from-file=postgres-ha.yaml

To view the created ConfigMap, use the following command:

.. code:: bash

   $ kubectl describe configmaps cluster1-custom-config

You can also use a similar ``kubectl edit configmap`` command to change the
already existing ConfigMap with your default text editor:

.. code:: bash

   $ kubectl edit -n pgo configmap cluster1-custom-config

Don't forget to put the name of your ConfigMap to the ``deploy/cr.yaml``
configuration file if it isn't already there:

.. code:: yaml

   spec:
     ...
     pgPrimary:
       ...
         customconfig: "cluster1-custom-config"

Now you should :ref:`restart the cluster<operator-pause>` to ensure the update
took effect.
