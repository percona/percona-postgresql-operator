.. _operator-constraints:

Binding Percona Distribution for PostgreSQL components to Specific Kubernetes/OpenShift Nodes
=============================================================================================

The operator does good job automatically assigning new Pods to nodes
with sufficient resources to achieve balanced distribution across the cluster.
Still there are situations when it is worth to ensure that pods will land
on specific nodes: for example, to get speed advantages of the SSD
equipped machine, or to reduce network costs choosing nodes in a same
availability zone.

Appropriate sections of the
`deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__
file (such as ``pgPrimary`` or ``pgReplicas``) contain keys which can be used to do this, depending on what is the
best for a particular situation.

Affinity and anti-affinity
--------------------------

Affinity makes Pod eligible (or not eligible - so called “anti-affinity”) to be
scheduled on the node which already has Pods with specific labels. Particularly,
this approach is good to to reduce costs making sure several Pods with intensive
data exchange will occupy the same availability zone or even the same node - or,
on the contrary, to make them land on different nodes or even different
availability zones for the high availability and balancing purposes.

Pod anti-affinity is controlled by the ``antiAffinityType`` option, which can
be put into ``pgPrimary``, ``pgBouncer``, and ``backup`` sections of the
``deploy/cr.yaml`` configuration file. This option can be set to one of two
values:

- ``preferred`` Pod anti-affinity is a sort of a *soft rule*. It makes
  Kubernetes *trying* to schedule Pods matching the anti-affinity rules to
  different Nodes. If it is not possible, then one or more Pods are scheduled
  to the same Node. This variant is used by default.
- ``required`` Pod anti-affinity is a sort of a *hard rule*. It forces
  Kubernetes to schedule each Pod matching the anti-affinity rules to different
  Nodes. If it is not possible, then a Pod will not be scheduled at all.

The following anti-affinity rules are applied to all Percona Distribution for PostgreSQL
Pods:

.. code:: yaml

   affinity:
     podAntiAffinity:
       preferredDuringSchedulingIgnoredDuringExecution:
       - podAffinityTerm:
           labelSelector:
             matchExpressions:
             - key: vendor
               operator: In
               values:
               - crunchydata
             - key: pg-pod-anti-affinity
               operator: Exists
             - key: pg-cluster
               operator: In
               values:
               - cluster1
           topologyKey: kubernetes.io/hostname
         weight: 1

You can see the explanation of these affinity options `in Kubernetes
documentation <https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#inter-pod-affinity-and-anti-affinity>`__.

.. note:: Setting ``required`` anti-affinity type will result in placing all
   Pods on separate nodes, so default configuration **will require 7 Kubernetes nodes**
   to deploy the cluster with separate nodes assigned to one PostgreSQL primary,
   two PostgreSQL replica instances, three pgBouncer and one pgBackrest Pod.

Tolerations
-----------

*Tolerations* allow Pods having them to be able to land onto nodes with
matching *taints*. Toleration is expressed as a ``key`` with and
``operator``, which is either ``exists`` or ``equal`` (the latter
variant also requires a ``value`` the key is equal to). Moreover,
toleration should have a specified ``effect``, which may be a
self-explanatory ``NoSchedule``, less strict ``PreferNoSchedule``, or
``NoExecute``. The last variant means that if a *taint* with
``NoExecute`` is assigned to node, then any Pod not tolerating this
*taint* will be removed from the node, immediately or after the
``tolerationSeconds`` interval, like in the following example:

You can use ``pgPrimary.tolerations`` key in the ``deploy/cr.yaml``
configuration file as follows:

.. code:: yaml

   tolerations:
   - key: "node.alpha.kubernetes.io/unreachable"
     operator: "Exists"
     effect: "NoExecute"
     tolerationSeconds: 6000

The `Kubernetes Taints and
Toleratins <https://kubernetes.io/docs/concepts/configuration/taint-and-toleration/>`__
contains more examples on this topic.

