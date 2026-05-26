# ==============================================================================
# Percona PostgreSQL Operator - OLM Bundle Generation
# ==============================================================================

# Default target
.DEFAULT_GOAL := help
.SUFFIXES:
SHELL := /bin/bash

# ==============================================================================
# Configuration Variables
# ==============================================================================

# Project configuration
NAME ?= percona-postgresql-operator
IMAGE_TAG_OWNER ?= percona
IMAGE_TAG_BASE ?= $(IMAGE_TAG_OWNER)/$(NAME)
CONTAINER ?= docker

# Bundle configuration
OPENSHIFT_VERSIONS ?= v4.18-v4.21
PACKAGE_CHANNEL ?= stable
DOCKER_DEFAULT_PLATFORM ?= linux/amd64

# Image configuration
IMAGE ?= $(IMAGE_TAG_BASE):$(VERSION)
BUNDLE_REPO ?= $(IMAGE_TAG_BASE)
BUNDLE_DISTRO ?= community
BUNDLE_IMG ?= $(BUNDLE_REPO):$(VERSION)-community-bundle
REDHAT_OPERATOR_IMAGE ?= registry.connect.redhat.com/percona/percona-postgresql-operator:$(VERSION)

# Paths
REPO_ROOT := $(shell git rev-parse --show-toplevel)
KUSTOMIZE := $(REPO_ROOT)/bin/kustomize

# Tool versions
JQ_VERSION := 1.7.1
OPERATOR_SDK_VERSION := v1.42.2
OPM_VERSION := v1.66.0


OS_KERNEL  ?= $(shell uname -s | tr '[:upper:]' '[:lower:]')
OS_MACHINE ?= $(shell uname -m | sed 's/^x86_/amd/')
SYSTEM := $(OS_KERNEL)-$(OS_MACHINE)

export PATH := $(CURDIR)/tools/$(SYSTEM):$(PATH)

# Display colors
GREEN := $(shell tput setaf 2)
RESET := $(shell tput sgr0)

# Export variables for generate.sh
export VERSION BUNDLE_REPO OPENSHIFT_VERSIONS PACKAGE_CHANNEL DOCKER_DEFAULT_PLATFORM

# ==============================================================================
# Bundle Targets
# ==============================================================================

DISTROS := community redhat

.PHONY: bundles
bundles: ## Build all OLM bundles (community, redhat)
bundles: check-prereqs $(DISTROS:%=bundles/%)

# https://olm.operatorframework.io/docs/tasks/creating-operator-bundle/#validating-your-bundle
.PHONY: bundles/community
bundles/community: tools $(KUSTOMIZE)
	@echo "$(GREEN)Building community bundle...$(RESET)"
	cd ../../config/manager/default/ && $(KUSTOMIZE) edit set image 'postgres-operator=$(IMAGE)'
	IMAGE='$(IMAGE)' ./generate.sh community
	operator-sdk bundle validate $@ --select-optional='suite=operatorframework'
	@echo "$(GREEN)Bundle stored in installers/olm/bundles/community$(RESET)"

# https://redhat-connect.gitbook.io/certified-operator-guide/ocp-deployment/operator-metadata/reviewing-your-metadata-bundle
.PHONY: bundles/redhat
bundles/redhat: tools $(KUSTOMIZE)
	@echo "$(GREEN)Building redhat bundle...$(RESET)"
	cd ../../config/manager/default/ && $(KUSTOMIZE) edit set image 'postgres-operator=$(REDHAT_OPERATOR_IMAGE)'
	REDHAT_OPERATOR_IMAGE='$(REDHAT_OPERATOR_IMAGE)' ./generate.sh redhat
	operator-sdk bundle validate $@ --select-optional='suite=operatorframework'
	@echo "$(GREEN)Bundle stored in installers/olm/bundles/redhat$(RESET)"

# ==============================================================================
# Docker Build & Push Targets
# ==============================================================================

.PHONY: build
build: ## Build bundle image (set BUNDLE_DISTRO=community|redhat)
build: check-version bundles/$(BUNDLE_DISTRO)
	@echo "$(GREEN)Building bundle image...$(RESET)"
	$(CONTAINER) build -t $(BUNDLE_IMG) --platform=$(DOCKER_DEFAULT_PLATFORM) bundles/$(BUNDLE_DISTRO)
	@echo "$(GREEN)Bundle image built: $(BUNDLE_IMG)$(RESET)"

