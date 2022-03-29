.. _install-helm:

Install Percona Distribution for PostgreSQL using Helm
======================================================

`Helm <https://github.com/helm/helm>`_ is the package manager for Kubernetes. Percona Helm charts can be found in `percona/percona-helm-charts <https://github.com/percona/percona-helm-charts>`_ repository in Github.

Pre-requisites
--------------

Install Helm following its `official installation instructions <https://docs.helm.sh/using_helm/#installing-helm>`_.

.. note:: Helm v3 is needed to run the following steps.


Installation
-------------

#. Add the Percona's Helm charts repository and make your Helm client up to
   date with it:

   .. code:: bash

      $ helm repo add percona https://percona.github.io/percona-helm-charts/
      $ helm repo update

#. Install the |operator|:

   .. code:: bash

      $ helm install my-operator percona/pg-operator --version {{{release}}}

   The ``my-operator`` parameter in the above example is the name of `a new release object <https://helm.sh/docs/intro/using_helm/#three-big-concepts>`_ 
   which is created for the Operator when you install its Helm chart (use any
   name you like).

   .. note:: If nothing explicitly specified, ``helm install`` command will work
      with ``default`` namespace. To use different namespace, provide it with
      the following additional parameter: ``--namespace my-namespace``.

#. Install PostgreSQL:

   .. code:: bash

      $ helm install my-db percona/pg-db --version {{{release}}} --namespace my-namespace

   The ``my-db`` parameter in the above example is the name of `a new release object <https://helm.sh/docs/intro/using_helm/#three-big-concepts>`_ 
   which is created for the Percona Distribution for PostgreSQL when you install
   its Helm chart (use any name you like).

Installing Percona Distribution for PostgreSQL with customized parameters
-------------------------------------------------------------------------

The command above installs Percona Distribution for PostgreSQL with :ref:`default parameters<operator.custom-resource-options>`.
Custom options can be passed to a ``helm install`` command as a
``--set key=value[,key=value]`` argument. The options passed with a chart can be
any of the Operator's :ref:`operator.custom-resource-options`.

The following example will deploy a Percona Distribution for PostgreSQL Cluster
in the ``pgdb`` namespace, with enabled `Percona Monitoring and Management (PMM) <https://www.percona.com/doc/percona-monitoring-and-management/2.x/index.html>`_
and 20 Gi storage for a Primary PostgreSQL node:

.. code:: bash

   $ helm install my-db percona/pg-db --namespace pgdb \
     --set pgPrimary.volumeSpec.size=20Gi \
     --set pmm.enabled=true
