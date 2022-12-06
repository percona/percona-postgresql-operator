
# Default values if not already set
NAME ?= percona-postgresql-operator
VERSION ?= $(shell git rev-parse --abbrev-ref HEAD | sed -e 's^/^-^g; s^[.]^-^g;' | tr '[:upper:]' '[:lower:]')
ROOT_REPO ?= ${PWD}
IMAGE_TAG_BASE ?= perconalab/$(NAME)
IMAGE ?= $(IMAGE_TAG_BASE):$(VERSION)

PGOROOT ?= $(CURDIR)
PGO_BASEOS ?= ubi8
PGO_IMAGE_PREFIX ?= crunchydata
PGO_IMAGE_TAG ?= $(PGO_BASEOS)-$(PGO_VERSION)
PGO_VERSION ?= $(shell git describe --tags)
PGO_PG_VERSION ?= 14
PGO_PG_FULLVERSION ?= 14.5
PGO_KUBE_CLIENT ?= kubectl

RELTMPDIR=/tmp/release.$(PGO_VERSION)
RELFILE=/tmp/postgres-operator.$(PGO_VERSION).tar.gz

# Valid values: buildah (default), docker
IMGBUILDER ?= buildah
# Determines whether or not rootless builds are enabled
IMG_ROOTLESS_BUILD ?= false
# The utility to use when pushing/pulling to and from an image repo (e.g. docker or buildah)
IMG_PUSHER_PULLER ?= docker
# Determines whether or not images should be pushed to the local docker daemon when building with
# a tool other than docker (e.g. when building with buildah)
IMG_PUSH_TO_DOCKER_DAEMON ?= true
# Defines the sudo command that should be prepended to various build commands when rootless builds are
# not enabled
IMGCMDSUDO=
ifneq ("$(IMG_ROOTLESS_BUILD)", "true")
	IMGCMDSUDO=sudo --preserve-env
endif
IMGCMDSTEM=$(IMGCMDSUDO) buildah bud --layers $(SQUASH)

# Default the buildah format to docker to ensure it is possible to pull the images from a docker
# repository using docker (otherwise the images may not be recognized)
export BUILDAH_FORMAT ?= docker

# Allows simplification of IMGBUILDER switching
ifeq ("$(IMGBUILDER)","docker")
        IMGCMDSTEM=docker build
endif

# set the proper packager, registry and base image based on the PGO_BASEOS configured
DOCKERBASEREGISTRY=
BASE_IMAGE_OS=
ifeq ("$(PGO_BASEOS)", "ubi8")
    BASE_IMAGE_OS=ubi8-minimal
    DOCKERBASEREGISTRY=registry.access.redhat.com/
    PACKAGER=microdnf
endif

DEBUG_BUILD ?= false
GO ?= go
GO_BUILD = $(GO_CMD) build -trimpath
GO_CMD = $(GO_ENV) $(GO)
GO_TEST ?= $(GO) test
KUTTL_TEST ?= kuttl test

# Disable optimizations if creating a debug build
ifeq ("$(DEBUG_BUILD)", "true")
	GO_BUILD = $(GO_CMD) build -gcflags='all=-N -l'
endif

# To build a specific image, run 'make <name>-image' (e.g. 'make postgres-operator-image')
images = postgres-operator \
	crunchy-postgres-exporter

.PHONY: all setup clean push pull release deploy


#======= Main functions =======
all: build-docker-image

setup:
	PGOROOT='$(PGOROOT)' ./bin/get-deps.sh
	./bin/check-deps.sh

#=== postgrescluster CRD ===

# Create operator and target namespaces
createnamespaces:
	$(PGO_KUBE_CLIENT) apply -k ./config/namespace

# Delete operator and target namespaces
deletenamespaces:
	$(PGO_KUBE_CLIENT) delete -k ./config/namespace

# Install the postgrescluster CRD
install:
	$(PGO_KUBE_CLIENT) apply --server-side -k ./config/crd

