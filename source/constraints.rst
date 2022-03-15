Binding Percona Distribution for PostgreSQL components to Specific Kubernetes/OpenShift Nodes
=============================================================================================

The operator does good job automatically assigning new Pods to nodes
with sufficient to achieve balanced distribution across the cluster.
Still there are situations when it worth to ensure that pods will land
on specific nodes: for example, to get speed advantages of the SSD
equipped machine, or to reduce costs choosing nodes in a same
availability zone.

Appropriate sections of the
`deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__
file (such as ``pgPrimary`` or ``pgReplicas``) contain keys which can be used to do this, depending on what is the
best for a particular situation.

Affinity and anti-affinity
--------------------------

Affinity makes Pod eligible (or not eligible - so called
“anti-affinity”) to be scheduled on the node which already has Pods with
specific labels. Particularly this approach is good to to reduce costs
making sure several Pods with intensive data exchange will occupy the
same availability zone or even the same node - or, on the contrary, to
make them land on different nodes or even different availability zones
for the high availability and balancing purposes.

Percona Distribution for PostgreSQL Operator provides two approaches for doing this:

-  Pod anti-affinity,
-  node affinity.

Using Pod anti-affinity
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
Pod anti-affinity makes it possible to assign PostgreSQL instances to specific
Kubernetes Nodes based on labels on pods that are already running on the node.
There are two types of constraints:

- ``preferredDuringSchedulingIgnoredDuringExecution`` Pod anti-affinity is a
  sort of a *soft rule*. It makes Kubernetes *trying* to schedule Pods matching
  the anti-affinity rules to different Nodes. If it is not possible, then one or
  more Pods to the same Node.
- ``requiredDuringSchedulingIgnoredDuringExecution`` Pod anti-affinity iis a
  sort of a *hard rule*. It forces Kubernetes to schedule each Pod matching the
  anti-affinity rules to different Nodes. If it is not possible, then a Pod will
  not be scheduled at all.

You can set Node affinity using the ``pgReplicas.affinity.podAntiAffinity`` option
in the ``deploy/cr.yaml`` configuration file and the standard `Kubernetes node
affinity rules <https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#inter-pod-affinity-and-anti-affinity>`__.

The following example implies two anti-affinity rules to Distribution for
PostgreSQL Pods:

.. code:: yaml

   affinity:
      podAntiAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
        - labelSelector:
            matchExpressions:
            - key: security
              operator: In
              values:
              - S1
          topologyKey: failure-domain.beta.kubernetes.io/zone
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            podAffinityTerm:
              labelSelector:
                matchExpressions:
                - key: security
                  operator: In
                  values:
                  - S2
              topologyKey: kubernetes.io/hostname


See explanation of the advanced affinity options `in Kubernetes
documentation <https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#inter-pod-affinity-and-anti-affinity>`__.

Using node affinity
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Node affinity makes it possible to assign PostgreSQL instances to specific
Kubernetes Nodes (ones with specific hardware, zone, etc.).
You can set Node affinity using the ``pgReplicas.affinity.nodeAffinity`` option
in the ``deploy/cr.yaml`` configuration file and the standard `Kubernetes node
affinity rules <https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/>`_.

The following example forces Distribution for PostgreSQL Pods to avoid
occupying the same node:

.. code:: yaml

   affinity:
     nodeAffinity:
       requiredDuringSchedulingIgnoredDuringExecution:
         nodeSelectorTerms:
         - matchExpressions:
           - key: kubernetes.io/e2e-az-name
             operator: In
             values:
             - e2e-az1
             - e2e-az2
       preferredDuringSchedulingIgnoredDuringExecution:
       - weight: 1
         preference:
           matchExpressions:
           - key: another-node-label-key
             operator: In
             values:
             - another-node-label-value

See explanation of the advanced affinity options `in Kubernetes
documentation <https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#node-affinity>`__.

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

