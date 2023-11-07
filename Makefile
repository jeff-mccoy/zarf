# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2021-Present The Zarf Authors

# Provide a default value for the operating system architecture used in tests, e.g. " APPLIANCE_MODE=true|false make test-e2e ARCH=arm64"
ARCH ?= amd64
KEY ?= ""
######################################################################################

# Figure out which Zarf binary we should use based on the operating system we are on
ZARF_BIN := ./build/zarf
ifeq ($(OS),Windows_NT)
	ZARF_BIN := $(addsuffix .exe,$(ZARF_BIN))
else
	UNAME_S := $(shell uname -s)
	UNAME_P := $(shell uname -p)
	ifneq ($(UNAME_S),Linux)
		ifeq ($(UNAME_S),Darwin)
			ZARF_BIN := $(addsuffix -mac,$(ZARF_BIN))
		endif
		ifeq ($(UNAME_P),i386)
			ZARF_BIN := $(addsuffix -intel,$(ZARF_BIN))
		endif
		ifeq ($(UNAME_P),arm)
			ZARF_BIN := $(addsuffix -apple,$(ZARF_BIN))
		endif
	endif
endif

CLI_VERSION ?= $(if $(shell git describe --tags),$(shell git describe --tags),"UnknownVersion")
GIT_SHA := $(if $(shell git rev-parse HEAD),$(shell git rev-parse HEAD),"")
BUILD_DATE := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
BUILD_ARGS := -s -w -X 'github.com/defenseunicorns/zarf/src/config.CLIVersion=$(CLI_VERSION)' -X 'k8s.io/component-base/version.gitVersion=v0.0.0+zarf$(CLI_VERSION)' -X 'k8s.io/component-base/version.gitCommit=$(GIT_SHA)' -X 'k8s.io/component-base/version.buildDate=$(BUILD_DATE)'
.DEFAULT_GOAL := help

.PHONY: help
help: ## Display this help information
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
	  | sort | awk 'BEGIN {FS = ":.*?## "}; \
	  {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

clean: ## Clean the build directory
	rm -rf build

destroy: ## Run `zarf destroy` on the current cluster
	$(ZARF_BIN) destroy --confirm --remove-components
	rm -fr build

delete-packages: ## Delete all Zarf package tarballs in the project recursively
	find . -type f -name 'zarf-package-*' -delete

# Note: the path to the main.go file is not used due to https://github.com/golang/go/issues/51831#issuecomment-1074188363


### Build the CLIs
# hack to tell the make directives if there's been a change.
SRC_FILES := $(shell find . -type f -name '*.go')
INIT_PACKAGE_FILES := $(shell find packages -type f)

build-cli-linux-amd: build/zarf ## Build the Zarf CLI for Linux on AMD64
build/zarf: $(SRC_FILES)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(BUILD_ARGS)" -o build/zarf .

build-cli-linux-arm: build/zarf-arm ## Build the Zarf CLI for Linux on ARM
build/zarf-arm: $(SRC_FILES)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$(BUILD_ARGS)" -o build/zarf-arm .

build-cli-mac-intel: build/zarf-mac-intel ## Build the Zarf CLI for macOS on AMD64
build/zarf-mac-intel: $(SRC_FILES)
	GOOS=darwin GOARCH=amd64 go build -ldflags="$(BUILD_ARGS)" -o build/zarf-mac-intel .

build-cli-mac-apple: build/zarf-mac-apple ## Build the Zarf CLI for macOS on ARM
build/zarf-mac-apple: $(SRC_FILES)
	GOOS=darwin GOARCH=arm64 go build -ldflags="$(BUILD_ARGS)" -o build/zarf-mac-apple .

build-cli-windows-amd: build/zarf.exe ## Build the Zarf CLI for Windows on AMD64
build/zarf.exe: $(SRC_FILES)
	GOOS=windows GOARCH=amd64 go build -ldflags="$(BUILD_ARGS)" -o build/zarf.exe .

