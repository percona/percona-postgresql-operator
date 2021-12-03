.. _operator-configmaps:

Changing PostgreSQL Options
===========================

You may require a configuration change for your application. PostgreSQL
allows the option to configure the database with a configuration files.
You can pass the PostgreSQL configuration options in one of the following ways:

* edit the ``deploy/cr.yaml`` file,
* use a ConfigMap.

Both ways allow you to provide options specific to the following PostgreSQL
configuration files:

* PostgreSQL main confiuguration, `postgresql.conf <https://www.postgresql.org/docs/current/config-setting.html>`_,
* client authentication configuration, `pg_hba.conf <https://www.postgresql.org/docs/current/auth-pg-hba-conf.html>`_,
* user name configuration, `pg_ident.conf <https://www.postgresql.org/docs/current/auth-username-maps.html>`_.

.. note:: PostgreSQL cluster is managed by the Operator, and so there is no need
   to set custom configuration options in common usage scenarios. Also, changing
   certain options may cause PostgreSQL cluster malfunction. Do not customize
   configuration unless you know what are you doing!

.. _operator-configmaps-cr:

Edit the ``deploy/cr.yaml`` file
---------------------------------

You can add options with custom values to the configuration section of the
``deploy/cr.yaml``. Configuration can include ``postgresql``, ``pg_hba`` and
``pg_ident`` sections for the appropriate PostgreSQL configuration files.
Here is an example which changes the ``max_wal_senders`` option:

.. code:: yaml

   spec:
     ...
     pgPrimary:
       ...
         customconfig: |
           postgresql:
             parameters:
               max_wal_senders: 10

.. _operator-configmaps-cm:

.. note:: Do not forget to restart the cluster to ensure it has updated the
   configuration.

Use a ConfigMap
---------------

You can use a ConfigMap and the cluster restart to reset configuration
options. A `ConfigMap <https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/#create-a-configmap>`__
allows Kubernetes to pass or update configuration data inside a containerized
application.

Use the ``kubectl edit configmap -n pgo <cluster-name>-pgha-config`` command
with the name of your cluster instead of the ``<cluster-name>`` placeholder.
This will run your default text editor where you can put needed options to
``postgresql``, ``pg_hba``, or ``pg_ident`` sections for the appropriate
PostgreSQL configuration files. 

.. note:: To find the cluster name, you can use the ``kubectl get pgo`` command.

For example, let's set set the ``max_wal_senders`` parameter to ``10`` via the
ConfigMap. Put it into the ``postgresql.parameters`` subsection of the YAML
code present in the text editor:

.. code:: yaml

   postgresql:
     parameters:
       max_wal_senders: 10

Save changes and exit your text editor to make options updated. Also, some options
may require you to restart the cluster to ensure the configuration update took
effect.
