.. _tls:

Transport Layer Security (TLS)
******************************

The |operator| uses Transport Layer Security
(TLS) cryptographic protocol for the following types of communication:

* Internal - communication between PostgreSQL instances in the cluster
* External - communication between the client application and the cluster

The internal certificate is also used as an authorization method for PostgreSQL
Replica instances.

TLS security can be configured in several ways:

* the Operator can generate certificates automatically at cluster creation time,
* you can also generate certificates manually.

You can also use pre-generated certificates available in the
``deploy/ssl-secrets.yaml`` file for test purposes, but we strongly recommend
**avoiding their usage on any production system**!

The following subsections explain how to configure TLS security with the
Operator yourself, as well as how to temporarily disable it if needed.

.. contents:: :local:

.. _tls.certs.auto:

Allow the Operator to generate certificates automatically
=========================================================

The Operator is able to generate long-term certificates automatically and
turn on encryption at cluster creation time, if there are no certificate
secrets available. It generates certificates with the help of `cert-manager <https://cert-manager.io/docs/>`_
-  a Kubernetes certificate management controller widely used to
automate the management and issuance of TLS certificates.
Cert-manager is community-driven and open source.

.. _tls.certs.auto.manager:

Installation of the *cert-manager*
----------------------------------

You can install *cert-manager* as follows:

* Create a namespace,
* Disable resource validations on the cert-manager namespace,
* Install the cert-manager.

The following commands perform all the needed actions:

.. code:: bash

   $ kubectl create namespace cert-manager
   $ kubectl label namespace cert-manager certmanager.k8s.io/disable-validation=true
   $ kubectl_bin apply -f https://github.com/jetstack/cert-manager/releases/download/v1.5.4/cert-manager.yaml

After the installation, you can verify the *cert-manager* by running the following command:

.. code:: bash

   $ kubectl get pods -n cert-manager

The result should display the *cert-manager* and webhook active and running.

.. _tls.certs.auto.on:

Turning automatic generation of certificates on
-----------------------------------------------

When you have already installed *cert-manager*, the operator is able to request a
certificate from it. To make this happend, uncomment ``sslCA``, ``sslSecretName``,
and ``sslReplicationSecretName`` options in the ``deploy/cr.yaml`` configuration
file:

   .. code:: yaml

      ...
      spec:
      #  secretsName: cluster1-users
        sslCA: cluster1-ssl-ca
        sslSecretName: cluster1-ssl-keypair
        sslReplicationSecretName: cluster1-ssl-keypair
      ...

When done, deploy your cluster as usual, with the ``kubectl apply -f deploy/cr.yaml``
command. Certificates will be generated if there are no certificate secrets
available.

.. _tls.certs.manual:

Generate certificates manually
==============================

To generate certificates manually, follow these steps:

1. Provision a :abbr:`CA (Certificate authority)` to generate TLS certificates,
2. Generate a :abbr:`CA (Certificate authority)` key and certificate file with
   the server details,
3. Create the server TLS certificates using the
   :abbr:`CA (Certificate authority)` keys, certs, and server details.

The set of commands generates certificates with the following attributes:

*  ``Server-pem`` - Certificate
*  ``Server-key.pem`` - the private key
*  ``ca.pem`` - Certificate Authority

You should generate one set of certificates for external communications, and
another set for internal ones.

Supposing that your cluster name is ``cluster1``, you can use the following
commands to generate certificates:

.. code:: bash

   $ CLUSTER_NAME=cluster1
   $ NAMESPACE=default
   $ cat <<EOF | cfssl gencert -initca - | cfssljson -bare ca
   {
     "CN": "*",
     "key": {
       "algo": "ecdsa",
       "size": 384
     }
   }
   EOF

   $ cat <<EOF > ca-config.json
   {
      "signing": {
        "default": {
           "expiry": "87600h",
           "usages": ["digital signature", "key encipherment", "content commitment"]
         }
      }
   }
   EOF

   $ cat <<EOF | cfssl gencert -ca=ca.pem  -ca-key=ca-key.pem -config=./ca-config.json - | cfssljson -bare server
   {
      "hosts": [
        "localhost",
        "${CLUSTER_NAME}",
        "${CLUSTER_NAME}.${NAMESPACE}",
        "${CLUSTER_NAME}.${NAMESPACE}.svc.cluster.local",
        "${CLUSTER_NAME}-pgbouncer",
        "${CLUSTER_NAME}-pgbouncer.${NAMESPACE}",
        "${CLUSTER_NAME}-pgbouncer.${NAMESPACE}.svc.cluster.local",
        "*.${CLUSTER_NAME}",
        "*.${CLUSTER_NAME}.${NAMESPACE}",
        "*.${CLUSTER_NAME}.${NAMESPACE}.svc.cluster.local",
        "*.${CLUSTER_NAME}-pgbouncer",
        "*.${CLUSTER_NAME}-pgbouncer.${NAMESPACE}",
        "*.${CLUSTER_NAME}-pgbouncer.${NAMESPACE}.svc.cluster.local"
      ],
      "CN": "${CLUSTER_NAME}",
      "key": {
        "algo": "ecdsa",
        "size": 384
      }
   }
   EOF

   $ kubectl create secret generic ${CLUSTER_NAME}-ssl-ca --from-file=ca.crt=ca.pem
   $ kubectl create secret tls  ${CLUSTER_NAME}-ssl-keypair --cert=server.pem --key=server-key.pem