# Delete the postgrescluster CRD
uninstall:
	$(PGO_KUBE_CLIENT) delete -k ./config/crd

# Deploy the PostgreSQL Operator (enables the postgrescluster controller)
deploy:
	$(PGO_KUBE_CLIENT) apply --server-side -k ./config/default

# Deploy the PostgreSQL Operator locally
deploy-dev: build-postgres-operator createnamespaces
	$(PGO_KUBE_CLIENT) apply --server-side -k ./config/dev
	hack/create-kubeconfig.sh postgres-operator pgo
	env \
		CRUNCHY_DEBUG=true \
		CHECK_FOR_UPGRADES=false \
		KUBECONFIG=hack/.kube/postgres-operator/pgo \
		$(shell $(PGO_KUBE_CLIENT) kustomize ./config/dev | \
			sed -ne '/^kind: Deployment/,/^---/ { \
				/RELATED_IMAGE_/ { N; s,.*\(RELATED_[^[:space:]]*\).*value:[[:space:]]*\([^[:space:]]*\),\1="\2",; p; }; \
			}') \
		$(foreach v,$(filter RELATED_IMAGE_%,$(.VARIABLES)),$(v)="$($(v))") \
		bin/postgres-operator

# Undeploy the PostgreSQL Operator
undeploy:
	$(PGO_KUBE_CLIENT) delete -k ./config/default


#======= Binary builds =======
build-postgres-operator:
	$(GO_BUILD) -ldflags '-X "main.versionString=$(VERSION)"' \
		-o bin/postgres-operator ./cmd/postgres-operator

build-pgo-%:
	$(info No binary build needed for $@)

build-crunchy-postgres-exporter:
	$(info No binary build needed for $@)


#======= Image builds =======
build-docker-image:
	ROOT_REPO=$(ROOT_REPO) VERSION=$(VERSION) IMAGE=$(IMAGE) $(ROOT_REPO)/e2e-tests/build

$(PGOROOT)/build/%/Dockerfile:
	$(error No Dockerfile found for $* naming pattern: [$@])

crunchy-postgres-exporter-img-build: pgo-base-$(IMGBUILDER) build-crunchy-postgres-exporter $(PGOROOT)/build/crunchy-postgres-exporter/Dockerfile
	$(IMGCMDSTEM) \
		-f $(PGOROOT)/build/crunchy-postgres-exporter/Dockerfile \
		-t $(PGO_IMAGE_PREFIX)/crunchy-postgres-exporter:$(PGO_IMAGE_TAG) \
		--build-arg BASEOS=$(PGO_BASEOS) \
		--build-arg BASEVER=$(PGO_VERSION) \
		--build-arg PACKAGER=$(PACKAGER) \
		--build-arg PGVERSION=$(PGO_PG_VERSION) \
		--build-arg PREFIX=$(PGO_IMAGE_PREFIX) \
		$(PGOROOT)

postgres-operator-img-build: build-postgres-operator $(PGOROOT)/build/postgres-operator/Dockerfile
	$(IMGCMDSTEM) \
		-f $(PGOROOT)/build/postgres-operator/Dockerfile \
		-t $(PGO_IMAGE_PREFIX)/postgres-operator:$(PGO_IMAGE_TAG) \
		--build-arg BASE_IMAGE_OS=$(BASE_IMAGE_OS) \
		--build-arg PACKAGER=$(PACKAGER) \
		--build-arg PGVERSION=$(PGO_PG_VERSION) \
		--build-arg RELVER=$(PGO_VERSION) \
		--build-arg DOCKERBASEREGISTRY=$(DOCKERBASEREGISTRY) \
		--build-arg PACKAGER=$(PACKAGER) \
		--build-arg PG_FULL=$(PGO_PG_FULLVERSION) \
		--build-arg PGVERSION=$(PGO_PG_VERSION) \
		$(PGOROOT)

