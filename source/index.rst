Percona Distribution for PostgreSQL Operator
============================================

Kubernetes and platforms, based on Kubernetes, have added a way to
manage containerized systems, including database clusters. This management is
achieved by controllers, declared in configuration files. These controllers
provide automation with the ability to create objects, such as a container or a
group of containers called pods, to listen for an specific event and then
perform a task.

This automation adds a level of complexity to the container-based architecture
and stateful applications, such as a database. A Kubernetes Operator is a
special type of controller introduced to simplify complex deployments. The
Operator extends the Kubernetes API with custom resources.

Requirements
============

.. toctree::
   :maxdepth: 1

   System-Requirements
   architecture

Installation guides
===================

.. toctree::
   :maxdepth: 1

   minikube
   gke
   eks
   kubernetes
   custom-registry
   broker
   helm
   cluster-wide

Configuration and Management
============================

.. toctree::
   :maxdepth: 1

   users
   storage
   constraints
   options
   haproxy-conf
   proxysql-conf
   TLS
   encryption
   backups
   pause
   update
   scaling
   monitoring
   recovery
   debug

.. toctree::
   :maxdepth: 1

Reference
=============

.. toctree::
   :maxdepth: 1

   operator
   images
   api
   faq
   Release Notes <ReleaseNotes/index>