If your PostgreSQL cluster includes replica instances (this feature is on by default), generate certificates for them in a similar way:

.. code:: bash

   $ cat <<EOF | cfssl gencert -ca=ca.pem  -ca-key=ca-key.pem -config=./ca-config.json - | cfssljson -bare replicas
   {
      "CN": "primaryuser",
      "key": {
         "algo": "ecdsa",
         "size": 384
      }
   }
   EOF

   $ kubectl create secret tls  ${CLUSTER_NAME}-ssl-replicas --cert=replicas.pem --key=replicas-key.pem

When certificates are generated, set the following keys in the
``deploy/cr.yaml`` configuration file:

* ``spec.sslCA`` key should contain the name of the secret with TLS
  :abbr:`CA (Certificate authority)` used for both connection encryption
  (external traffic), and replication (internal traffic),
* ``spec.sslSecretName`` key should contain the name of the secret created to
  encrypt **external** communications,
* ``spec.secrets.sslReplicationSecretName`` key should contain the name of the
  secret created to encrypt **internal** communications,
* ``spec.tlsOnly`` is set to ``true`` by default and enforces encryption

Don't forget to apply changes as usual:

.. code:: bash

   $ kubectl apply -f deploy/cr.yaml

.. _tls.connectivity.check:

Check connectivity to the cluster
=================================

You can check TLS communication with use of the ``psql``, the standard
interactive terminal-based frontend to PostgreSQL. The following command will
spawn a new ``pg-client`` container, which includes needed command and can be
used for the check (use your real cluster name instead of the ``<cluster-name>``
placeholder):

.. code:: bash

   $ cat <<EOF | kubectl apply -f -
   apiVersion: apps/v1
   kind: Deployment
   metadata:
     name: pg-client
   spec:
     replicas: 1
     selector:
       matchLabels:
         name: pg-client
     template:
       metadata:
         labels:
           name: pg-client
       spec:
         containers:
           - name: pg-client
             image: perconalab/percona-distribution-postgresql:{{{postgresrecommended}}}
             imagePullPolicy: Always
             command:
             - sleep
             args:
             - "100500"
             volumeMounts:
               - name: ca
                 mountPath: "/tmp/tls"
         volumes:
         - name: ca
           secret:
             secretName: <cluster_name>-ssl-ca
             items:
             - key: ca.crt
               path: ca.crt
               mode: 0777
   EOF

Now get shell access to the newly created container, and launch the PostgreSQL
interactive terminal to check connectivity over the encrypted channel (please
use real cluster-name, PostgreSQL user login and password):

.. code:: bash

   $ kubectl exec -it deployment/pg-client -- bash -il
   [postgres@pg-client /]$ PGSSLMODE=verify-ca PGSSLROOTCERT=/tmp/tls/ca.crt psql postgres://<postgresql-user>:<postgresql-password>@<cluster-name>-pgbouncer.<namespace>.svc.cluster.local

Now you should see the prompt of PostgreSQL interactive terminal:

.. code:: bash

   psql ({{{postgresrecommended}}})
   Type "help" for help.
   pgdb=>

.. _tls.no.tls:

Run Percona Distribution for PostgreSQL without TLS
===================================================

Omitting TLS is also possible, but we recommend that you run your cluster with
the TLS protocol enabled.

To disable TLS protocol (e.g. for demonstration purposes) set the
``spec.tlsOnly`` key to ``false``, and make sure that there are no
certificate secrets configured in the ``deploy/cr.yaml`` file.
