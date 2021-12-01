.. _users:

Users
==============================

User accounts within the Cluster can be divided into two different groups:

* *application-level users*: the unprivileged user accounts,
* *system-level users*: the accounts needed to automate the cluster deployment
  and management tasks.

.. _users.system-users:

`System Users <users.html#system-users>`_
-------------------------------------------

Credentials for system users are stored as a `Kubernetes Secrets <https://kubernetes.io/docs/concepts/configuration/secret/>`_ object.
The Operator requires to be deployed before PostgreSQL Cluster is
started. The name of the required secrets (``cluster1-users-secrets`` by default)
should be set in the ``spec.secretsName`` option of the ``deploy/cr.yaml``
configuration file.

The following table shows system users' names and purposes.

.. warning:: These users should not be used to run an application.

The default PostgreSQL instance installation via the Percona Distribution for
PostgreSQL Operator comes with the following users:

.. list-table::
    :header-rows: 1

    * - Role name
      - Attributes
    * - ``postgres``
      - Superuser, Create role, Create DB, Replication, Bypass RLS
    * - ``primaryuser``
      - Replication
    * - ``pguser``
      - Non-privileged user
    * - ``pgbouncer``
      - Administrative user for the `pgBouncer connection pooler <http://pgbouncer.github.io/>`_

The ``postgres`` user will be the admin user for the database instance. The
``primaryuser`` is used for replication between primary and replicas. The
``pguser`` is the default non-privileged user.

YAML Object Format
******************

The default name of the Secrets object for these users is
``cluster1-users-secrets`` and can be set in the CR for your cluster in
``spec.secretName`` to something different. When you create the object yourself,
it should match the following simple format:

.. code:: yaml

   apiVersion: v1
   kind: Secret
   metadata:
     name: my-cluster-secrets
   type: Opaque
   stringData:
     pgbouncer: pgbouncer_password
     postgres: postgres_password
     primaryuser: primaryuser_password
     pguser: pguser_password

The example above matches what is shipped in the `deploy/secrets.yaml <https://github.com/percona/percona-postgresql-operator/blob/main/deploy/users-secret.yaml>`_
file.

As you can see, we use the ``stringData`` type when creating the Secrets
object, so all values for each key/value pair are stated in plain text format
convenient from the user's point of view. But the resulting Secrets
object contains passwords stored as ``data`` - i.e., base64-encoded strings.
If you want to update any field, you'll need to encode the value into base64
format. To do this, you can run ``echo -n "password" | base64`` in your local
shell to get valid values. For example, setting the PMM Server user's password
to ``new_password`` in the ``cluster1-users-secrets`` object can be done
with the following command:

.. code:: bash

   kubectl patch secret/cluster1-users-secrets -p '{"data":{"pguser": '$(echo -n new_password | base64)'}}'