%-img-buildah: %-img-build ;
# only push to docker daemon if variable PGO_PUSH_TO_DOCKER_DAEMON is set to "true"
ifeq ("$(IMG_PUSH_TO_DOCKER_DAEMON)", "true")
	$(IMGCMDSUDO) buildah push $(PGO_IMAGE_PREFIX)/$*:$(PGO_IMAGE_TAG) docker-daemon:$(PGO_IMAGE_PREFIX)/$*:$(PGO_IMAGE_TAG)
endif

%-img-docker: %-img-build ;

%-image: %-img-$(IMGBUILDER) ;

pgo-base: pgo-base-$(IMGBUILDER)

pgo-base-build: $(PGOROOT)/build/pgo-base/Dockerfile licenses
	$(IMGCMDSTEM) \
		-f $(PGOROOT)/build/pgo-base/Dockerfile \
		-t $(PGO_IMAGE_PREFIX)/pgo-base:$(PGO_IMAGE_TAG) \
		--build-arg BASE_IMAGE_OS=$(BASE_IMAGE_OS) \
		--build-arg BASEOS=$(PGO_BASEOS) \
		--build-arg RELVER=$(PGO_VERSION) \
		--build-arg DOCKERBASEREGISTRY=$(DOCKERBASEREGISTRY) \
		--build-arg PACKAGER=$(PACKAGER) \
		--build-arg PG_FULL=$(PGO_PG_FULLVERSION) \
		--build-arg PGVERSION=$(PGO_PG_VERSION) \
		$(PGOROOT)

pgo-base-buildah: pgo-base-build ;
# only push to docker daemon if variable PGO_PUSH_TO_DOCKER_DAEMON is set to "true"
ifeq ("$(IMG_PUSH_TO_DOCKER_DAEMON)", "true")
	$(IMGCMDSUDO) buildah push $(PGO_IMAGE_PREFIX)/pgo-base:$(PGO_IMAGE_TAG) docker-daemon:$(PGO_IMAGE_PREFIX)/pgo-base:$(PGO_IMAGE_TAG)
endif

pgo-base-docker: pgo-base-build


#======== Utility =======
.PHONY: check
check:
	PGO_NAMESPACE="postgres-operator" $(GO_TEST) -cover ./...

# Available versions: curl -s 'https://storage.googleapis.com/kubebuilder-tools/' | grep -o '<Key>[^<]*</Key>'
# - KUBEBUILDER_ATTACH_CONTROL_PLANE_OUTPUT=true
.PHONY: check-envtest
check-envtest: ENVTEST_USE = hack/tools/setup-envtest --bin-dir=$(CURDIR)/hack/tools/envtest use $(ENVTEST_K8S_VERSION)
check-envtest: SHELL = bash
check-envtest:
	GOBIN='$(CURDIR)/hack/tools' $(GO) install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
	@$(ENVTEST_USE) --print=overview && echo
	source <($(ENVTEST_USE) --print=env) && PGO_NAMESPACE="postgres-operator" $(GO_TEST) -count=1 -cover -tags=envtest ./...

# - PGO_TEST_TIMEOUT_SCALE=1
.PHONY: check-envtest-existing
check-envtest-existing: createnamespaces
	${PGO_KUBE_CLIENT} apply --server-side -k ./config/dev
	USE_EXISTING_CLUSTER=true PGO_NAMESPACE="postgres-operator" $(GO_TEST) -count=1 -cover -p=1 -tags=envtest ./...
	${PGO_KUBE_CLIENT} delete -k ./config/dev

# Expects operator to be running
.PHONY: check-kuttl
check-kuttl:
	${PGO_KUBE_CLIENT} ${KUTTL_TEST} \
		--config testing/kuttl/kuttl-test.yaml

