PGMONITOR_DIR ?= hack/tools/pgmonitor
PGMONITOR_VERSION ?= v4.11.0
QUERIES_CONFIG_DIR ?= hack/tools/queries

GO ?= CGO_ENABLED=1 go
GO_BUILD = $(GO) build -trimpath
GO_TEST ?= $(GO) test
KUTTL ?= kubectl-kuttl
KUTTL_TEST ?= $(KUTTL) test
SED := $(shell which gsed || which sed)

# CRDs without descriptions are used in Helm and Bundles to avoid hitting the maximum file size limit.
CRD_OPTIONS ?= crd:crdVersions='v1'
CRD_OPTIONS_WITHOUT_DESCRIPTION = crd:crdVersions='v1',maxDescLen=0

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
all: build

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
	rm -f bin/postgres-operator
	rm -f config/rbac/role.yaml
	rm -rf licenses/*/
	rm -rf build/crd/generated build/crd/*/generated
	[ ! -f hack/tools/setup-envtest ] || rm hack/tools/setup-envtest
	[ ! -d hack/tools/envtest ] || { chmod -R u+w hack/tools/envtest && rm -r hack/tools/envtest; }
	[ ! -d hack/tools/pgmonitor ] || rm -rf hack/tools/pgmonitor
	[ ! -d hack/tools/external-snapshotter ] || rm -rf hack/tools/external-snapshotter
	[ ! -n "$$(ls hack/tools)" ] || rm -r hack/tools/*
	[ ! -d hack/.kube ] || rm -r hack/.kube

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

##@ Test
GOLANGCI_LINT_VERSION ?= v2.12.2

.PHONY: lint
lint: ## Run golangci-lint (same as CI)
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout 5m --max-issues-per-linter 0 --max-same-issues 0

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

##@ Generate

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
		$(CRD_OPTIONS) \
		paths='./pkg/apis/upstream.pgv2.percona.com/...' \
		output:dir='build/crd/crunchy/generated' # build/crd/generated/{group}_{plural}.yaml
	@
	GOBIN='$(CURDIR)/hack/tools' ./hack/controller-generator.sh \
		$(CRD_OPTIONS) \
		paths='./pkg/apis/...' \
		output:dir='build/crd/pgupgrades/generated' # build/crd/{plural}/generated/{group}_{plural}.yaml
	@
	GOBIN='$(CURDIR)/hack/tools' ./hack/controller-generator.sh \
		$(CRD_OPTIONS) \
		paths='./pkg/apis/...' \
		output:dir='build/crd/pgadmins/generated' # build/crd/{plural}/generated/{group}_{plural}.yaml
	@
	GOBIN='$(CURDIR)/hack/tools' ./hack/controller-generator.sh \
		$(CRD_OPTIONS) \
		paths='./pkg/apis/...' \
		output:dir='build/crd/crunchybridgeclusters/generated' # build/crd/{plural}/generated/{group}_{plural}.yaml
	@
	$(KUSTOMIZE) build ./build/crd/crunchy/ > ./config/crd/bases/upstream.pgv2.percona.com_postgresclusters.yaml
	$(KUSTOMIZE) build ./build/crd/pgupgrades > ./config/crd/bases/upstream.pgv2.percona.com_pgupgrades.yaml
	$(KUSTOMIZE) build ./build/crd/pgadmins > ./config/crd/bases/upstream.pgv2.percona.com_pgadmins.yaml
	$(KUSTOMIZE) build ./build/crd/crunchybridgeclusters > ./config/crd/bases/upstream.pgv2.percona.com_crunchybridgeclusters.yaml

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

.PHONY: generate-crd
generate-crd: generate-crunchy-crd generate-percona-crd
	$(KUSTOMIZE) build ./config/crd/ > ./deploy/crd.yaml

.PHONY: generate-crd-without-description
generate-crd-without-description: CRD_OPTIONS = $(CRD_OPTIONS_WITHOUT_DESCRIPTION)
generate-crd-without-description: kustomize generate-crd

.PHONY: generate-percona-crd
generate-percona-crd:
	go generate ./percona/...
	GOBIN='$(CURDIR)/hack/tools' ./hack/controller-generator.sh \
		$(CRD_OPTIONS) \
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

##@ Percona

# Default values if not already set
ifeq (undefined,$(origin REGISTRY_NAME))
  $(info REGISTRY_NAME is not set)
else ifeq (undefined,$(origin IMAGE))
  $(info IMAGE is not set)
else
  IMAGE := $(REGISTRY_NAME)/$(IMAGE)
  $(info Combined IMAGE: $(IMAGE))
endif

NAME ?= percona-postgresql-operator
VERSION ?= $(shell git rev-parse --abbrev-ref HEAD | sed -e 's^/^-^g; s^[.]^-^g;' | tr '[:upper:]' '[:lower:]')
ROOT_REPO ?= ${PWD}
IMAGE_TAG_BASE ?= perconalab/$(NAME)
IMAGE ?= $(IMAGE_TAG_BASE):$(VERSION)
PGO_VERSION ?= $(shell git describe --tags)
REGISTRY_NAME ?= docker.io
REGISTRY_NAME_FULL = $(REGISTRY_NAME)/

generate:
ifneq (,$(filter percona/% perconalab/%,$(IMAGE)))
  ifeq (,$(findstring docker.io/,$(IMAGE)))
    IMAGE := $(REGISTRY_NAME_FULL)$(IMAGE)
    $(info Updated IMAGE to: $(IMAGE))
  else
    $(info IMAGE already qualified: $(IMAGE))
  endif
else
  $(info Skipping: IMAGE does not match percona/perconalab)
endif
$(info $(IMAGE))

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@v4.5.3)

ENVTEST ?= hack/tools/setup-envtest
tools: tools/setup-envtest
tools/setup-envtest:
	$(call go-get-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

build: get-pgmonitor
	ROOT_REPO=$(ROOT_REPO) VERSION=$(VERSION) IMAGE=$(IMAGE) $(ROOT_REPO)/e2e-tests/build

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

update-version:
	echo $(NEXT_VER) > percona/version/version.txt

# Prepare release
PG_VER ?= $(shell grep -o "postgresVersion: .*" deploy/cr.yaml|grep -oE "[0-9]+")
CERT_MANAGER_VER := $(shell grep -Eo "cert-manager v.*" go.mod|grep -Eo "[0-9]+\.[0-9]+\.[0-9]+")
include e2e-tests/release_versions
release: generate license
	$(SED) -i "/CERT_MANAGER_VER/s/CERT_MANAGER_VER=\".*/CERT_MANAGER_VER=\"$(CERT_MANAGER_VER)\"/" e2e-tests/functions
	$(SED) -i \
		-e "/^spec:/,/^  crVersion:/{s/crVersion: .*/crVersion: $(VERSION)/}" \
		-e "/^spec:/,/^  image:/{/^#/! s#image: .*#image: $(REGISTRY_NAME_FULL)$(IMAGE_POSTGRESQL18)#}" \
		-e "s|    image: docker.io/perconalab/percona-postgresql-operator:main|    image: $(IMAGE)|" \
		-e "/^    pgBouncer:/,/^      image:/{s#image: .*#image: $(REGISTRY_NAME_FULL)$(IMAGE_PGBOUNCER18)#}" \
		-e "/^    pgbackrest:/,/^      image:/{s#image: .*#image: $(REGISTRY_NAME_FULL)$(IMAGE_BACKREST18)#}" \
		-e "/extensions:/,/image:/{s#image: .*#image: $(REGISTRY_NAME_FULL)$(IMAGE_OPERATOR)#}" \
		-e "/^  pmm:/,/^    image:/{s#image: .*#image: $(REGISTRY_NAME_FULL)$(IMAGE_PMM3_CLIENT)#}" deploy/cr.yaml
	$(SED) -i -r "/Version *= \"[0-9]+\.[0-9]+\.[0-9]+\"$$/ s/[0-9]+\.[0-9]+\.[0-9]+/$(VERSION)/" pkg/apis/pgv2.percona.com/v2/perconapgcluster_types.go
	$(SED) -i \
       -e "/^spec:/,/^  image:/{s#image: .*#image: $(REGISTRY_NAME_FULL)$(IMAGE_UPGRADE)#}" \
       -e "/^spec:/,/^  toPostgresImage:/{s#toPostgresImage: .*#toPostgresImage: $(REGISTRY_NAME_FULL)$(IMAGE_POSTGRESQL18)#}" \
       -e "/^spec:/,/^  toPgBouncerImage:/{s#toPgBouncerImage: .*#toPgBouncerImage: $(REGISTRY_NAME_FULL)$(IMAGE_PGBOUNCER18)#}" \
       -e "/^spec:/,/^  toPgBackRestImage:/{s#toPgBackRestImage: .*#toPgBackRestImage: $(REGISTRY_NAME_FULL)$(IMAGE_BACKREST18)#}" deploy/upgrade.yaml

