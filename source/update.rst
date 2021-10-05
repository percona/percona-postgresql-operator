.. _operator-updates:

Update Percona Distribution for PostgreSQL Operator
===================================================

Percona Distribution for PostgreSQL Operator allows upgrades to newer versions.
This includes upgrades of the Operator itself, and upgrades of the Percona
Distribution for PostgreSQL.

.. note:: Only the incremental update to a nearest minor version of the
   Operator is supported. To update
   to a newer version, which differs from the current version by more
   than one, make several incremental updates sequentially.

The following steps will allow you to update both of them to current version
(use the name of your cluster instead of the ``<cluster-name>`` placeholder).

#. Pause the cluster in order to stop all possible activities:

.. code:: bash

   $ kubectl patch perconapgcluster/<cluster-name> --type json -p '[{"op": "replace", "path": "/spec/pause", "value": true},{"op":"replace","path":"/spec/pgBouncer/size","value":0}
]'

#. Remove the old Operator and start the new Operator version:

.. code:: bash

   $ kubectl delete \
       serviceaccounts/pgo-deployer-sa \
       clusterroles/pgo-deployer-cr \
       configmaps/pgo-deployer-cm \
       configmaps/pgo-config \
       clusterrolebindings/pgo-deployer-crb \
       jobs.batch/pgo-deploy \
       deployment/postgres-operator
 
   $ kubectl create -f https://raw.githubusercontent.com/percona/percona-postgresql-operator/v{{{release}}}/deploy/operator.yaml
   $ kubectl wait --for=condition=Complete job/pgo-deploy --timeout=90s

#. Now you can switch the cluster to a new version:

.. code:: bash

   $ kubectl patch perconapgcluster/<cluster-name> --type json -p '[{"op": "replace", "path": "/spec/backup/backrestRepoImage", "value": "percona/percona-postgresql-operator:v{{{release}}}-ppg13-pgbackrest-repo"},{"op":"replace","path":"/spec/backup/image","value":"percona/percona-postgresql-operator:v{{{release}}}-ppg13-pgbackrest"},{"op":"replace","path":"/spec/pgBadger/image","value":"percona/percona-postgresql-operator:v{{{release}}}-ppg13-pgbadger"},{"op":"replace","path":"/spec/pgBouncer/image","value":"percona/percona-postgresql-operator:v{{{release}}}-ppg13-pgbouncer"},{"op":"replace","path":"/spec/pgPrimary/image","value":"percona/percona-postgresql-operator:v{{{release}}}-ppg13-postgres-ha"},{"op":"replace","path":"/spec/userLabels/pgo-version","value":"v{{{release}}}"},{"op":"replace","path":"/metadata/labels/pgo-version","value":"v{{{release}}}"},{"op": "replace", "path": "/spec/pause", "value": false}]'

.. note:: The above example is composed in asumption of using PostgreSQL 13 as
   a database management system. For PostgreSQL 12 you should change all
   occurrences of the ``ppg13`` substring to ``ppg12``.

This will carry on the image update, cluster version update and the pause status
switch.

#. Now you can enable the ``pgbouncer`` again:

.. code:: bash

   $ kubectl patch perconapgcluster/<cluster-name --type json -p \
       '[
           {"op":"replace","path":"/spec/pgBouncer/size","value":1}
       ]'

Wait until the cluster is ready.
