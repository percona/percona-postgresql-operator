.. _install-kubernetes:

Install Percona Distribution for PostgreSQL on Kubernetes
=========================================================

Following steps will allow you to install Percona Distribution for PostgreSQL
Operator and use it to manage Percona Distribution for PostgreSQL in a
Kubernetes-based environment.

#. First of all, clone the percona-postgresql-operator repository:

   .. code:: bash

      git clone -b v{{{release}}} https://github.com/percona/percona-postgresql-operator
      cd percona-postgresql-operator

   .. note:: It is crucial to specify the right branch with ``-b``
      option while cloning the code on this step. Please be careful.

#. The next thing to do is to add the ``pgo`` namespace to Kubernetes,
   not forgetting to set the correspondent context for further steps:

   .. code:: bash

      $ kubectl create namespace pgo
      $ kubectl config set-context $(kubectl config current-context) --namespace=pgo


#. Deploy the operator with the following command:

   .. code:: bash

      $ kubectl apply -f deploy/operator.yaml

#. After the operator is started Percona distribution for PostgreSQL
   can be created at any time with the following commands:

   .. code:: bash

      $ kubectl apply -f deploy/pguser-secret.yaml
      $ kubectl apply -f deploy/cr.yaml

   Creation process will take some time. The process is over when both
   operator and replica set pod have reached their Running status:

   .. code:: bash

      $ kubectl get pods
      NAME                                              READY   STATUS    RESTARTS   AGE
      backrest-backup-cluster1-j275w                    0/1     Completed 0          10m
      cluster1-85486d645f-gpxzb                         1/1     Running   0          10m
      cluster1-backrest-shared-repo-6495464548-c8wvl    1/1     Running   0          10m
      cluster1-pgbouncer-fc45869f7-s86rf                1/1     Running   0          10m
      pgo-deploy-rhv6k                                  0/1     Completed 0          5m
      postgres-operator-8646c68b57-z8m62                4/4     Running   1          5m

#. You can also deploy PosgreSQL replica at any time as follows: 

    .. code:: bash

       $ kubectl apply -f ./deploy/cr-pgreplica.yaml

#. Check connectivity to newly created cluster

   .. code:: bash

      $ kubectl run -i --rm --tty pg-client --image=perconalab/percona-distribution-postgresql:13.2 --restart=Never -- bash -il
      [postgres@pg-client /]$ PGPASSWORD='pguser_password' psql -h cluster1-pgbouncer -p 5432 -U pguser pgdb


   This command will connect you to the PostgreSQL interactive terminal.

   .. code:: text

      psql (13.2)
      Type "help" for help.
      pgdb=>