# Update sidecars
	$(SED) -i -r "s#pgv2.percona.com/version: [0-9]+\.[0-9]+\.[0-9]+#pgv2.percona.com/version: $(VERSION)#" e2e-tests/tests/sidecars/01-assert.yaml

# Prepare main branch after release
CURRENT_VERSION := $(shell grep -oE "crVersion: [0-9]+\.[0-9]+\.[0-9]+" deploy/cr.yaml | grep -oE "[0-9]+\.[0-9]+\.[0-9]+")
MAJOR_VER       := $(word 1,$(subst ., ,$(CURRENT_VERSION)))
MINOR_VER       := $(word 2,$(subst ., ,$(CURRENT_VERSION)))
NEXT_VER        := $(MAJOR_VER).$(shell expr $(MINOR_VER) + 1).0
PREV1_VERSION   := $(MAJOR_VER).$(shell expr $(MINOR_VER) - 1).0
PREV2_VERSION   := $(MAJOR_VER).$(shell expr $(MINOR_VER) - 2).0

.PHONY: after-release after-release-versions
after-release: update-version generate after-release-versions
	$(SED) -i \
		-e "/^spec:/,/^  crVersion:/{s/crVersion: .*/crVersion: $(NEXT_VER)/}" \
		-e "/^spec:/,/^  image:/{s#image: .*#image: $(REGISTRY_NAME_FULL)perconalab/percona-postgresql-operator:main-ppg$(PG_VER)-postgres#}" \
		-e "/initContainer:/,/image:/{s#image: .*#image: $(REGISTRY_NAME_FULL)perconalab/percona-postgresql-operator:main#}" \
		-e "/^    pgBouncer:/,/^      image:/{s#image: .*#image: $(REGISTRY_NAME_FULL)perconalab/percona-postgresql-operator:main-pgbouncer$(PG_VER)#}" \
		-e "/^    pgbackrest:/,/^      image:/{s#image: .*#image: $(REGISTRY_NAME_FULL)perconalab/percona-postgresql-operator:main-pgbackrest$(PG_VER)#}" \
		-e "/extensions:/,/image:/{s#image: .*#image: $(REGISTRY_NAME_FULL)perconalab/percona-postgresql-operator:main#}" \
		-e "/^  pmm:/,/^    image:/{s#image: .*#image: $(REGISTRY_NAME_FULL)perconalab/pmm-client:3-dev-latest#}" deploy/cr.yaml percona/controller/testdata/sidecar-resources-cr.yaml
	$(SED) -i -r "/Version *= \"[0-9]+\.[0-9]+\.[0-9]+\"$$/ s/[0-9]+\.[0-9]+\.[0-9]+/$(NEXT_VER)/" pkg/apis/pgv2.percona.com/v2/perconapgcluster_types.go
	$(SED) -i \
		-e "/^spec:/,/^  image:/{s#image: .*#image: $(REGISTRY_NAME_FULL)perconalab/percona-postgresql-operator:main-upgrade#}" \
		-e "/^spec:/,/^  toPostgresImage:/{s#toPostgresImage: .*#toPostgresImage: $(REGISTRY_NAME_FULL)perconalab/percona-postgresql-operator:main-ppg$(PG_VER)-postgres#}" \
		-e "/^spec:/,/^  toPgBouncerImage:/{s#toPgBouncerImage: .*#toPgBouncerImage: $(REGISTRY_NAME_FULL)perconalab/percona-postgresql-operator:main-pgbouncer$(PG_VER)#}" \
		-e "/^spec:/,/^  toPgBackRestImage:/{s#toPgBackRestImage: .*#toPgBackRestImage: $(REGISTRY_NAME_FULL)perconalab/percona-postgresql-operator:main-pgbackrest$(PG_VER)#}" deploy/upgrade.yaml