build-cli-windows-arm: build/zarf-arm.exe ## Build the Zarf CLI for Windows on ARM
build/zarf-arm.exe: $(SRC_FILES)
	GOOS=windows GOARCH=arm64 go build -ldflags="$(BUILD_ARGS)" -o build/zarf-arm.exe .

build-cli-linux: build-cli-linux-amd build-cli-linux-arm ## Build the Zarf CLI for Linux on AMD64 and ARM

build-cli: build-cli-linux build-cli-mac-intel build-cli-mac-apple build-cli-windows-amd build-cli-windows-arm ## Build the CLI

docs-and-schema: ## Generate the Zarf Documentation and Schema
	hack/gen-cli-docs.sh
	ZARF_CONFIG=hack/empty-config.toml hack/create-zarf-schema.sh

# INTERNAL: a shim used to build the agent image only if needed on Windows using the `test` command
init-package-local-agent:
	@test "$(AGENT_IMAGE_TAG)" != "local" || $(MAKE) build-local-agent-image

build-local-agent-image-amd64: build-cli-linux-amd
	@cp build/zarf build/zarf-linux-amd64

build-local-agent-image-arm64: build-cli-linux-arm
	@cp build/zarf-arm build/zarf-linux-arm64

build-local-agent-image: build-local-agent-image-$(ARCH)
	@docker buildx build --load --platform linux/$(ARCH) --tag ghcr.io/defenseunicorns/zarf/agent:local .

build/zarf-init-$(ARCH)-$(CLI_VERSION).tar.zst: zarf.yaml $(INIT_PACKAGE_FILES)
	@test -s $@ || $(ZARF_BIN) package create -o build -a $(ARCH) --confirm .

init-package: $(ZARF_BIN) build/zarf-init-$(ARCH)-$(CLI_VERSION).tar.zst ## Create the zarf init package (must `brew install coreutils` on macOS and have `docker` first)

# INTERNAL: used to build a release version of the init package with a specific agent image
release-init-package:
	$(ZARF_BIN) package create -o build -a $(ARCH) --set AGENT_IMAGE_TAG=$(AGENT_IMAGE_TAG) --confirm .

# INTERNAL: used to build an iron bank version of the init package with an ib version of the registry image
ib-init-package: build-cli
	$(ZARF_BIN) package create -o build -a $(ARCH) --confirm . \
		--set REGISTRY_IMAGE_DOMAIN="registry1.dso.mil/" \
		--set REGISTRY_IMAGE="ironbank/opensource/docker/registry-v2" \
		--set REGISTRY_IMAGE_TAG="2.8.3"

# INTERNAL: used to publish the init package
publish-init-package:
	$(ZARF_BIN) package publish build/zarf-init-$(ARCH)-$(CLI_VERSION).tar.zst oci://$(REPOSITORY_URL)
	$(ZARF_BIN) package publish . oci://$(REPOSITORY_URL)

build-examples: build-cli ## Build all of the example packages
	@test -s ./build/zarf-package-dos-games-$(ARCH)-1.0.0.tar.zst || $(ZARF_BIN) package create examples/dos-games -o build -a $(ARCH) --confirm

	@test -s ./build/zarf-package-manifests-$(ARCH)-0.0.1.tar.zst || $(ZARF_BIN) package create examples/manifests -o build -a $(ARCH) --confirm

	@test -s ./build/zarf-package-component-actions-$(ARCH).tar.zst || $(ZARF_BIN) package create examples/component-actions -o build -a $(ARCH) --confirm

	@test -s ./build/zarf-package-component-choice-$(ARCH).tar.zst || $(ZARF_BIN) package create examples/component-choice -o build -a $(ARCH) --confirm

	@test -s ./build/zarf-package-variables-$(ARCH).tar.zst || $(ZARF_BIN) package create examples/variables --set NGINX_VERSION=1.23.3 -o build -a $(ARCH) --confirm

	@test -s ./build/zarf-package-kiwix-$(ARCH)-3.5.0.tar || $(ZARF_BIN) package create examples/kiwix -o build -a $(ARCH) --confirm

	@test -s ./build/zarf-package-git-data-$(ARCH)-0.0.1.tar.zst || $(ZARF_BIN) package create examples/git-data -o build -a $(ARCH) --confirm

	@test -s ./build/zarf-package-helm-charts-$(ARCH)-0.0.1.tar.zst || $(ZARF_BIN) package create examples/helm-charts -o build -a $(ARCH) --confirm

	@test -s ./build/zarf-package-podinfo-flux-$(ARCH).tar.zst || $(ZARF_BIN) package create examples/podinfo-flux -o build -a $(ARCH) --confirm

	@test -s ./build/zarf-package-argocd-$(ARCH).tar.zst || $(ZARF_BIN) package create examples/argocd -o build -a $(ARCH) --confirm

	@test -s ./build/zarf-package-yolo-$(ARCH).tar.zst || $(ZARF_BIN) package create examples/yolo -o build -a $(ARCH) --confirm

	@test -s ./build/zarf-package-component-webhooks-$(ARCH)-0.0.1.tar.zst || $(ZARF_BIN) package create examples/component-webhooks -o build -a $(ARCH) --confirm

