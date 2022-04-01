.. _faq:

================================================================================
Frequently Asked Questions
================================================================================

.. contents::
   :local:
   :depth: 1

Why do we need to follow "the Kubernetes way" when Kubernetes was never intended to run databases?
=====================================================================================================

As it is well known, the Kubernetes approach is targeted at stateless
applications but provides ways to store state (in Persistent Volumes, etc.) if
the application needs it. Generally, a stateless mode of operation is supposed
to provide better safety, sustainability, and scalability, it makes the
already-deployed components interchangeable. You can find more about substantial
benefits brought by Kubernetes to databases in `this blog post <https://www.percona.com/blog/2020/10/08/the-criticality-of-a-kubernetes-operator-for-databases/>`_.

The architecture of state-centric applications (like databases) should be
composed in a right way to avoid crashes, data loss, or data inconsistencies
during hardware failure. |operator|
provides out-of-the-box functionality to automate provisioning and management of
highly available PostgreSQL database clusters on Kubernetes.

How can I contact the developers?
================================================================================

The best place to discuss |operator|
with developers and other community members is the `community forum <https://forums.percona.com/c/postgresql/percona-kubernetes-operator-for-postgresql/68>`_.

If you would like to report a bug, use the |operator| `project in JIRA <https://jira.percona.com/projects/K8SPG>`_.

.. _faq-pgBadger:

How can I analyze PostgreSQL logs with pgBadger?
================================================================================

`pgBadger <https://pgbadger.darold.net/>`_ is a report generator for PostgreSQL,
which can analyze PostgreSQL logs and provide you web-based representation with
charts and various statistics. You can configure it via the 
:ref:`pgBadger Section<operator-pgbadger-section>` in the `deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__
file. The most important option there is :ref:`pgBadger.enabled<operator.html#pgbadger-enabled>`,
which is off by default. When enabled, a separate pgBadger sidecar container
with a specialized HTTP server is added to each PostgreSQL Pod. 

You can generate the log report and access it through an exposed port (10000 by
default) and an ``/api/badgergenerate`` endpoint: 
``http://<Pod-address>:10000/api/badgergenerate``. Also, this report
is available in the appropriate pgBadger container as a ``/report/index.html``
file.

.. _faq-namespaces:

How can I set the Operator to control PostgreSQL in several namespaces?
================================================================================

Sometimes it is convenient to have one Operator watching for PostgreSQL Cluster
custom resources in several namespaces.

You can set additional namespace to be watched by the Operator as follows:

#. First of all clean up the installer artefacts:

   .. code:: bash

      $ kubectl delete -f ./deploy/operator.yaml

#. Make two changes in the ``deploy/operator.yaml`` file. Find the ``namespace``
   key (it is set to ``"pgo"`` by default) and append your new namespace to it
   in a comma-separated list. Also find the element named ``DEPLOY_ACTION`` in
   the ``env`` subsection and change the value from ``install`` to ``update``:

   .. code:: bash

      ...
      namespace: "pgo,myadditionalnamespace"
      ...
      env:
       - name: DEPLOY_ACTION
         value: update

#. Now apply your changes as usual:

   .. code:: bash

      $ kubectl apply -f ./deploy/operator.yaml

   .. note:: You need to perform cleanup between each ``DEPLOY_ACTION``
      activity, which can be either ``install``, ``update``, or ``uninstall``.

