.. _howtos:

How to deploy a standby cluster for Disaster Recovery
====================================================================================

Deployment of a standby PostgreSQL cluster is mainly targeted for Disaster Recovery (DR),
 though it can be also used for migrations.

In both cases, it involves using some :ref:`object storage system for backups<backups>`, such as AWS S3 or GCP Cloud Storage buckets, that can be accessed by the standby cluster:

* there is a *primary cluster* with configured ``pgbackrest`` tool, which pushes the write-ahead log (WAL) archives :ref:`to the
correct remote repository<backups.pgbackrest.repository>`,
* the *standby cluster* is built from one of these backups, and it is kept in sync with the primary cluster by consuming the WAL files that are copied from the remote repository.

.. note:: The primary node in the *standby cluster* is **not a streaming replica** from any of the nodes in the *primary cluster*. It relies only on WAL archives to replicate events. For this reason, this approach cannot be used as a High Availability solution.

Creating such standby cluster involves the following steps:

* Copy needed passwords from the *primary cluster* Secrets, and  adjust them to use the *standby cluster’s* name. The following commands save the secrets files from ``cluster1`` under ``/tmp/copied-secrets`` directory and prepares them to be used in ``cluster2``:

  .. note:: Make sure you have the `yq tool installed <https://github.com/mikefarah/yq/#install>`_ in your system.

.. code:: bash

   $ mkdir -p /tmp/copied-secrets/
   $ export primary_cluster_name=cluster1
   $ export standby_cluster_name=cluster2
   $ export secrets="${primary_cluster_name}-users"
   $ kubectl get secret/$secrets -o yaml \
   yq eval 'del(.metadata.creationTimestamp)' - \
   yq eval 'del(.metadata.uid)' - \
   yq eval 'del(.metadata.selfLink)' - \
   yq eval 'del(.metadata.resourceVersion)' - \
   yq eval 'del(.metadata.namespace)' - \
   yq eval 'del(.metadata.annotations."kubectl.kubernetes.io/last-applied-configuration")' - \
   yq eval '.metadata.name = "'"${secrets/$primary_cluster_name/$standby_cluster_name}"'"' - \
   yq eval '.metadata.labels.pg-cluster = "'"${standby_cluster_name}"'"' - \
   >/tmp/copied-secrets/${secrets/$primary_cluster_name/$standby_cluster_name}

* Create the Operator in the Kubernetes environment for the standby cluster, if
  not done:

  .. code:: bash 

     $ kubectl apply -f deploy/operator.yaml

* Apply the Adjusted Kubernetes Secrets:

   $ export standby_cluster_name=cluster2
   $ kubectl create -f /tmp/copied-secrets/${standby_cluster_name}-users

* Enable the standby option in your *standby cluster's* ``deploy/cr.yaml`` file:

  .. code:: yaml

     standby: true

* Configure your *standby cluster's* ``deploy/cr.yaml`` file to use the same
  Cloud Storage for backups, which was used by the **primary cluster**. This
  may be specific to your Storage. Refer to the Operator's :ref:`backups documentation<backups>`
  for this step.
