.. rn:: 1.2.0

================================================================================
*Percona Operator for PostgreSQL* 1.2.0
================================================================================

:Date: April 6, 2022
:Installation: `Percona Operator for PostgreSQL <https://www.percona.com/doc/kubernetes-operator-for-postgresql/index.html#installation-guide>`_

Release Highlights
================================================================================

* With this release, the Operator turns to a simplified naming convention and changes its official name to **Percona Operator for PostgreSQL**
* Starting from this release, the Operator :ref:`automatically generates<tls.certs.auto>` TLS certificates and turns on encryption by default at cluster creation time. This includes both external certificates which allow users to connect to pgBouncer and PostgreSQL via the encrypted channel, and internal ones used for communication between PostgreSQL cluster nodes
* Various cleanups in the `deploy/cr.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr.yaml>`__ configuration file simplify the deployment of the cluster, making no need in going into YAML manifests and tuning them

Improvements
================================================================================

* :jirabug:`K8SPG-149`: It is now possible to :ref:`explicitly set the version of PostgreSQL for newly provisioned clusters<operator-update-smartupdates>`. Before that, all new clusters were started with the latest PostgreSQL version if Version Service was enabled
* :jirabug:`K8SPG-148`: Add possibility of specifying ``imagePullPolicy`` option for all images in the Custom Resource of the cluster to run in air-gapped environments
* :jirabug:`K8SPG-147`: Users now can :ref:`pass additional customizations<backup-customconfig>` to pgBackRest with the  pgBackRest configuration options provided via ConfigMap 
* :jirabug:`K8SPG-142`: Introduce `deploy/cr-minimal.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/cr-minimal.yaml>`__ configuration file to deploy minimal viable clusters - useful for developers to deploy PostgreSQL on local Kubernetes clusters, such as :ref:`Minikube<install-minikube>`
* :jirabug:`K8SPG-141`: YAML manifest cleanup simplifies cluster deployment, reducing it to just two commands
* :jirabug:`K8SPG-112`: Enable automated generation of TLS certificates and provide encryption for all new clusters by default
* :jirabug:`K8SPG-161`: The Operator documentation now has a how-to that covers :ref:`deploying a standby PostgreSQL cluster on Kubernetes<howto_standby>`

Bugs Fixed
================================================================================

* :jirabug:`K8SPG-115`: Fix the bug that caused creation a "cloned" cluster with ``pgDataSource`` to fail due to missing Secrets
* :jirabug:`K8SPG-163`: Fix the security vulnerability `CVE-2021-40346 <https://nvd.nist.gov/vuln/detail/CVE-2021-20329>`_ by removing the unused dependency in the Operator images
* :jirabug:`K8SPG-152`: Fix the bug that prevented deploying the Operator in disabled/readonly namespace mode. It is now possible to deploy several operators in different namespaces in the same cluster

Options Changes
================================================================================

* :jirabug:`K8SPG-116`: The ``backrest-restore-from-cluster`` parameter was renamed to ``backrest-restore-cluster`` for clarity in the `deploy/backup/restore.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/backup/restore.yaml>`_ file used to :ref:`restore the cluster from a previously saved backup<backups-restore>`

Supported platforms
================================================================================


The following platforms were tested and are officially supported by the Operator
1.2.0:

* `Google Kubernetes Engine (GKE) <https://cloud.google.com/kubernetes-engine>`_ 1.19 - 1.22
* `Amazon Elastic Container Service for Kubernetes (EKS) <https://aws.amazon.com>`_ 1.19 - 1.21
* `OpenShift <https://www.redhat.com/en/technologies/cloud-computing/openshift>`_ 4.7 - 4.9

This list only includes the platforms that the Percona Operators are specifically tested on as part of the release process. Other Kubernetes flavors and versions depend on the backward compatibility offered by Kubernetes itself.
