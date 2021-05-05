.. _users:

Users
==============================

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
    * - ``testuser``
      - 

The ``postgres`` user will be the admin user for the database instance. The
``primaryuser`` is used for replication between primary and replicas. The
``testuser`` is a normal user that has access to the database “userdb” that is
created for testing purposes.
