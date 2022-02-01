.. _faq:

================================================================================
Frequently Asked Questions
================================================================================

.. contents::
   :local:
   :depth: 1

Why do we need to follow "the Kubernetes way" when Kubernetes was never intended to run databases?
=====================================================================================================

As it is well known, the Kubernetes approach is targeted at stateless
applications but provides ways to store state (in Persistent Volumes, etc.) if
the application needs it. Generally, a stateless mode of operation is supposed
to provide better safety, sustainability, and scalability, it makes the
already-deployed components interchangeable. You can find more about substantial
benefits brought by Kubernetes to databases in `this blog post <https://www.percona.com/blog/2020/10/08/the-criticality-of-a-kubernetes-operator-for-databases/>`_.

The architecture of state-centric applications (like databases) should be
composed in a right way to avoid crashes, data loss, or data inconsistencies
during hardware failure. Percona Distribution for PostgreSQL Operator
provides out-of-the-box functionality to automate provisioning and management of
highly available PostgreSQL database clusters on Kubernetes.

How can I contact the developers?
================================================================================

The best place to discuss Percona Distribution for PostgreSQL Operator
with developers and other community members is the `community forum <https://forums.percona.com/c/postgresql/percona-kubernetes-operator-for-postgresql/68>`_.

If you would like to report a bug, use the `Percona Distribution for MySQL Operator project in JIRA <https://jira.percona.com/projects/K8SPG>`_.
