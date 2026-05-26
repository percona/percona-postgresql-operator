# OLM Bundle Generation

This directory generates and validates OLM bundles for Percona Operator for PostgreSQL.

The Makefile builds two bundle variants:

- `community`: package `percona-postgresql-operator`
- `redhat`: package `percona-postgresql-operator-certified`

## Requirements

Run the commands from this directory:

```sh
cd installers/olm
```

Install the local toolchain used by the Makefile:

```sh
make tools
```

The Makefile puts `tools/<system>` first in `PATH`, so `generate.sh` uses the expected versions of `jq`, `yq`, `kubectl`, `operator-sdk`, and `opm`.

## Generate Bundles

Generate and validate both bundles:

```sh
make bundles VERSION=3.0.0
```

Generate only the community bundle:

```sh
make bundles/community VERSION=3.0.0
```

Generate only the Red Hat certified bundle:

```sh
make bundles/redhat VERSION=3.0.0
```

Generated files are written to:

```text
bundles/community
bundles/redhat
```

## Build And Push Bundle Images

Build one bundle image:

```sh
make build VERSION=3.0.0 BUNDLE_DISTRO=community
make build VERSION=3.0.0 BUNDLE_DISTRO=redhat
```

Override the target image when needed:

```sh
make build VERSION=3.0.0 BUNDLE_DISTRO=redhat \
  BUNDLE_IMG=perconalab/percona-postgresql-operator:certified-bundle-3.0.0
```

Push the selected `BUNDLE_IMG`:

```sh
make push VERSION=3.0.0 \
  BUNDLE_IMG=perconalab/percona-postgresql-operator:certified-bundle-3.0.0
```

## Validate Bundles

The `bundles/*` targets already run:

```sh
operator-sdk bundle validate bundles/<distro> --select-optional='suite=operatorframework'
```

Additional validation targets are available:

```sh
make validate-bundles
make validate-community-directory
make validate-redhat-directory
make validate-community-image
make validate-redhat-image
```

## Useful Variables

Common variables:

```text
VERSION              Release version. Required.
OPENSHIFT_VERSIONS   OpenShift range annotation. Default: v4.18-v4.21
PACKAGE_CHANNEL      Bundle channel. Default: preview
IMAGE                Community operator image.
REDHAT_OPERATOR_IMAGE Red Hat operator image.
BUNDLE_DISTRO        Bundle to build: community or redhat.
BUNDLE_IMG           Bundle image tag used by build/push.
CONTAINER            Container tool. Default: docker.
```

Example:

```sh
make bundles/redhat VERSION=3.0.0 \
  OPENSHIFT_VERSIONS=v4.18-v4.21 \
```

## Red Hat Notes

The Red Hat bundle uses `distributions/redhat.sh` as a distribution automation. It sets certified package metadata, related images, digest references, and CSV overrides.

If a Red Hat image digest cannot be resolved from the Red Hat catalog, the generated CSV uses:

```text
@<DIGEST>
```

Replace these placeholders before submitting the final certified bundle.

For Red Hat, `spec.skips` is generated for previous certified versions. Community continues to use:

```yaml
metadata:
  annotations:
    olm.skipRange: <VERSION
```

## Known Issues

### Update Graphs

Some previous certified releases required `skips` instead of `replaces` because the replaced bundle was not present in every OpenShift index. This can happen when the supported OpenShift range changes between releases and older bundles are missing from newer indexes.

For this release flow:

- community uses `olm.skipRange: <VERSION`
- Red Hat uses explicit `spec.skips`

See the OLM update graph docs for background:

```text
https://olm.operatorframework.io/docs/concepts/olm-architecture/operator-catalog/creating-an-update-graph/
```

### SELinux During Validation

If validation fails with an error similar to:

```text
cannot find Containerfile or Dockerfile in context directory: stat /mnt/Dockerfile: permission denied
```

the container runtime may be blocked by SELinux. Adjust the local SELinux/container settings and rerun the validation target.

## Cleanup

Remove generated bundles, temporary SDK projects, and downloaded tools:

```sh
make clean
```
