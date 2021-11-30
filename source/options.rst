.. _operator-configmaps:

Changing PostgreSQL Options
===========================

You may require a configuration change for your application. PostgreSQL
allows the option to configure the database with a configuration file.
You can pass the `PostgreSQL configuration options <https://www.postgresql.org/docs/9.3/config-setting.html>`__
in one of the following ways:

* edit the ``deploy/cr.yaml`` file,
* use a ConfigMap.

.. _operator-configmaps-cr:

Edit the ``deploy/cr.yaml`` file
---------------------------------

You can add options by editing the configuration section of the
``deploy/cr.yaml``. Here is an example:

.. code:: yaml

   spec:
     ...
     pgPrimary:
       ...
         customconfig: |
           postgresql:
             parameters:
               max_wal_senders: 10

See :ref:`operator.custom-resource-options` for more details.

.. _operator-configmaps-cm:

Use a ConfigMap
---------------

You can use a configmap and the cluster restart to reset configuration
options. A configmap allows Kubernetes to pass or update configuration
data inside a containerized application.

Use the ``kubectl`` command to create the configmap from external
resources, for more information see `Configure a Pod to use a
ConfigMap <https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/#create-a-configmap>`__.

For example::

   postgresql:
     parameters:
       max_wal_senders: 10

You can create a configmap from the ``postgresql.conf`` file with the
``kubectl create configmap`` command.

You should use the combination of the cluster name with the ``-pgha-config``
addon as the naming convention for the configmap. To find the cluster
name, you can use the following command:

.. code:: bash

   kubectl get pgo

The syntax for ``kubectl create configmap`` command is:

::

   kubectl create configmap <clusterName>-pgha-config <resource-type=resource-name>

The following example defines ``cluster1-pgo`` as the configmap name and the
``postgresql.conf`` file as the data source:

.. code:: bash

   kubectl create configmap cluster1-pgo --from-file=postgresql.conf

To view the created configmap, use the following command:

.. code:: bash

   kubectl describe configmaps cluster1-pgo

.. _operator-configmaps-restart:

Make changed options visible to PostgreSQL
------------------------------------------

Do not forget to restart the cluster to ensure it has updated the configuration
(see details on how to connect in the `Install Percona Distribution for PostgreSQL on Kubernetes <kubernetes.html>`_ page).
