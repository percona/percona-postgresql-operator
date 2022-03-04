.. _K8SPG-1.2.0:

================================================================================
*Percona Kubernetes Operator for PostgreSQL* 1.2.0
================================================================================

:Date: March 21, 2022
:Installation: `Installing Percona Distribution for PostgreSQL Operator <https://www.percona.com/doc/kubernetes-operator-for-postgresql/index.html#installation-guide>`_

Release Highlights
================================================================================

* Starting from this release, the Operator automatically generates TLS certificates and turns on encryption by default at cluster creation time. This includes both external certificates which allow user to connect to pgbouncer and PostgreSQL via encrypted channel, and internal ones used for communication between PostgreSQL cluster nodes
* Various cleanups in the ``deploy/cr.yaml`` configuration file simplify the deployment of the cluster making no need in going into YAML manifests and tuning them
* Backup configuration (buckets, storage types, etc.) can now be changed on the running cluster

Improvements
================================================================================

* :jirabug:`K8SPG-149`: Operator should start fresh cluster using the latest images from the Version Service
* :jirabug:`K8SPG-148`: Add possibility of specifying ``imagePullPolicy`` option for all images
* :jirabug:`K8SPG-147`: Add possibility to provide custom configuration for pgbackrest
* :jirabug:`K8SPG-146`: Add ``tar`` package to all PostgreSQL docker images to add support of ``kubectl cp`` command
* :jirabug:`K8SPG-142`: Introduce ``deploy/cr-minimal.yaml`` configuration file to simplify Minikube installation
* :jirabug:`K8SPG-141`: YAML manifest cleanup simplifies cluster bringing up and running, reducing it to just two commands
* :jirabug:`K8SPG-112`: Enable automated generation of TLS certificates and provide encryption for all new clusters by default.
* :jirabug:`K8SPG-95`: Allow changing options from the ``spec.backup`` section for already created clusters

Bugs Fixed
================================================================================

* :jirabug:`K8SPG-161`: Documentation on how to setup a standby cluster is missing
* :jirabug:`K8SPG-182`: Fix the bug that made pause/resume PostgreSQL Cluster functionality not working
* :jirabug:`K8SPG-115`: Fix the bug that caused creation a "cloned" cluster with ``pgDataSource`` to fail due to missing Secrets
* :jirabug:`K8SPG-163`: Remove mongo-driver dependency
* :jirabug:`K8SPG-152`: The operator can't be deployed in disabled/readonly namespace mode
* :jirabug:`K8SPG-116`: restore parameter backrest-restore-from-cluster is misleading
