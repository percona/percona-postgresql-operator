# Building and testing the Operator

## Requirements

You need to install a number of software packages on your system to satisfy the build dependencies for building the Operator and/or to run its automated tests:

* [kubectl](https://kubernetes.io/docs/tasks/tools/) - Kubernetes command-line tool
* [docker](https://www.docker.com/) - platform for developing, shipping, and running applications in containers
* [sed](https://www.gnu.org/software/sed/manual/sed.html) - CLI stream editor
* [helm](https://helm.sh/) - the package manager for Kubernetes
* [jq](https://stedolan.github.io/jq/) - command-line JSON processor
* [yq](https://github.com/mikefarah/yq) - command-line YAML processor
* [oc](https://docs.openshift.com/container-platform/4.7/cli_reference/openshift_cli/getting-started-cli.html) - Openshift command-line tool
* [gcloud](https://cloud.google.com/sdk/gcloud) - Google Cloud command-line tool

### CentOS

Run the following commands to install the required components:

```
sudo yum -y install epel-release https://repo.percona.com/yum/percona-release-latest.noarch.rpm
sudo yum -y install coreutils sed jq curl docker
sudo curl -s -L https://github.com/mikefarah/yq/releases/download/3.4.1/yq_linux_amd64 -o /usr/bin/yq
sudo chmod a+x /usr/bin/yq
curl -s -L https://github.com/openshift/origin/releases/download/v4.10.0/openshift-origin-client-tools-v4.10.0-0cbc58b-linux-64bit.tar.gz \
    | tar -C /usr/bin --strip-components 1 --wildcards -zxvpf - '*/oc' '*/kubectl'
curl -s https://get.helm.sh/helm-v3.2.4-linux-amd64.tar.gz \
    | tar -C /usr/bin --strip-components 1 -zxvpf - '*/helm'
curl https://sdk.cloud.google.com | bash
```

### MacOS

Install [Docker](https://docs.docker.com/docker-for-mac/install/), and run the following commands for the other required components:

```
brew install coreutils gnu-sed jq kubernetes-cli openshift-cli kubernetes-helm
brew install yq@3
brew link yq@3
curl https://sdk.cloud.google.com | bash
```

### Runtime requirements

Also, you need a Kubernetes platform of [supported version](https://docs.percona.com/percona-operator-for-postgresql/System-Requirements.html#officially-supported-platforms), available via [GKE](https://docs.percona.com/percona-operator-for-postgresql/gke.html), [OpenShift](https://docs.percona.com/percona-operator-for-postgresql/openshift.html) or [minikube](https://docs.percona.com/percona-operator-for-postgresql/minikube.html) to run the Operator.

**Note:** there is no need to build an image if you are going to test some already-released version.

## Building and testing the Operator

There are scripts which build the image and run tests. Both building and testing
needs some repository for the newly created docker images. If nothing is
specified, scripts use Percona's experimental repository `perconalab/percona-postgresql-operator`, which
requires decent access rights to make a push.

To specify your own repository for the Operator docker image, you can use IMAGE environment variable:

```
export IMAGE=bob/my_repository_for_test_images:K8SPG-42-fix-feature-X
```
We use linux/amd64 platform by default. To specify another platform, you can use DOCKER_DEFAULT_PLATFORM environment variable

```
export DOCKER_DEFAULT_PLATFORM=linux/amd64
```

Use the following script to build the image:

```
./e2e-tests/build
```

You can run tests one-by-one using the appropriate scripts with self-explanatory names
(see how to configure the testing infrastructure [here](#using-environment-variables-to-customize-the-testing-process)):

```
./e2e-tests/affinity/run
./e2e-tests/clone-cluster/run
./e2e-tests/demand-backup/run
./e2e-tests/init-deploy/run
./e2e-tests/monitoring/run
./e2e-tests/ns-mode/run
./e2e-tests/operator-self-healing/run
./e2e-tests/recreate/run
./e2e-tests/scaling/run
./e2e-tests/scheduled-backup/run
./e2e-tests/self-healing/run
./e2e-tests/smart-update/run
./e2e-tests/tls-check/run
./e2e-tests/upgrade/run
./e2e-tests/users/run
./e2e-tests/version-service/run
....
```

## Using environment variables to customize the testing process

### Re-declaring default image names

You can use environment variables declared in `examples/envs.sh` to control aspects of the Operator installation.
The file contains the list of variables with comments.