.PHONY: push
push: ## Push bundle image to registry
push: check-version
	@echo "$(GREEN)Pushing bundle image to registry...$(RESET)"
	$(CONTAINER) push $(BUNDLE_IMG)
	@echo "$(GREEN)Bundle image pushed: $(BUNDLE_IMG)$(RESET)"

# ==============================================================================
# Utility Targets
# ==============================================================================

.PHONY: check-prereqs
check-prereqs: check-version tools $(KUSTOMIZE)

.PHONY: check-version
check-version:
ifndef VERSION
	$(error VERSION is not set)
endif

$(KUSTOMIZE):
	$(MAKE) -C '$(REPO_ROOT)' kustomize

.PHONY: install-olm
install-olm: ## Install OLM in Kubernetes
install-olm: tools
	operator-sdk olm install

.PHONY: clean
clean: ## Remove generated files and downloaded tools
	rm -rf ./bundles ./projects ./tools

.PHONY: help
help: ## Show this help message
	@awk 'BEGIN {FS = ": ## "; printf "\n$(GREEN)Usage:$(RESET)\n  make [target]\n\n$(GREEN)Targets:$(RESET)\n"} /^[a-zA-Z_-]+: ## / {printf "  %-24s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# ==============================================================================
# Tool Management
# ==============================================================================

.PHONY: tools
tools: ## Download required tools
tools: tools/$(SYSTEM)/jq
tools/$(SYSTEM)/jq:
	install -d '$(dir $@)'
	curl -fSL -o '$@' "https://github.com/stedolan/jq/releases/download/jq-$(JQ_VERSION)/jq-$$(SYSTEM='$(SYSTEM)'; \
		case "$$SYSTEM" in \
			(linux-*) echo "$${SYSTEM/-amd/}";; (darwin-*) echo "$${SYSTEM/darwin-*/osx-amd64}";; (*) echo '$(SYSTEM)';; \
		esac)"
	chmod u+x '$@'

tools: tools/$(SYSTEM)/kubectl
tools/$(SYSTEM)/kubectl:
	install -d '$(dir $@)'
	curl -fSL -o '$@' 'https://dl.k8s.io/release/$(shell curl -Ls https://dl.k8s.io/release/stable.txt)/bin/$(OS_KERNEL)/$(OS_MACHINE)/kubectl'
	chmod u+x '$@'

# quay.io/operator-framework/operator-sdk
tools: tools/$(SYSTEM)/operator-sdk
tools/$(SYSTEM)/operator-sdk:
	install -d '$(dir $@)'
	curl -fSL -o '$@' 'https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$(OS_KERNEL)_$(OS_MACHINE)'
	chmod u+x '$@'

tools: tools/$(SYSTEM)/opm
tools/$(SYSTEM)/opm:
	install -d '$(dir $@)'
	curl -fSL -o '$@' 'https://github.com/operator-framework/operator-registry/releases/download/$(OPM_VERSION)/$(OS_KERNEL)-$(OS_MACHINE)-opm'
	chmod u+x '$@'

tools/$(SYSTEM)/venv:
	install -d '$(dir $@)'
	python3 -m venv '$@'

tools: tools/$(SYSTEM)/yq
tools/$(SYSTEM)/yq: | tools/$(SYSTEM)/venv
	'tools/$(SYSTEM)/venv/bin/python' -m pip install yq
	cd '$(dir $@)' && ln -s venv/bin/yq

# ==============================================================================
# Development Targets
# ==============================================================================

.PHONY: validate-bundles
validate-bundles: ## Build temporary bundle images and run scorecard tests in Kubernetes
validate-bundles: tools
validate-bundles: $(DISTROS:%=validate-%-image)
validate-bundles: $(DISTROS:%=validate-%-directory)

validate-%-directory:
	./validate-directory.sh 'bundles/$*'

validate-%-image:
	./validate-image.sh '$(CONTAINER)' 'bundles/$*'

.PHONY: build-bundle-images
build-bundle-images: check-version $(DISTROS:%=build-%-image)

build-%-image:
	./build-image.sh '$(CONTAINER)' 'bundles/$*' '$*' '$(VERSION)'
