.. _operator-scale:

Scale Percona Distribution for PostgreSQL on Kubernetes and OpenShift
=====================================================================

One of the great advantages brought by Kubernetes and the OpenShift
platform is the ease of an application scaling. Scaling an application
results in adding or removing the Pods and scheduling them to available 
Kubernetes nodes.

Size of the cluster is dynamically controlled by a :ref:`pgReplicas.REPLICA-NAME.size key<pgreplicas-size>` in the :ref:`operator.custom-resource-options` configuration.  That’s why scaling the cluster needs nothing more but changing
this option and applying the updated configuration file. This may be done in a
specifically saved config, or on the fly, using the following command:

.. code:: bash

   $ kubectl scale --replicas=5 pgo/cluster1


In this example we have changed the number of PostgreSQL Replicas to ``5``
instances.

