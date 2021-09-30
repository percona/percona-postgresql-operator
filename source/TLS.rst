.. _tls:

Transport Layer Security (TLS)
******************************

The Percona Distribution for PostgreSQL Operator uses Transport Layer Security
(TLS) cryptographic protocol for the following types of communication:

* Internal - communication between PostgreSQL instances in the cluster
* External - communication between the client application and the cluster

The internal certificate is also used as an authorization method.

Currently, TLS security needs manual certificates generation.

You can also use pre-generated certificates available in the
``deploy/ssl-secrets.yaml`` file for test purposes, but we strongly recommend
**avoiding their usage on any production system**!

The following subsections explain how to configure TLS security with the
Operator yourself, as well as how to temporarily disable it if needed.

.. contents:: :local:

Generate certificates for the Operator
======================================

To generate certificates, follow these steps:

1. Provision a :abbr:`CA (Certificate authority)` to generate TLS certificates,
2. Generate a :abbr:`CA (Certificate authority)` key and certificate file with the server details,
3. Create the server TLS certificates using the :abbr:`CA (Certificate authority)` keys, certs, and server
   details.

The set of commands generates certificates with the following attributes:

*  ``Server-pem`` - Certificate
*  ``Server-key.pem`` - the private key
*  ``ca.pem`` - Certificate Authority

You should generate one set of certificates for external communications, and another set for internal ones.

Supposing that your cluster name is ``cluster1``, you can use the following commands to generate certificates:

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

When certificates are generated, set the following keys in the ``deploy/cr.yaml`` configuration file:

* ``spec.sslCA`` key should contain the name of the secret with TLS
  :abbr:`CA (Certificate authority)` used for both connection encryption
  (external traffic), and replication (internal traffic),
* ``spec.sslSecretName`` key should contain the name of the secret created to
  encrypt **external** communications,
* ``spec.secrets.sslReplicationSecretName`` key should contain the name of the
  secret created to encrypt **internal** communications,
* ``spec.tlsOnly`` key should be set to ``true`` if you want to disable
  unencrypted communications.

Don't forget to apply changes as usual:

.. code:: bash

   $ kubectl apply -f deploy/cr.yaml

Check the external connection
-----------------------------

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
             image: perconalab/percona-distribution-postgresql:13.2
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

.. code:: bash

   $ kubectl exec -it deployment/pg-client -- bash -il
   $ PGSSLMODE=verify-ca PGSSLROOTCERT=/tmp/tls/ca.crt psql postgres://<postgres-user>:<postgres-pass>@<cluster-name>-pgbouncer.<namespace>.svc.cluster.local

Run Percona Distribution for PostgreSQL without TLS
===================================================

Omitting TLS is also possible, but we recommend that you run your cluster with the TLS protocol enabled.

To disable TLS protocol (e.g. for demonstration purposes) set the
``spec.allowUnsafeConfigurations`` key to ``true`` and ``spec.tlsOnly`` key to
``false`, and and make sure that there are no
certificate secrets configured in the ``deploy/cr.yaml`` file.