# Update sidecars
	$(SED) -i -r "s#pgv2.percona.com/version: [0-9]+\.[0-9]+\.[0-9]+#pgv2.percona.com/version: $(NEXT_VER)#" e2e-tests/tests/sidecars/01-assert.yaml

# Update upgrade-consistency
	$(SED) -i "s/$(PREV2_VERSION)/$(PREV1_VERSION)/g" e2e-tests/tests/upgrade-consistency/01-*.yaml
	$(SED) -i "s/$(PREV1_VERSION)/$(CURRENT_VERSION)/g" e2e-tests/tests/upgrade-consistency/02-*.yaml
	$(SED) -i "s/$(CURRENT_VERSION)/$(NEXT_VER)/g" e2e-tests/tests/upgrade-consistency/03-*.yaml e2e-tests/tests/init-deploy/05-assert.yaml

after-release-versions:
	$(SED) -i \
		-e "s#^IMAGE_OPERATOR=.*#IMAGE_OPERATOR=$(IMAGE_TAG_BASE):main#" \
		-e "s#^IMAGE_POSTGRESQL14=.*#IMAGE_POSTGRESQL14=$(IMAGE_TAG_BASE):main-ppg14-postgres#" \
		-e "s#^IMAGE_PGBOUNCER14=.*#IMAGE_PGBOUNCER14=$(IMAGE_TAG_BASE):main-pgbouncer14#" \
		-e "s#^IMAGE_POSTGIS14=.*#IMAGE_POSTGIS14=$(IMAGE_TAG_BASE):main-ppg14-postgres-gis#" \
		-e "s#^IMAGE_BACKREST14=.*#IMAGE_BACKREST14=$(IMAGE_TAG_BASE):main-pgbackrest14#" \
		-e "s#^IMAGE_POSTGRESQL15=.*#IMAGE_POSTGRESQL15=$(IMAGE_TAG_BASE):main-ppg15-postgres#" \
		-e "s#^IMAGE_PGBOUNCER15=.*#IMAGE_PGBOUNCER15=$(IMAGE_TAG_BASE):main-pgbouncer15#" \
		-e "s#^IMAGE_POSTGIS15=.*#IMAGE_POSTGIS15=$(IMAGE_TAG_BASE):main-ppg15-postgres-gis#" \
		-e "s#^IMAGE_BACKREST15=.*#IMAGE_BACKREST15=$(IMAGE_TAG_BASE):main-pgbackrest15#" \
		-e "s#^IMAGE_POSTGRESQL16=.*#IMAGE_POSTGRESQL16=$(IMAGE_TAG_BASE):main-ppg16-postgres#" \
		-e "s#^IMAGE_PGBOUNCER16=.*#IMAGE_PGBOUNCER16=$(IMAGE_TAG_BASE):main-pgbouncer16#" \
		-e "s#^IMAGE_POSTGIS16=.*#IMAGE_POSTGIS16=$(IMAGE_TAG_BASE):main-ppg16-postgres-gis#" \
		-e "s#^IMAGE_BACKREST16=.*#IMAGE_BACKREST16=$(IMAGE_TAG_BASE):main-pgbackrest16#" \
		-e "s#^IMAGE_POSTGRESQL17=.*#IMAGE_POSTGRESQL17=$(IMAGE_TAG_BASE):main-ppg17-postgres#" \
		-e "s#^IMAGE_PGBOUNCER17=.*#IMAGE_PGBOUNCER17=$(IMAGE_TAG_BASE):main-pgbouncer17#" \
		-e "s#^IMAGE_POSTGIS17=.*#IMAGE_POSTGIS17=$(IMAGE_TAG_BASE):main-ppg17-postgres-gis#" \
		-e "s#^IMAGE_BACKREST17=.*#IMAGE_BACKREST17=$(IMAGE_TAG_BASE):main-pgbackrest17#" \
		-e "s#^IMAGE_POSTGRESQL18=.*#IMAGE_POSTGRESQL18=$(IMAGE_TAG_BASE):main-ppg18-postgres#" \
		-e "s#^IMAGE_PGBOUNCER18=.*#IMAGE_PGBOUNCER18=$(IMAGE_TAG_BASE):main-pgbouncer18#" \
		-e "s#^IMAGE_POSTGIS18=.*#IMAGE_POSTGIS18=$(IMAGE_TAG_BASE):main-ppg18-postgres-gis#" \
		-e "s#^IMAGE_BACKREST18=.*#IMAGE_BACKREST18=$(IMAGE_TAG_BASE):main-pgbackrest18#" \
		-e "s#^IMAGE_UPGRADE=.*#IMAGE_UPGRADE=$(IMAGE_TAG_BASE):main-upgrade#" \
		-e "s#^IMAGE_PMM_CLIENT=.*#IMAGE_PMM_CLIENT=perconalab/pmm-client:dev-latest#" \
		-e "s#^IMAGE_PMM_SERVER=.*#IMAGE_PMM_SERVER=perconalab/pmm-server:dev-latest#" \
		-e "s#^IMAGE_PMM3_CLIENT=.*#IMAGE_PMM3_CLIENT=perconalab/pmm-client:3-dev-latest#" \
		-e "s#^IMAGE_PMM3_SERVER=.*#IMAGE_PMM3_SERVER=perconalab/pmm-server:3-dev-latest#" \
		e2e-tests/release_versions