build-injector-linux: ## Build the Zarf injector for AMD64 and ARM64
	docker run --rm --user "$(id -u)":"$(id -g)" -v $$PWD/src/injector:/usr/src/zarf-injector -w /usr/src/zarf-injector rust:1.71.0-bookworm make build-injector-linux

## NOTE: Requires an existing cluster or the env var APPLIANCE_MODE=true
.PHONY: test-e2e
test-e2e: init-package build-examples ## Run all of the core Zarf CLI E2E tests (builds any deps that aren't present)
	cd src/test/e2e && go test -failfast -v -timeout 35m

## NOTE: Requires an existing cluster
.PHONY: test-external
test-external: init-package ## Run the Zarf CLI E2E tests for an external registry and cluster
	@test -s ./build/zarf-package-podinfo-flux-$(ARCH).tar.zst || $(ZARF_BIN) package create examples/podinfo-flux -o build -a $(ARCH) --confirm
	@test -s ./build/zarf-package-argocd-$(ARCH).tar.zst || $(ZARF_BIN) package create examples/argocd -o build -a $(ARCH) --confirm
	cd src/test/external && go test -failfast -v -timeout 30m

## NOTE: Requires an existing cluster
.PHONY: test-upgrade
test-upgrade: ## Run the Zarf CLI E2E tests for an external registry and cluster
	@test -s $(ZARF_BIN) || $(MAKE) build-cli
	[ -n "$(shell zarf version)" ] || (echo "Zarf must be installed prior to the upgrade test" && exit 1)
	[ -n "$(shell zarf package list 2>&1 | grep test-upgrade-package)" ] || (echo "Zarf must be initialized and have the 6.3.3 upgrade-test package installed prior to the upgrade test" && exit 1)
	@test -s "zarf-package-test-upgrade-package-amd64-6.3.4.tar.zst" || zarf package create src/test/upgrade/ --set PODINFO_VERSION=6.3.4 --confirm
	cd src/test/upgrade && go test -failfast -v -timeout 30m

.PHONY: test-unit
test-unit: ## Run unit tests
	cd src/pkg && go test ./... -failfast -v -timeout 30m
	cd src/internal && go test ./... -failfast -v timeout 30m
	cd src/extensions/bigbang && go test ./. -failfast -v timeout 30m

# INTERNAL: used to test that a dev has ran `make docs-and-schema` in their PR
test-docs-and-schema:
	$(MAKE) docs-and-schema
	hack/check-zarf-docs-and-schema.sh

# INTERNAL: used to test for new CVEs that may have been introduced
test-cves:
	go run main.go tools sbom packages . -o json --exclude './docs-website' --exclude './examples' | grype --fail-on low

cve-report: ## Create a CVE report for the current project (must `brew install grype` first)
	go run main.go tools sbom packages . -o json --exclude './docs-website' --exclude './examples' | grype -o template -t hack/.templates/grype.tmpl > build/zarf-known-cves.csv

lint-go: ## Run revive to lint the go code (must `brew install revive` first)
	revive -config revive.toml -exclude src/cmd/viper.go -formatter stylish ./src/...
