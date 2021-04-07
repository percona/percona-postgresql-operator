
---
title: "Compatibility Requirements"
draft: false
weight: 1
---

## Container Dependencies

The Operator depends on the Crunchy Containers and there are
version dependencies between the two projects. Below are the operator releases and their dependent container release. For reference, the Postgres and PgBackrest versions for each container release are also listed.

| Operator Release   |      Container Release      | Postgres | PgBackrest Version
|:----------|:-------------|:------------|:--------------
| 0.1.0 | 0.1.0  | 13.2 | 2.31 |
|||12.6|2.31|

Features sometimes are added into the underlying Crunchy Containers
to support upstream features in the Operator thus dictating a
dependency between the two projects at a specific version level.

## Operating Systems

The PostgreSQL Operator is developed on both CentOS 7 and RHEL 7 operating
systems.  The underlying containers are designed to use either CentOS 7 or
Red Hat UBI 7 as the base container image.

Other Linux variants are possible but are not supported at this time.

Also, please note that as of version 4.2.2 of the PostgreSQL Operator,
[Red Hat Universal Base Image (UBI)](https://www.redhat.com/en/blog/introducing-red-hat-universal-base-image) 7
has replaced RHEL 7 as the base container image for the various PostgreSQL
Operator containers.  You can find out more information about Red Hat UBI from
the following article:

https://www.redhat.com/en/blog/introducing-red-hat-universal-base-image

## Kubernetes Distributions

The Operator is designed and tested on Kubernetes and OpenShift Container Platform.

## Storage

The Operator is designed to support HostPath, NFS, and Storage Classes for
persistence.  The Operator does not currently include code specific to
a particular storage vendor.

## Releases

The Operator is released on a quarterly basis often to coincide with Postgres releases.

There are pre-release and or minor bug fix releases created on an as-needed basis.
