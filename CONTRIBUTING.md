# Contributing to Percona Operator for PostgreSQL

We welcome contributions to the Percona Operator for PostgreSQL project and we're glad that you would like to become a Percona community member and participate in keeping open source open. For you to help us improve the Operator, please follow the guidelines below.

## Prerequisites

Before submitting code contributions, complete the following prerequisites first.

### 1. Sign the CLA

Before you can contribute, we kindly ask you to sign our [Contributor License Agreement](https://cla-assistant.io/percona/percona-postgresql-operator) (CLA). You can do this using your GitHub account and one click.

### 2. Code of Conduct

Please make sure to read and observe the [Contribution Policy](code-of-conduct.md).

## Submitting a pull request

### 1. Making a bug report

We track improvement and bugfix tasks for Percona Operator project in [Jira](https://jira.percona.com/projects/K8SPG/issues).

Although not mandatory, it is a good practice to examine already open Jira issues first. For bigger contributions, we suggest creating a Jira issue and discussing it with the engineering team and community before proposing any code changes.

Another good place to discuss Percona's projects with developers and other community members is the [community forum](https://forums.percona.com).

### 2. Contributing to the source tree

Follow the workflow described below:

1. [Fork the repository on GitHub](https://docs.github.com/en/github/getting-started-with-github/fork-a-repo), clone your fork locally, and then [sync your local fork to upstream](https://docs.github.com/en/github/collaborating-with-issues-and-pull-requests/syncing-a-fork). Make sure to always sync your fork with upstream before starting to work on any changes.

2. Create a branch for changes you are planning to make. If there is a Jira ticket related to your contribution, name your branch in the following way: `<Jira issue number>-<short description>`, where the issue number is something like `K8SPG-42`.

   Create the branch in your local repo as follows:

   ```
   $ git checkout -b K8SPG-42-fix-feature-X
   ```

3. When your changes are ready, make a commit, mentioning the Jira issue in the commit message, if any:

   ```
   $ git add .
   $ git commit -m "K8SPG-42 fixed by ......"
   $ git push -u origin K8SPG-42-fix-feature-X
   ```

4. Create a pull request to the main repository on GitHub.
5. [Build a custom Operator image based on your changes](#build-a-custom-operator-image) to verify that they work
6. [Update deployment manifests](#update-deployment-manifests) to reflect your changes
7. [Run e2e tests](#run-e2e-tests) to verify your changes are stable and robust.
8. Someone from our team reviews your pull request. When the reviewer makes some comments, address any feedback that comes and update the pull request.
9. When your contribution is accepted, your pull request will be approved and merged to the main branch.

#### Build a custom Operator image based on your changes

To build a new Operator image based on your local changes, do the following:

1. Set the `IMAGE` environment variable to the your image repository and tag. For example:

   ```
   $ export IMAGE=<your-repository>/percona-postgresql-operator:<feature-XYZ>
   ```

   Replace <your-repository> and <feature-XYZ> with your own values.

2. Build the Docker image and push it to the specified repository:

   ```
   $ make build-docker-image
   ```

#### Update deployment manifests

Update the files under the `deploy/` directory to reflect any new fields in the resource API, a new image, etc. The `deploy/` directory contains the CRDs, bundles, and other manifests. 

Run the following command to update deployment manifests:

```
$ make generate VERSION=<feature-XYZ>
```

`<feature-XYZ>` here is the tag of your built image.

Next, test your custom changes by deploying the Operator on your Kubernetes cluster.

First, deploy the Operator:

```
$ kubectl apply --server-side -f deploy/bundle.yaml
```

Then, deploy a Percona PostgreSQL cluster CRD:

```
$ kubectl apply -f deploy/cr.yaml
```

#### Run end-to-end tests

The Operator repository includes a collection of end-to-end (e2e) tests under the `e2e-tests/` directory. You can run these tests on your own Kubernetes cluster to ensure that your changes are robust and stable.


To run a specific test by name, use the following command. In the example below, we run the `init-deploy` test:

```
$ kubectl kuttl test --config e2e-tests/kuttl.yaml --test "^init-deploy\$" --skip-delete
```

Replace `init-deploy` with the name of the test you want to run. 

### 3. Contributing to documentation

The workflow for documentation is similar, but we store source code for the Percona Operator for PostgreSQL documentation in a [separate repository](https://github.com/percona/k8spg-docs).

See the [Documentation Contribution Guide](https://github.com/percona/k8spg-docs/blob/main/CONTRIBUTING.md) for more information.

### 4. Container images

Find Operator Dockerfile in [build folder](build).

Our Operator uses various container images - databases, proxies, other. You can find the Dockerfiles in [percona-docker](https://github.com/percona/percona-docker) repository.

* [PostgreSQL (different versions)](https://github.com/percona/percona-docker/tree/main/postgresql-containers/build/postgres)
* [PGBouncer](https://github.com/percona/percona-docker/tree/main/postgresql-containers/build/pgbouncer)
* [pgBackrest](https://github.com/percona/percona-docker/tree/main/postgresql-containers/build/pgbackrest)

## Code review

Your contribution will be reviewed by other developers contributing to the project. The more complex your changes are, the more experts will be involved. You will receive feedback and recommendations directly on your pull request on GitHub, so keep an eye on your submission and be prepared to make further amendments. The developers might even provide some concrete suggestions on modifying your code to match the project’s expectations better.