.PHONY: generate-kuttl
generate-kuttl: export KUTTL_PG_VERSION ?= 14
generate-kuttl: export KUTTL_POSTGIS_VERSION ?= 3.1
generate-kuttl: export KUTTL_PSQL_IMAGE ?= registry.developers.crunchydata.com/crunchydata/crunchy-postgres:centos8-14.2-0
generate-kuttl:
	[ ! -d testing/kuttl/e2e-generated ] || rm -r testing/kuttl/e2e-generated
	[ ! -d testing/kuttl/e2e-generated-other ] || rm -r testing/kuttl/e2e-generated-other
	bash -ceu ' \
	render() { envsubst '"'"'$$KUTTL_PG_VERSION $$KUTTL_POSTGIS_VERSION $$KUTTL_PSQL_IMAGE'"'"'; }; \
	while [ $$# -gt 0 ]; do \
		source="$${1}" target="$${1/e2e/e2e-generated}"; \
		mkdir -p "$${target%/*}"; render < "$${source}" > "$${target}"; \
		shift; \
	done' - $(wildcard testing/kuttl/e2e/*/*.yaml) $(wildcard testing/kuttl/e2e-other/*/*.yaml)

.PHONY: check-generate
check-generate: generate-crd generate-deepcopy generate-rbac
	git diff --exit-code -- config/crd
	git diff --exit-code -- config/rbac
	git diff --exit-code -- pkg/apis

