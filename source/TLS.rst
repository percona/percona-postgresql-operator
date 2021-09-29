.. _tls:

Transport Layer Security (TLS)
******************************

The Percona Distribution for PostgreSQL Operator uses Transport Layer Security
(TLS) cryptographic protocol for the following types of communication:

* Internal - communication between PostgreSQL instances in the cluster
* External - communication between the client application and the cluster

The internal certificate is also used as an authorization method.

TLS security can be configured in several ways:

* The Operator can use a specifically installed *cert-manager* for the automatic
  certificates generation,
* Certificates can be generated manually.

You can also use pre-generated certificates available in the
``deploy/ssl-secrets.yaml`` file for test purposes, but we strongly recommend
**avoiding their usage on any production system**!

The following subsections explain how to configure TLS security with the
Operator yourself, as well as how to temporarily disable it if needed.

.. contents:: :local:

Install and use the *cert-manager*
====================================

About the *cert-manager*
------------------------

A *cert-manager* is a Kubernetes certificate management controller widely used to automate the management and issuance of TLS certificates. It is community-driven, and open source.

When you have already installed *cert-manager* and deploy the operator, the operator requests a certificate from the *cert-manager*. The *cert-manager* acts as a self-signed issuer and generates certificates. The Percona Operator self-signed issuer is local to the operator namespace. This self-signed issuer is created because Percona Distribution for PostgreSQL requires all certificates are issued by the same CA.

The creation of the self-signed issuer allows you to deploy and use the Percona Operator without creating a clusterissuer separately.

Installation of the *cert-manager*
----------------------------------

The steps to install the *cert-manager* are the following:

* Create a namespace
* Disable resource validations on the cert-manager namespace
* Install the cert-manager.

The following commands perform all the needed actions:

.. code:: bash

   $ kubectl create namespace cert-manager
   $ kubectl label namespace cert-manager certmanager.k8s.io/disable-validation=true
   $ kubectl_bin apply -f https://github.com/jetstack/cert-manager/releases/download/v0.15.1/cert-manager.yaml

After the installation, you can verify the *cert-manager* by running the following command:

.. code:: bash

   $ kubectl get pods -n cert-manager

The result should display the *cert-manager* and webhook active and running.

Generate certificates manually
==============================

To generate certificates manually, follow these steps:

1. Provision a :abbr:`CA (Certificate authority)` to generate TLS certificates,
2. Generate a :abbr:`CA (Certificate authority)` key and certificate file with the server details,
3. Create the server TLS certificates using the :abbr:`CA (Certificate authority)` keys, certs, and server
   details.

The set of commands generates certificates with the following attributes:

*  ``Server-pem`` - Certificate
*  ``Server-key.pem`` - the private key
*  ``ca.pem`` - Certificate Authority


You should generate certificates twice: one set is for external communications, and another set is for internal ones.

* The secret which contains TLS :abbr:`CA (Certificate authority)` used for both connection encryption (external traffic), and replication (internal traffic) must be added to the ``spec.sslCA`` key of the ``deploy/cr.yaml`` file.
* The secret created for the **external** use must be added to the ``spec.sslSecretName`` key of the ``deploy/cr.yaml`` file.
* A secret which contains the key pair to encrypt the **internal** communications must be added to the ``spec.secrets.sslReplicationSecretName`` key of the ``deploy/cr.yaml`` file.

Supposing that your cluster name is ``cluster1``, the instructions to generate certificates manually are as follows:


.. code:: bash

   $ CLUSTER_NAME=cluster1
   $ NAMESPACE=default
   $cat <<EOF | cfssl gencert -initca - | cfssljson -bare ca
     {
       "CN": "Root CA",
       "names": [
         {
           "O": "PSMDB"
         }
       ],
       "key": {
         "algo": "rsa",
         "size": 2048
       }
     }
   EOF

   $ cat <<EOF > ca-config.json
     {
       "signing": {
         "default": {
            "expiry": "87600h",
            "usages": ["signing", "key encipherment", "server auth", "client auth"]
          }
       }
     }
   EOF

   $cat <<EOF | cfssl gencert -ca=ca.pem  -ca-key=ca-key.pem -config=./ca-config.json - | cfssljson -bare server
     {
       "hosts": [
         "localhost",
         "${CLUSTER_NAME}",
         "${CLUSTER_NAME}.${NAMESPACE}",
         "${CLUSTER_NAME}.${NAMESPACE}.svc.cluster.local",
         "*.${CLUSTER_NAME}",
         "*.${CLUSTER_NAME}.${NAMESPACE}",
         "*.${CLUSTER_NAME}.${NAMESPACE}.svc.cluster.local"
       ],
       "names": [
         {
           "O": "PGO"
         }
       ],
       "CN": "${CLUSTER_NAME}",
       "key": {
         "algo": "rsa",
         "size": 2048
       }
     }
   EOF
   $ cfssl bundle -ca-bundle=ca.pem -cert=server.pem | cfssljson -bare server

   $ kubectl create secret generic cluster1-ssl-internal --from-file=tls.crt=server.pem --from-file=tls.key=server-key.pem --from-file=ca.crt=ca.pem --type=kubernetes.io/tls

   $ cat <<EOF | cfssl gencert -ca=ca.pem  -ca-key=ca-key.pem -config=./ca-config.json - | cfssljson -bare client
     {
       "hosts": [
         "${CLUSTER_NAME}",
         "${CLUSTER_NAME}.${NAMESPACE}",
         "${CLUSTER_NAME}.${NAMESPACE}.svc.cluster.local",
         "*.${CLUSTER_NAME}",
         "*.${CLUSTER_NAME}.${NAMESPACE}",
         "*.${CLUSTER_NAME}.${NAMESPACE}.svc.cluster.local"
       ],
       "names": [
         {
           "O": "PGO"
         }
       ],
       "CN": "${CLUSTER_NAME}",
       "key": {
         "algo": "rsa",
         "size": 2048
       }
     }
   EOF

   $ kubectl create secret generic cluster1-ssl --from-file=tls.crt=client.pem --from-file=tls.key=client-key.pem --from-file=ca.crt=ca.pem --type=kubernetes.io/tls

Run Percona Distribution for PostgreSQL without TLS
===================================================

Omitting TLS is also possible, but we recommend that you run your cluster with the TLS protocol enabled.

To disable TLS protocol (e.g. for demonstration purposes) set the ``spec.allowUnsafeConfigurations`` key to ``true`` in the ``deploy/cr.yaml`` file and and make sure that there are no certificate secrets available.
