PGO_IMAGE_NAME ?= percona-postgresql-operator
PGO_IMAGE_MAINTAINER ?= Percona
PGO_IMAGE_SUMMARY ?= Percona PostgreSQL Operator
PGO_IMAGE_DESCRIPTION ?= $(PGO_IMAGE_SUMMARY)
PGO_IMAGE_URL ?= https://github.com/percona/percona-postgresql-operator
PGO_IMAGE_PREFIX ?= localhost

PGMONITOR_DIR ?= hack/tools/pgmonitor
PGMONITOR_VERSION ?= v4.11.0
QUERIES_CONFIG_DIR ?= hack/tools/queries

# Buildah's "build" used to be "bud". Use the alias to be compatible for a while.
BUILDAH_BUILD ?= buildah bud

GO ?= CGO_ENABLED=1 go
GO_BUILD = $(GO) build -trimpath
GO_TEST ?= $(GO) test
KUTTL ?= kubectl-kuttl
KUTTL_TEST ?= $(KUTTL) test
SED := $(shell which gsed || which sed)

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-formatting the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: all
all: ## Build all images
all: build-docker-image

.PHONY: setup
setup: ## Run Setup needed to build images
setup: get-pgmonitor

.PHONY: get-pgmonitor
get-pgmonitor:
	git -C '$(dir $(PGMONITOR_DIR))' clone https://github.com/CrunchyData/pgmonitor.git || git -C '$(PGMONITOR_DIR)' fetch origin
	@git -C '$(PGMONITOR_DIR)' checkout '$(PGMONITOR_VERSION)'
	@git -C '$(PGMONITOR_DIR)' config pull.ff only
	[ -d '${QUERIES_CONFIG_DIR}' ] || mkdir -p '${QUERIES_CONFIG_DIR}'
	cp -r '$(PGMONITOR_DIR)/postgres_exporter/common/.' '${QUERIES_CONFIG_DIR}'
	cp '$(PGMONITOR_DIR)/postgres_exporter/linux/queries_backrest.yml' '${QUERIES_CONFIG_DIR}'

.PHONY: clean
clean: ## Clean resources
clean: clean-deprecated
	rm -f bin/postgres-operator
	rm -f config/rbac/role.yaml
	rm -rf licenses/*/
	[ ! -d testing/kuttl/e2e-generated ] || rm -r testing/kuttl/e2e-generated
	[ ! -d testing/kuttl/e2e-generated-other ] || rm -r testing/kuttl/e2e-generated-other
	rm -rf build/crd/generated build/crd/*/generated
	[ ! -f hack/tools/setup-envtest ] || rm hack/tools/setup-envtest
	[ ! -d hack/tools/envtest ] || { chmod -R u+w hack/tools/envtest && rm -r hack/tools/envtest; }
	[ ! -d hack/tools/pgmonitor ] || rm -rf hack/tools/pgmonitor
	[ ! -d hack/tools/external-snapshotter ] || rm -rf hack/tools/external-snapshotter
	[ ! -n "$$(ls hack/tools)" ] || rm -r hack/tools/*
	[ ! -d hack/.kube ] || rm -r hack/.kube

.PHONY: clean-deprecated
clean-deprecated: ## Clean deprecated resources
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
	@# crunchy-postgres-exporter used to live in this repo
	[ ! -d bin/crunchy-postgres-exporter ] || rm -r bin/crunchy-postgres-exporter
	[ ! -d build/crunchy-postgres-exporter ] || rm -r build/crunchy-postgres-exporter


##@ Deployment
.PHONY: createnamespaces
createnamespaces: ## Create operator and target namespaces
	kubectl apply -k ./config/namespace

.PHONY: deletenamespaces
deletenamespaces: ## Delete operator and target namespaces
	kubectl delete -k ./config/namespace

.PHONY: install
install: ## Install the postgrescluster CRD
	kubectl apply --server-side -f ./deploy/crd.yaml
	kubectl apply --server-side -f ./deploy/rbac.yaml

.PHONY: uninstall
uninstall: ## Delete the postgrescluster CRD
	kubectl delete -f ./deploy/crd.yaml
	kubectl delete -f ./deploy/rbac.yaml

.PHONY: deploy
deploy: ## Deploy the PostgreSQL Operator (enables the postgrescluster controller)
	yq eval '(.spec.template.spec.containers[] | select(.name=="operator")).image = "$(IMAGE)"' ./deploy/operator.yaml \
		| yq eval '(.spec.template.spec.containers[] | select(.name=="operator") | .env[] | select(.name=="DISABLE_TELEMETRY") | .value) = "true"' - \
		| yq eval '(.spec.template.spec.containers[] | select(.name=="operator") | .env[] | select(.name=="LOG_LEVEL") | .value) = "DEBUG"' - \
		| kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy the PostgreSQL Operator
	kubectl delete -f ./deploy/operator.yaml

.PHONY: deploy-dev
deploy-dev: ## Deploy the PostgreSQL Operator locally
deploy-dev: PGO_FEATURE_GATES ?= "TablespaceVolumes=true,CrunchyBridgeClusters=true"
deploy-dev: get-pgmonitor
deploy-dev: build-postgres-operator
deploy-dev: createnamespaces
	kubectl apply --server-side -k ./config/dev
	hack/create-kubeconfig.sh postgres-operator pgo
	env \
		QUERIES_CONFIG_DIR="${QUERIES_CONFIG_DIR}" \
		CRUNCHY_DEBUG=true \
		PGO_FEATURE_GATES="${PGO_FEATURE_GATES}" \
		CHECK_FOR_UPGRADES='$(if $(CHECK_FOR_UPGRADES),$(CHECK_FOR_UPGRADES),false)' \
		KUBECONFIG=hack/.kube/postgres-operator/pgo \
		PGO_NAMESPACE='postgres-operator' \
		PGO_INSTALLER='deploy-dev' \
		PGO_INSTALLER_ORIGIN='postgres-operator-repo' \
		BUILD_SOURCE='build-postgres-operator' \
		$(shell kubectl kustomize ./config/dev | \
			sed -ne '/^kind: Deployment/,/^---/ { \
				/RELATED_IMAGE_/ { N; s,.*\(RELATED_[^[:space:]]*\).*value:[[:space:]]*\([^[:space:]]*\),\1="\2",; p; }; \
			}') \
		$(foreach v,$(filter RELATED_IMAGE_%,$(.VARIABLES)),$(v)="$($(v))") \
		bin/postgres-operator

##@ Build - Binary
.PHONY: build-postgres-operator
build-postgres-operator: ## Build the postgres-operator binary
	$(GO_BUILD) $(\
		) --ldflags '-X "main.versionString=$(PGO_VERSION)"' $(\
		) --trimpath -o bin/postgres-operator ./cmd/postgres-operator

##@ Build - Images
.PHONY: build-postgres-operator-image
build-postgres-operator-image: ## Build the postgres-operator image
build-postgres-operator-image: PGO_IMAGE_REVISION := $(shell git rev-parse HEAD)
build-postgres-operator-image: PGO_IMAGE_TIMESTAMP := $(shell date -u +%FT%TZ)
build-postgres-operator-image: build-postgres-operator
build-postgres-operator-image: build/postgres-operator/Dockerfile
	$(if $(shell (echo 'buildah version 1.24'; $(word 1,$(BUILDAH_BUILD)) --version) | sort -Vc 2>&1), \
		$(warning WARNING: old buildah does not invalidate its cache for changed labels: \
			https://github.com/containers/buildah/issues/3517))
	$(if $(IMAGE_TAG),,	$(error missing IMAGE_TAG))
	$(strip $(BUILDAH_BUILD)) \
		--tag $(BUILDAH_TRANSPORT)$(PGO_IMAGE_PREFIX)/$(PGO_IMAGE_NAME):$(IMAGE_TAG) \
		--label name='$(PGO_IMAGE_NAME)' \
		--label build-date='$(PGO_IMAGE_TIMESTAMP)' \
		--label description='$(PGO_IMAGE_DESCRIPTION)' \
		--label maintainer='$(PGO_IMAGE_MAINTAINER)' \
		--label summary='$(PGO_IMAGE_SUMMARY)' \
		--label url='$(PGO_IMAGE_URL)' \
		--label vcs-ref='$(PGO_IMAGE_REVISION)' \
		--label vendor='$(PGO_IMAGE_MAINTAINER)' \
		--label io.k8s.display-name='$(PGO_IMAGE_NAME)' \
		--label io.k8s.description='$(PGO_IMAGE_DESCRIPTION)' \
		--label io.openshift.tags="postgresql,postgres,sql,nosql,crunchy" \
		--annotation org.opencontainers.image.authors='$(PGO_IMAGE_MAINTAINER)' \
		--annotation org.opencontainers.image.vendor='$(PGO_IMAGE_MAINTAINER)' \
		--annotation org.opencontainers.image.created='$(PGO_IMAGE_TIMESTAMP)' \
		--annotation org.opencontainers.image.description='$(PGO_IMAGE_DESCRIPTION)' \
		--annotation org.opencontainers.image.revision='$(PGO_IMAGE_REVISION)' \
		--annotation org.opencontainers.image.title='$(PGO_IMAGE_SUMMARY)' \
		--annotation org.opencontainers.image.url='$(PGO_IMAGE_URL)' \
		$(if $(PGO_VERSION),$(strip \
			--label release='$(PGO_VERSION)' \
			--label version='$(PGO_VERSION)' \
			--annotation org.opencontainers.image.version='$(PGO_VERSION)' \
		)) \
		--file $< --format docker --layers .

##@ Test
.PHONY: check
check: ## Run basic go tests with coverage output
check: get-pgmonitor
	QUERIES_CONFIG_DIR="$(CURDIR)/${QUERIES_CONFIG_DIR}" $(GO_TEST) -cover ./...

# Available versions: curl -s 'https://storage.googleapis.com/kubebuilder-tools/' | grep -o '<Key>[^<]*</Key>'
# - KUBEBUILDER_ATTACH_CONTROL_PLANE_OUTPUT=true
.PHONY: check-envtest
check-envtest: ## Run check using envtest and a mock kube api
check-envtest: ENVTEST_USE = hack/tools/setup-envtest --bin-dir=$(CURDIR)/hack/tools/envtest use $(ENVTEST_K8S_VERSION)
check-envtest: SHELL = bash
check-envtest: get-pgmonitor tools/setup-envtest
	GOBIN='$(CURDIR)/hack/tools' $(GO) install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
	@$(ENVTEST_USE) --print=overview && echo
	source <($(ENVTEST_USE) --print=env) && PGO_NAMESPACE="postgres-operator" QUERIES_CONFIG_DIR="$(CURDIR)/${QUERIES_CONFIG_DIR}" \
		$(GO_TEST) -count=1 -cover -tags=envtest ./...

# The "PGO_TEST_TIMEOUT_SCALE" environment variable (default: 1) can be set to a
# positive number that extends test timeouts. The following runs tests with 
# timeouts that are 20% longer than normal:
# make check-envtest-existing PGO_TEST_TIMEOUT_SCALE=1.2
.PHONY: check-envtest-existing
check-envtest-existing: ## Run check using envtest and an existing kube api
check-envtest-existing: get-pgmonitor
check-envtest-existing: createnamespaces
	kubectl apply --server-side -k ./config/dev
	USE_EXISTING_CLUSTER=true PGO_NAMESPACE="postgres-operator" QUERIES_CONFIG_DIR="$(CURDIR)/${QUERIES_CONFIG_DIR}" \
		$(GO_TEST) -count=1 -cover -p=1 -tags=envtest ./...
	kubectl delete -k ./config/dev

# Expects operator to be running
.PHONY: check-kuttl
check-kuttl: ## Run kuttl end-to-end tests
check-kuttl: ## example command: make check-kuttl KUTTL_TEST='
	${KUTTL_TEST} \
		--config testing/kuttl/kuttl-test.yaml

.PHONY: generate-kuttl
generate-kuttl: export KUTTL_PG_UPGRADE_FROM_VERSION ?= 15
generate-kuttl: export KUTTL_PG_UPGRADE_TO_VERSION ?= 16
generate-kuttl: export KUTTL_PG_VERSION ?= 16
generate-kuttl: export KUTTL_POSTGIS_VERSION ?= 3.4
generate-kuttl: export KUTTL_PSQL_IMAGE ?= registry.developers.crunchydata.com/crunchydata/crunchy-postgres:ubi8-16.3-1
generate-kuttl: export KUTTL_TEST_DELETE_NAMESPACE ?= kuttl-test-delete-namespace
generate-kuttl: ## Generate kuttl tests
	[ ! -d testing/kuttl/e2e-generated ] || rm -r testing/kuttl/e2e-generated
	[ ! -d testing/kuttl/e2e-generated-other ] || rm -r testing/kuttl/e2e-generated-other
	bash -ceu ' \
	case $(KUTTL_PG_VERSION) in \
	16 ) export KUTTL_BITNAMI_IMAGE_TAG=16.0.0-debian-11-r3 ;; \
	15 ) export KUTTL_BITNAMI_IMAGE_TAG=15.0.0-debian-11-r4 ;; \
	14 ) export KUTTL_BITNAMI_IMAGE_TAG=14.5.0-debian-11-r37 ;; \
	13 ) export KUTTL_BITNAMI_IMAGE_TAG=13.8.0-debian-11-r39 ;; \
	12 ) export KUTTL_BITNAMI_IMAGE_TAG=12.12.0-debian-11-r40 ;; \
	esac; \
	render() { envsubst '"'"' \
		$$KUTTL_PG_UPGRADE_FROM_VERSION $$KUTTL_PG_UPGRADE_TO_VERSION \
		$$KUTTL_PG_VERSION $$KUTTL_POSTGIS_VERSION $$KUTTL_PSQL_IMAGE \
		$$KUTTL_BITNAMI_IMAGE_TAG $$KUTTL_TEST_DELETE_NAMESPACE'"'"'; }; \
	while [ $$# -gt 0 ]; do \
		source="$${1}" target="$${1/e2e/e2e-generated}"; \
		mkdir -p "$${target%/*}"; render < "$${source}" > "$${target}"; \
		shift; \
	done' - testing/kuttl/e2e/*/*.yaml testing/kuttl/e2e-other/*/*.yaml testing/kuttl/e2e/*/*/*.yaml testing/kuttl/e2e-other/*/*/*.yaml

##@ Generate

.PHONY: check-generate
check-generate: ## Check crd, deepcopy functions, and rbac generation
check-generate: generate-crd
check-generate: generate-deepcopy
check-generate: generate-rbac
	git diff --exit-code -- config/crd
	git diff --exit-code -- config/rbac
	git diff --exit-code -- pkg/apis

.PHONY: generate
generate: ## Generate crd, crd-docs, deepcopy functions, and rbac
generate: kustomize
generate: generate-crd
generate: generate-deepcopy
generate: generate-rbac
generate: generate-manager
generate: generate-bundle
generate: generate-cw

.PHONY: generate-crunchy-crd
generate-crunchy-crd: ## Generate crd
	GOBIN='$(CURDIR)/hack/tools' ./hack/controller-generator.sh \
		crd:crdVersions='v1' \
		paths='./pkg/apis/postgres-operator.crunchydata.com/...' \
		output:dir='build/crd/crunchy/generated' # build/crd/generated/{group}_{plural}.yaml
	@
	GOBIN='$(CURDIR)/hack/tools' ./hack/controller-generator.sh \
		crd:crdVersions='v1' \
		paths='./pkg/apis/...' \
		output:dir='build/crd/pgupgrades/generated' # build/crd/{plural}/generated/{group}_{plural}.yaml
	@
	GOBIN='$(CURDIR)/hack/tools' ./hack/controller-generator.sh \
		crd:crdVersions='v1' \
		paths='./pkg/apis/...' \
		output:dir='build/crd/pgadmins/generated' # build/crd/{plural}/generated/{group}_{plural}.yaml
	@
	GOBIN='$(CURDIR)/hack/tools' ./hack/controller-generator.sh \
		crd:crdVersions='v1' \
		paths='./pkg/apis/...' \
		output:dir='build/crd/crunchybridgeclusters/generated' # build/crd/{plural}/generated/{group}_{plural}.yaml
	@
	$(KUSTOMIZE) build ./build/crd/crunchy/ > ./config/crd/bases/postgres-operator.crunchydata.com_postgresclusters.yaml
	$(KUSTOMIZE) build ./build/crd/pgupgrades > ./config/crd/bases/postgres-operator.crunchydata.com_pgupgrades.yaml
	$(KUSTOMIZE) build ./build/crd/pgadmins > ./config/crd/bases/postgres-operator.crunchydata.com_pgadmins.yaml
	$(KUSTOMIZE) build ./build/crd/crunchybridgeclusters > ./config/crd/bases/postgres-operator.crunchydata.com_crunchybridgeclusters.yaml

.PHONY: generate-deepcopy
generate-deepcopy: ## Generate deepcopy functions
	GOBIN='$(CURDIR)/hack/tools' ./hack/controller-generator.sh \
		object:headerFile='hack/boilerplate.go.txt' \
		paths='./pkg/apis/...'

.PHONY: generate-rbac
generate-rbac: ## Generate rbac
	GOBIN='$(CURDIR)/hack/tools' ./hack/generate-rbac.sh \
		'./...' 'config/rbac/'
	$(KUSTOMIZE) build ./config/rbac/namespace/ > ./deploy/rbac.yaml

generate-crd: generate-crunchy-crd generate-percona-crd
	$(KUSTOMIZE) build ./config/crd/ > ./deploy/crd.yaml

generate-percona-crd:
	go generate ./percona/...
	GOBIN='$(CURDIR)/hack/tools' ./hack/controller-generator.sh \
		crd:crdVersions='v1' \
		paths='./pkg/apis/pgv2.percona.com/...' \
		output:dir='build/crd/percona/generated' # build/crd/generated/{group}_{plural}.yaml
	$(KUSTOMIZE) build ./build/crd/percona/ > ./config/crd/bases/pgv2.percona.com_perconapgclusters.yaml

generate-manager:
	cd ./config/manager/namespace/ && $(KUSTOMIZE) edit set image postgres-operator=$(IMAGE)
	$(KUSTOMIZE) build ./config/manager/namespace/ > ./deploy/operator.yaml

generate-bundle:
	cd ./config/bundle/ && $(KUSTOMIZE) edit set image postgres-operator=$(IMAGE)
	$(KUSTOMIZE) build ./config/bundle/ > ./deploy/bundle.yaml

generate-cw: generate-cw-rbac generate-cw-manager generate-cw-bundle

generate-cw-rbac:
	$(KUSTOMIZE) build ./config/rbac/cluster/ > ./deploy/cw-rbac.yaml

generate-cw-manager:
	cd ./config/manager/cluster && $(KUSTOMIZE) edit set image postgres-operator=$(IMAGE)
	$(KUSTOMIZE) build ./config/manager/cluster/ > ./deploy/cw-operator.yaml

generate-cw-bundle:
	cd ./config/cw-bundle/ && $(KUSTOMIZE) edit set image postgres-operator=$(IMAGE)
	$(KUSTOMIZE) build ./config/cw-bundle/ > ./deploy/cw-bundle.yaml


##@ Release

.PHONY: license licenses
license: licenses
licenses: ## Aggregate license files
	./bin/license_aggregator.sh ./cmd/...

.PHONY: release-postgres-operator-image release-postgres-operator-image-labels
release-postgres-operator-image: ## Build the postgres-operator image and all its prerequisites
release-postgres-operator-image: release-postgres-operator-image-labels
release-postgres-operator-image: licenses
release-postgres-operator-image: build-postgres-operator-image
release-postgres-operator-image-labels:
	$(if $(PGO_IMAGE_DESCRIPTION),,	$(error missing PGO_IMAGE_DESCRIPTION))
	$(if $(PGO_IMAGE_MAINTAINER),, 	$(error missing PGO_IMAGE_MAINTAINER))
	$(if $(PGO_IMAGE_NAME),,       	$(error missing PGO_IMAGE_NAME))
	$(if $(PGO_IMAGE_SUMMARY),,    	$(error missing PGO_IMAGE_SUMMARY))
	$(if $(PGO_VERSION),,			$(error missing PGO_VERSION))

##@ Percona

# Default values if not already set
NAME ?= percona-postgresql-operator
VERSION ?= $(shell git rev-parse --abbrev-ref HEAD | sed -e 's^/^-^g; s^[.]^-^g;' | tr '[:upper:]' '[:lower:]')
ROOT_REPO ?= ${PWD}
IMAGE_TAG_BASE ?= perconalab/$(NAME)
IMAGE ?= $(IMAGE_TAG_BASE):$(VERSION)
PGO_VERSION ?= $(shell git describe --tags)
IMAGE_REGISTRY ?= docker.io/

# Images should contain registry. If IMAGE is provided with registry - use it. If no registry is provided - use `docker.io`
SLASH_COUNT := $(words $(subst /, ,$(IMAGE)))
ifeq ($(SLASH_COUNT),3)
  IMAGE_REGISTRY := $(word 1,$(subst /, ,$(IMAGE)))/
  $(info IMAGE is qualified: $(IMAGE))
  $(info Registry: $(IMAGE_REGISTRY))
else
  IMAGE := $(IMAGE_REGISTRY)$(IMAGE)
  $(info IMAGE reassigned to: $(IMAGE))
endif

print:
	@echo "Using image: $(IMAGE)"

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@v4.5.3)

ENVTEST ?= hack/tools/setup-envtest
tools: tools/setup-envtest
tools/setup-envtest:
	$(call go-get-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

build-docker-image: get-pgmonitor
	ROOT_REPO=$(ROOT_REPO) VERSION=$(VERSION) IMAGE=$(IMAGE) $(ROOT_REPO)/e2e-tests/build

build-extension-installer-image:
	ROOT_REPO=$(ROOT_REPO) VERSION=$(VERSION) IMAGE=$(IMAGE)-ext-installer COMPONENT=extension-installer $(ROOT_REPO)/e2e-tests/build

SWAGGER = $(shell pwd)/bin/swagger
swagger: ## Download swagger locally if necessary.
	$(call go-get-tool,$(SWAGGER),github.com/go-swagger/go-swagger/cmd/swagger@latest)

VERSION_SERVICE_CLIENT_PATH = ./percona/version/service
generate-versionservice-client: swagger
	rm $(VERSION_SERVICE_CLIENT_PATH)/version.swagger.yaml || :
	curl https://raw.githubusercontent.com/Percona-Lab/percona-version-service/main/api/version.swagger.yaml --output $(VERSION_SERVICE_CLIENT_PATH)/version.swagger.yaml
	rm -rf $(VERSION_SERVICE_CLIENT_PATH)/client
	$(SWAGGER) generate client -f $(VERSION_SERVICE_CLIENT_PATH)/version.swagger.yaml -c $(VERSION_SERVICE_CLIENT_PATH)/client -m $(VERSION_SERVICE_CLIENT_PATH)/client/models

run-local:
	./bin/postgres-operator

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

# Prepare release
PG_VER ?= $(shell grep -o "postgresVersion: .*" deploy/cr.yaml|grep -oE "[0-9]+")
include e2e-tests/release_versions
release: generate
	$(SED) -i \
    	-e "/^spec:/,/^  crVersion:/{s/crVersion: .*/crVersion: $(VERSION)/}" \
        -e "/^spec:/,/^  image:/{s#image: .*#image: $(IMAGE_REGISTRY)$(IMAGE_POSTGRESQL17)#}" \
        -e "/^    pgBouncer:/,/^      image:/{s#image: .*#image: $(IMAGE_REGISTRY)$(IMAGE_PGBOUNCER17)#}" \
        -e "/^    pgbackrest:/,/^      image:/{s#image: .*#image: $(IMAGE_REGISTRY)$(IMAGE_BACKREST17)#}" \
        -e "/extensions:/,/image:/{s#image: .*#image: $(IMAGE_REGISTRY)$(IMAGE_OPERATOR)#}" \
        -e "/^  pmm:/,/^    image:/{s#image: .*#image: $(IMAGE_REGISTRY)$(IMAGE_PMM3_CLIENT)#}" deploy/cr.yaml
	$(SED) -i -r "/Version *= \"[0-9]+\.[0-9]+\.[0-9]+\"$$/ s/[0-9]+\.[0-9]+\.[0-9]+/$(VERSION)/" pkg/apis/pgv2.percona.com/v2/perconapgcluster_types.go
	$(SED) -i \
       -e "/^spec:/,/^  image:/{s#image: .*#image: $(IMAGE_REGISTRY)$(IMAGE_UPGRADE)#}" \
       -e "/^spec:/,/^  toPostgresImage:/{s#toPostgresImage: .*#toPostgresImage: $(IMAGE_REGISTRY)$(IMAGE_POSTGRESQL17)#}" \
       -e "/^spec:/,/^  toPgBouncerImage:/{s#toPgBouncerImage: .*#toPgBouncerImage: $(IMAGE_REGISTRY)$(IMAGE_PGBOUNCER17)#}" \
       -e "/^spec:/,/^  toPgBackRestImage:/{s#toPgBackRestImage: .*#toPgBackRestImage: $(IMAGE_REGISTRY)$(IMAGE_BACKREST17)#}" deploy/upgrade.yaml

# Prepare main branch after release
MAJOR_VER := $(shell grep -oE "crVersion: .*" deploy/cr.yaml|grep -oE "[0-9]+\.[0-9]+\.[0-9]+"|cut -d'.' -f1)
MINOR_VER := $(shell grep -oE "crVersion: .*" deploy/cr.yaml|grep -oE "[0-9]+\.[0-9]+\.[0-9]+"|cut -d'.' -f2)
NEXT_VER ?= $(MAJOR_VER).$$(($(MINOR_VER) + 1)).0
after-release: generate
	$(SED) -i \
		-e "/^spec:/,/^  crVersion:/{s/crVersion: .*/crVersion: $(NEXT_VER)/}" \
		-e "/^spec:/,/^  image:/{s#image: .*#image: $(IMAGE_REGISTRY)perconalab/percona-postgresql-operator:main-ppg$(PG_VER)-postgres#}" \
		-e "/^    pgBouncer:/,/^      image:/{s#image: .*#image: $(IMAGE_REGISTRY)perconalab/percona-postgresql-operator:main-pgbouncer$(PG_VER)#}" \
		-e "/^    pgbackrest:/,/^      image:/{s#image: .*#image: $(IMAGE_REGISTRY)perconalab/percona-postgresql-operator:main-pgbackrest$(PG_VER)#}" \
		-e "/extensions:/,/image:/{s#image: .*#image: $(IMAGE_REGISTRY)perconalab/percona-postgresql-operator:main#}" \
		-e "/^  pmm:/,/^    image:/{s#image: .*#image: $(IMAGE_REGISTRY)perconalab/pmm-client:dev-latest#}" deploy/cr.yaml
	$(SED) -i -r "/Version *= \"[0-9]+\.[0-9]+\.[0-9]+\"$$/ s/[0-9]+\.[0-9]+\.[0-9]+/$(NEXT_VER)/" pkg/apis/pgv2.percona.com/v2/perconapgcluster_types.go
	$(SED) -i \
		-e "/^spec:/,/^  image:/{s#image: .*#image: $(IMAGE_REGISTRY)perconalab/percona-postgresql-operator:main-upgrade#}" \
		-e "/^spec:/,/^  toPostgresImage:/{s#toPostgresImage: .*#toPostgresImage: $(IMAGE_REGISTRY)perconalab/percona-postgresql-operator:main-ppg$(PG_VER)-postgres#}" \
		-e "/^spec:/,/^  toPgBouncerImage:/{s#toPgBouncerImage: .*#toPgBouncerImage: $(IMAGE_REGISTRY)perconalab/percona-postgresql-operator:main-pgbouncer$(PG_VER)#}" \
		-e "/^spec:/,/^  toPgBackRestImage:/{s#toPgBackRestImage: .*#toPgBackRestImage: $(IMAGE_REGISTRY)perconalab/percona-postgresql-operator:main-pgbackrest$(PG_VER)#}" deploy/upgrade.yaml