clean: clean-deprecated
	rm -f bin/postgres-operator
	rm -f config/rbac/role.yaml
	[ ! -d testing/kuttl/e2e-generated ] || rm -r testing/kuttl/e2e-generated
	[ ! -d testing/kuttl/e2e-generated-other ] || rm -r testing/kuttl/e2e-generated-other
	[ ! -d build/crd/generated ] || rm -r build/crd/generated
	[ ! -f hack/tools/setup-envtest ] || hack/tools/setup-envtest --bin-dir=hack/tools/envtest cleanup
	[ ! -f hack/tools/setup-envtest ] || rm hack/tools/setup-envtest
	[ ! -d hack/tools/envtest ] || rm -r hack/tools/envtest
	[ ! -n "$$(ls hack/tools)" ] || rm hack/tools/*
	[ ! -d hack/.kube ] || rm -r hack/.kube

clean-deprecated:
	@# packages used to be downloaded into the vendor directory
	[ ! -d vendor ] || rm -r vendor
	@# executables used to be compiled into the $GOBIN directory
	[ ! -n '$(GOBIN)' ] || rm -f $(GOBIN)/postgres-operator $(GOBIN)/apiserver $(GOBIN)/*pgo
	@# executables used to be in subdirectories
	[ ! -d bin/pgo-rmdata ] || rm -r bin/pgo-rmdata
	[ ! -d bin/pgo-backrest ] || rm -r bin/pgo-backrest
	[ ! -d bin/pgo-scheduler ] || rm -r bin/pgo-scheduler
	[ ! -d bin/postgres-operator ] || rm -r bin/postgres-operator
	@# keys used to be generated before install
	[ ! -d conf/pgo-backrest-repo ] || rm -r conf/pgo-backrest-repo
	[ ! -d conf/postgres-operator ] || rm -r conf/postgres-operator

push: $(images:%=push-%) ;

push-%:
	$(IMG_PUSHER_PULLER) push $(PGO_IMAGE_PREFIX)/$*:$(PGO_IMAGE_TAG)

pull: $(images:%=pull-%) ;

pull-%:
	$(IMG_PUSHER_PULLER) pull $(PGO_IMAGE_PREFIX)/$*:$(PGO_IMAGE_TAG)

generate: generate-crd generate-deepcopy generate-rbac generate-manager generate-bundle

generate-crunchy-crd:
	GOBIN='$(CURDIR)/hack/tools' ./hack/controller-generator.sh \
		crd:crdVersions='v1' \
		paths='./pkg/apis/postgres-operator.crunchydata.com/...' \
		output:dir='build/crd/crunchy/generated' # build/crd/generated/{group}_{plural}.yaml
	$(PGO_KUBE_CLIENT) kustomize ./build/crd/crunchy/ > ./config/crd/bases/postgres-operator.crunchydata.com_postgresclusters.yaml

generate-percona-crd:
	GOBIN='$(CURDIR)/hack/tools' ./hack/controller-generator.sh \
		crd:crdVersions='v1' \
		paths='./pkg/apis/pg.percona.com/...' \
		output:dir='build/crd/percona/generated' # build/crd/generated/{group}_{plural}.yaml
	$(PGO_KUBE_CLIENT) kustomize ./build/crd/percona/ > ./config/crd/bases/pg.percona.com_perconapgclusters.yaml

generate-crd: generate-crunchy-crd generate-percona-crd
	cat ./config/crd/bases/pg.percona.com_perconapgclusters.yaml > ./deploy/crd.yaml
	echo --- >> ./deploy/crd.yaml
	cat ./config/crd/bases/postgres-operator.crunchydata.com_postgresclusters.yaml >> ./deploy/crd.yaml

generate-crd-docs:
	GOBIN='$(CURDIR)/hack/tools' go install fybrik.io/crdoc@v0.5.2
	./hack/tools/crdoc \
		--resources ./config/crd/bases \
		--template ./hack/api-template.tmpl \
		--output ./docs/content/references/crd.md

generate-deepcopy:
	GOBIN='$(CURDIR)/hack/tools' ./hack/controller-generator.sh \
		object:headerFile='hack/boilerplate.go.txt' \
		paths='./pkg/apis/...'

generate-rbac:
	GOBIN='$(CURDIR)/hack/tools' ./hack/generate-rbac.sh \
		'./...' 'config/rbac/'
	$(KUSTOMIZE) build ./config/rbac/namespace/ > ./deploy/rbac.yaml

generate-manager:
	cd ./config/manager/ && $(KUSTOMIZE) edit set image postgres-operator=$(IMAGE)
	$(KUSTOMIZE) build ./config/manager/ > ./deploy/operator.yaml

generate-bundle:
	cd ./config/bundle/ && $(KUSTOMIZE) edit set image postgres-operator=$(IMAGE)
	$(KUSTOMIZE) build ./config/bundle/ > ./deploy/bundle.yaml

generate-cw-bundle:
	cd ./config/default/ && $(KUSTOMIZE) edit set image postgres-operator=$(IMAGE)
	$(KUSTOMIZE) build ./config/default/ > ./deploy/cw-bundle.yaml

VERSION_SERVICE_CLIENT_PATH = ./percona/version/service
generate-versionservice-client: swagger
	rm $(VERSION_SERVICE_CLIENT_PATH)/version.swagger.yaml || :
	curl https://raw.githubusercontent.com/Percona-Lab/percona-version-service/main/api/version.swagger.yaml --output $(VERSION_SERVICE_CLIENT_PATH)/version.swagger.yaml
	rm -rf $(VERSION_SERVICE_CLIENT_PATH)/client
	swagger generate client -f $(VERSION_SERVICE_CLIENT_PATH)/version.swagger.yaml -c $(VERSION_SERVICE_CLIENT_PATH)/client -m $(VERSION_SERVICE_CLIENT_PATH)/client/models

# Available versions: curl -s 'https://storage.googleapis.com/kubebuilder-tools/' | grep -o '<Key>[^<]*</Key>'
# - ENVTEST_K8S_VERSION=1.19.2
hack/tools/envtest: SHELL = bash
hack/tools/envtest:
	source '$(shell $(GO) list -f '{{ .Dir }}' -m 'sigs.k8s.io/controller-runtime')/hack/setup-envtest.sh' && fetch_envtest_tools $@

.PHONY: license licenses
license: licenses
licenses:
	./bin/license_aggregator.sh ./cmd/...

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@v4.5.3)

SWAGGER = $(shell pwd)/bin/swagger
swagger: ## Download swagger locally if necessary.
	$(call go-get-tool,$(SWAGGER),github.com/go-swagger/go-swagger/cmd/swagger@latest)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go install $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef
