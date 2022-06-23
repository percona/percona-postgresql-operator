.. _install-openshift:

Install Percona Distribution for PostgreSQL on OpenShift
========================================================

Following steps will allow you to install the Operator and use it to manage
Percona Distribution for PostgreSQL on Red Hat OpenShift platform.
For more information on the OpenShift, see its `official documentation <https://access.redhat.com/documentation/en-us/openshift_container_platform>`_.

Following steps will allow you to install the Operator and use it to manage
Percona Distribution for PostgreSQL on OpenShift.

#. First of all, clone the percona-postgresql-operator repository:

   .. code:: bash

      git clone -b v{{{release}}} https://github.com/percona/percona-postgresql-operator
      cd percona-postgresql-operator

   .. note:: It is crucial to specify the right branch with ``-b``
      option while cloning the code on this step. Please be careful.

#. The next thing to do is to add the ``pgo`` namespace to Kubernetes,
   not forgetting to set the correspondent context for further steps:

   .. code:: bash

      $ oc create namespace pgo
      $ oc config set-context $(kubectl config current-context) --namespace=pgo

   .. note:: To use different namespace, you should edit *all occurrences* of
      the ``namespace: pgo`` line in both ``deploy/cr.yaml`` and
      ``deploy/operator.yaml`` configuration files.

#. If you are going to use the operator with `anyuid <https://docs.openshift.com/container-platform/4.9/authentication/managing-security-context-constraints.html>` security context constraint
   please execute the following command:

   .. code:: bash

      $ sed -i '/disable_auto_failover: "false"/a \ \ \ \ disable_fsgroup: "false"' deploy/operator.yaml

#. Deploy the operator with the following command:

   .. code:: bash

      $ oc apply -f deploy/operator.yaml

#. After the operator is started, Percona Distribution for PostgreSQL
   can be created at any time with the following command:

   .. code:: bash

      $ oc apply -f deploy/cr.yaml

   Creation process will take some time. The process is over when both
   operator and replica set pod have reached their Running status:

   .. code:: bash

      $ oc get pods
      NAME                                              READY   STATUS    RESTARTS   AGE
      backrest-backup-cluster1-j275w                    0/1     Completed 0          10m
      cluster1-85486d645f-gpxzb                         1/1     Running   0          10m
      cluster1-backrest-shared-repo-6495464548-c8wvl    1/1     Running   0          10m
      cluster1-pgbouncer-fc45869f7-s86rf                1/1     Running   0          10m
      pgo-deploy-rhv6k                                  0/1     Completed 0          5m
      postgres-operator-8646c68b57-z8m62                4/4     Running   1          5m

#. During previous steps, the Operator has generated several `secrets <https://kubernetes.io/docs/concepts/configuration/secret/>`_, including the password for the ``pguser`` user, which you will need to access the cluster.

   Use ``oc get secrets`` command to see the list of Secrets objects (by default Secrets object you are interested in has ``cluster1-pguser-secret`` name). Then ``kubectl get secret cluster1-pguser-secret -o yaml`` will return the YAML file with generated secrets, including the password which should look as follows:

   .. code:: yaml

     ...
     data:
       ...
       password: cGd1c2VyX3Bhc3N3b3JkCg==

   Here the actual password is base64-encoded, and ``echo 'cGd1c2VyX3Bhc3N3b3JkCg==' | base64 --decode`` will bring it back to a human-readable form (in this example it will be a ``pguser_password`` string).

#. Check connectivity to newly created cluster

   .. code:: bash

      $ oc run -i --rm --tty pg-client --image=perconalab/percona-distribution-postgresql:{{{postgresrecommended}}} --restart=Never -- bash -il
      [postgres@pg-client /]$ PGPASSWORD='pguser_password' psql -h cluster1-pgbouncer -p 5432 -U pguser pgdb


   This command will connect you to the PostgreSQL interactive terminal.

   .. code:: text

      psql ({{{postgresrecommended}}})
      Type "help" for help.
      pgdb=>

