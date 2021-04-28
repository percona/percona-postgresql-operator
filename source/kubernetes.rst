.. _install-kubernetes:

Install Persona Server for PostgreSQL on Kubernetes
====================================================

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

#. After the operator is started Percona server for PostgreSQL
   can be created at any time with the following command:

   .. code:: bash

      $ kubectl apply -f deploy/cr.yaml

   Creation process will take some time. The process is over when both
   operator and replica set pod have reached their Running status:

   .. code:: bash

      $ kubectl get pods
      NAME                                              READY   STATUS    RESTARTS   AGE
      cluster1-haproxy-0                                1/1     Running   0          5m
      cluster1-haproxy-1                                1/1     Running   0          5m
      cluster1-haproxy-2                                1/1     Running   0          5m
      cluster1-pxc-0                                    1/1     Running   0          5m
      cluster1-pxc-1                                    1/1     Running   0          4m
      cluster1-pxc-2                                    1/1     Running   0          2m
      percona-xtradb-cluster-operator-dc67778fd-qtspz   1/1     Running   0          6m

#. You can also deploy PosgreSQL replica at any time as follows: 

    .. code:: bash

       $ kubectl apply -f ./deploy/cr-pgreplica.yaml

#. Check connectivity to newly created cluster

   .. code:: bash

      $ kubectl run -i --rm --tty percona-client --image=percona:8.0 --restart=Never -- bash -il
      percona-client:/$ mysql -h cluster1-haproxy -uroot -proot_password

   This command will connect you to the MySQL monitor.

   .. code:: text

      mysql: [Warning] Using a password on the command line interface can be insecure.
      Welcome to the MySQL monitor.  Commands end with ; or \g.
      Your MySQL connection id is 1976
      Server version: 8.0.19-10 Percona XtraDB Cluster (GPL), Release rel10, Revision 727f180, WSREP version 26.4.3

      Copyright (c) 2009-2020 Percona LLC and/or its affiliates
      Copyright (c) 2000, 2020, Oracle and/or its affiliates. All rights reserved.

      Oracle is a registered trademark of Oracle Corporation and/or its
      affiliates. Other names may be trademarks of their respective
      owners.

      Type 'help;' or '\h' for help. Type '\c' to clear the current input statement.
