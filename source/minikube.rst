.. _install-minikube:

Install Percona Distribution for PostgreSQL on Minikube
======================================================= 

Installing the Percona Distribution for PostgreSQL Operator on `minikube <https://github.com/kubernetes/minikube>`_
is the easiest way to try it locally without a cloud provider. Minikube runs
Kubernetes on GNU/Linux, Windows, or macOS system using a system-wide
hypervisor, such as VirtualBox, KVM/QEMU, VMware Fusion or Hyper-V. Using it is
a popular way to test the Kubernetes application locally prior to deploying it
on a cloud.

The following steps are needed to run Percona Distribution for PostgreSQL Operator on
Minikube:

#. `Install minikube <https://kubernetes.io/docs/tasks/tools/install-minikube/>`_,
   using a way recommended for your system. This includes the installation of
   the following three components:

   #. kubectl tool,
   #. a hypervisor, if it is not already installed,
   #. actual minikube package

   After the installation, run ``minikube start`` command. Being executed,
   this command will download needed virtualized images, then initialize and run
   the cluster. After Minikube is successfully started, you can optionally run
   the Kubernetes dashboard, which visually represents the state of your cluster.
   Executing ``minikube dashboard`` will start the dashboard and open it in your
   default web browser.

#. Clone the percona-xtradb-cluster-operator repository:

   .. code:: bash

      $ git clone -b v{{{release}}} https://github.com/percona/percona-postgresql-operator
      $ cd percona-postgresql-operator

   .. note:: It is crucial to specify the right branch with ``-b``
      option while cloning the code on this step. Please be careful.

#. The next thing to do is to add the ``pgo`` namespace to Kubernetes,
   not forgetting to set the correspondent context for further steps:

   .. code:: bash

      $ kubectl create namespace pgo
      $ kubectl config set-context $(kubectl config current-context) --namespace=pgo

   .. note:: To use different namespace, you should edit *all occurrences* of
      the ``namespace: pgo`` line in both ``deploy/cr.yaml`` and
      ``deploy/operator.yaml`` configuration files.

   If you use Kubernetes dashboard, choose your newly created namespace to be
   shown instead of the default one:

   .. image:: ./assets/images/minikube-ns.svg
      :align: center

#. Deploy the operator with the following command:

   .. code:: bash

      $ kubectl apply -f deploy/operator.yaml

#. Because minikube runs locally, the default ``deploy/cr.yaml`` file should
   be edited to adapt the Operator for the the local installation with limited
   resources. Comment **all occurrences** of the ``resources.requests.memory``
   and ``resources.requests.cpu`` keys to fit the Operator in minikube default
   limitations.

#. Now apply the ``deploy/cr.yaml`` file with the following command:

   .. code:: bash

      $ kubectl apply -f deploy/cr.yaml

   Creation process will take some time. The process is over when both
   operator and replica set pod have reached their Running status:

   .. code:: text

      $ kubectl get pods
      NAME                                             READY   STATUS      RESTARTS      AGE
      backrest-backup-cluster1--1-f29n8                0/1     Completed   0             46s
      cluster1-79bcc648c5-l4mp6                        1/1     Running     0             2m13s
      cluster1-backrest-shared-repo-76b888ff97-85bd9   1/1     Running     0             2m39s
      cluster1-pgbouncer-74867b55f5-cxx74              1/1     Running     0             73s
      cluster1-repl1-d4599d9fd-64cwb                   1/1     Running     0             32s
      cluster1-repl2-67d75d4664-nnpzs                  1/1     Running     0             32s
      pgo-deploy--1-2rxxt                              0/1     Completed   0             12m
      postgres-operator-7df6999fbd-hfp9g               4/4     Running     1 (11m ago)   11m

   You can also track the progress via the Kubernetes dashboard:

   .. image:: ./assets/images/minikube-pods.svg
      :align: center

#. During previous steps, the Operator has generated several `secrets <https://kubernetes.io/docs/concepts/configuration/secret/>`_,
   including the password for the ``pguser`` user, which you will need to access
   the cluster.

   Use ``kubectl get secrets`` command to see the list of Secrets objects(by
   default Secrets object you are interested in has ``cluster1-pguser-secret``
   name). Then ``kubectl get secret cluster1-pguser-secret -o yaml`` will return
   the YAML file with generated secrets, including the password which should
   look as follows:

   .. code:: yaml

     ...
     data:
       ...
       password: cGd1c2VyX3Bhc3N3b3JkCg==

   Here the actual password is base64-encoded, and
   ``echo 'cGd1c2VyX3Bhc3N3b3JkCg==' | base64 --decode`` will bring it back to
   a human-readable form (in this example it will be a ``pguser_password``
   string).


#. Check connectivity to a newly created cluster.

   Run new Pod to use it as a client and connect its console output to your
   terminal (running it may require some time to deploy). When you see the
   command line prompt of the newly created Pod, run run ``psql`` tool using the
   password obtained from the secret:

   .. code:: bash

      $ kubectl run -i --rm --tty pg-client --image=perconalab/percona-distribution-postgresql:13.2 --restart=Never -- bash -il
      [postgres@pg-client /]$ PGPASSWORD='pguser_password' psql -h cluster1-pgbouncer -p 5432 -U pguser pgdb


   This command will connect you to the  PostgreSQL interactive terminal.

   .. code:: text

      psql (13.2)
      Type "help" for help.
      pgdb=>

