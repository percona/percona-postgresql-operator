Percona Distribution for PostgreSQL Operator
============================================

Kubernetes have added a way to manage containerized systems, including database
clusters. This management is achieved by controllers, declared in configuration
files. These controllers provide automation with the ability to create objects,
such as a container or a group of containers called pods, to listen for an
specific event and then perform a task.

This automation adds a level of complexity to the container-based architecture
and stateful applications, such as a database. A Kubernetes Operator is a
special type of controller introduced to simplify complex deployments. The
Operator extends the Kubernetes API with custom resources.


The `Percona Distribution for PostgreSQL Operator <https://github.com/percona/percona-postgresql-operator>`_ is based on best practices for configuration and
setup of a Percona Distribution for PostgreSQL cluster. The benefits of the
Operator are many, but saving time and delivering a consistent and vetted
environment is key.

Requirements
============

.. toctree::
   :maxdepth: 1

   System-Requirements
   architecture

.. _operator-install:

Installation guide
===================

.. toctree::
   :maxdepth: 1

   kubernetes
   openshift
   minikube
   gke
   helm

Configuration and Management
============================

.. toctree::
   :maxdepth: 1

   users
   backups
   options
   constraints
   pause
   update
   scaling
   TLS
   monitoring

.. toctree::
   :maxdepth: 1

HOWTOs
=============

   standby


Reference
=============

.. toctree::
   :maxdepth: 1

   operator
   images
   faq
   Release Notes <ReleaseNotes/index>
