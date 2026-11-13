.PHONY: all full help fmt fix lint vuln build examples test benchmark bench \
	fuzz integration coverage coverage-html install-tools version clean

GO           ?= go
PKGS         := ./...
COVERPKG     := ./...
FMT_DIRS     := .
VERSION      ?= $(shell git describe --tags --always --dirty --match=v* 2>/dev/null || cat .version 2>/dev/null || echo dev)
BENCH        ?= .
FUZZTIME     ?= 10s
EXAMPLE_SRCS := $(wildcard examples/*/main.go)
EXAMPLES     := $(foreach dir,$(patsubst %/main.go,%,$(EXAMPLE_SRCS)),$(dir)/$(notdir $(dir)))

# Verbosity:
#   V=0 (default): one status line per operation; command output shown only on error.
#   V=1:           status lines + standard tool output (e.g. package test summaries).
#   V=2:           echoed commands + verbose flags (-v).
VERBOSE  ?= 0
V        ?= $(VERBOSE)
ifeq ($(V),0)
Q        := @
RUN      := @quiet() { out=$$("$$@" 2>&1) || { printf '%s\n' "$$out" >&2; exit 1; }; }; quiet
TEST_V   :=
VULN_V   :=
else ifeq ($(V),1)
Q        := @
RUN      := @
TEST_V   :=
VULN_V   :=
else
Q        :=
RUN      :=
TEST_V   := -v
VULN_V   := -show verbose
endif

# Tool discovery (PATH -> GOPATH/bin -> ~/go/bin)
GOBIN       := $(shell $(GO) env GOPATH)/bin
find_tool    = $(or $(shell command -v $(1) 2>/dev/null),$(wildcard $(GOBIN)/$(1)),$(wildcard $(HOME)/go/bin/$(1)))
REVIVE      := $(call find_tool,revive)
STATICCHECK := $(call find_tool,staticcheck)
GOVULNCHECK := $(call find_tool,govulncheck)

define require_tool
	@if [ -z "$(1)" ] || [ ! -x "$(1)" ]; then \
		echo "Error: $(2) not found (run 'make install-tools')" >&2; \
		exit 1; \
	fi
endef

all: fix fmt lint build test ## Run fix, fmt, lint, build, and test

full: clean fix fmt lint vuln build test examples fuzz integration coverage benchmark ## Clean and run all checks, examples, fuzz, integration, coverage, and benchmarks

help: ## Show available targets
	@echo "Usage: make [target] [V=0|1|2]"
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

fmt: ## Format source files with gofmt
	@echo "Formatting..."
	$(RUN) gofmt -w $(FMT_DIRS)

fix: ## Modernize Go code with go fix
	@echo "Fixing..."
	$(RUN) $(GO) fix $(PKGS)

lint: ## Run go vet, revive, and staticcheck
	$(call require_tool,$(REVIVE),revive)
	$(call require_tool,$(STATICCHECK),staticcheck)
	@echo "Linting..."
	$(RUN) $(GO) vet $(PKGS)
	$(RUN) "$(REVIVE)" -set_exit_status $(PKGS)
	$(RUN) "$(STATICCHECK)" $(PKGS)

vuln: ## Check for vulnerabilities with govulncheck
	$(call require_tool,$(GOVULNCHECK),govulncheck)
	@echo "Checking vulnerabilities..."
	$(RUN) "$(GOVULNCHECK)" $(VULN_V) $(PKGS)

build: ## Build all packages
	@echo "Building..."
	$(RUN) $(GO) build $(PKGS)

examples: $(EXAMPLES) ## Build all example binaries

define EXAMPLE_RULE
$(1)/$(notdir $(1)): $(1)/main.go
	@echo "Building $$@..."
	$$(RUN) $$(GO) -C $(1) build -tags ignore_test -o $(notdir $(1))
endef

$(foreach dir,$(patsubst %/main.go,%,$(EXAMPLE_SRCS)),$(eval $(call EXAMPLE_RULE,$(dir))))

test: ## Run unit tests with race detector
	@echo "Running tests..."
	$(RUN) $(GO) test -race $(TEST_V) $(PKGS)

benchmark: ## Run benchmarks
	@echo "Running benchmarks..."
	$(Q) $(GO) test -bench=$(BENCH) -benchmem -run='^$$' $(TEST_V) $(PKGS)

bench: benchmark

fuzz: ## Discover and run all fuzz tests (FUZZTIME=10s)
	@echo "Running fuzz tests (FUZZTIME=$(FUZZTIME))..."
	$(Q) for dir in . ./internal/packets; do \
		tests=$$(grep -oh "func Fuzz[A-Z][a-zA-Z0-9]*" $$dir/*.go 2>/dev/null | cut -d' ' -f2); \
		if [ -n "$$tests" ]; then \
			echo "  --- Package: $$dir ---"; \
			for t in $$tests; do \
				echo "    Fuzzing $$t..."; \
				$(GO) test -fuzz=$$t -fuzztime=$(FUZZTIME) $$dir || exit 1; \
			done; \
		fi; \
	done

coverage: ## Run tests with coverage summary (coverage.out)
	@echo "Generating coverage..."
	$(RUN) $(GO) test -coverprofile=coverage.out -covermode=atomic -coverpkg=$(COVERPKG) $(PKGS)
	@$(GO) tool cover -func=coverage.out | $(if $(filter 0,$(V)),tail -n 1,cat)

coverage-html: coverage ## Generate HTML coverage report (coverage.html)
	@echo "Generating coverage HTML..."
	@$(GO) tool cover -html=coverage.out -o coverage.html

integration: ## Run integration tests with Podman or Docker
	@echo "Running integration tests..."
	@if command -v podman >/dev/null 2>&1; then \
		echo "Using Podman"; \
		if ! systemctl --user is-active --quiet podman.socket 2>/dev/null; then \
			echo "  Starting Podman socket..."; \
			systemctl --user start podman.socket || true; \
		fi; \
		cd integration && $(GO) mod tidy && DOCKER_HOST=unix:///run/user/$(shell id -u)/podman/podman.sock TESTCONTAINERS_RYUK_DISABLED=true $(GO) test -v -timeout=5m; \
	elif command -v docker >/dev/null 2>&1; then \
		echo "Using Docker"; \
		cd integration && $(GO) mod tidy && TESTCONTAINERS_RYUK_DISABLED=true $(GO) test -v -timeout=5m; \
	else \
		echo "Error: Neither Podman nor Docker found" >&2; \
		echo "Please install one of them to run integration tests" >&2; \
		exit 1; \
	fi

install-tools: ## Install revive, staticcheck, and govulncheck
	@echo "Installing tools..."
	$(RUN) $(GO) install github.com/mgechev/revive@latest
	$(RUN) $(GO) install honnef.co/go/tools/cmd/staticcheck@latest
	$(RUN) $(GO) install golang.org/x/vuln/cmd/govulncheck@latest

version: ## Show current version
	@echo "$(VERSION)"

clean: ## Remove build and test artifacts
	@echo "Cleaning..."
	$Q rm -rf coverage.out coverage.html coverage.txt cpu.out mem.out *.test \
		$(EXAMPLES) \
		examples/throughput/paho_v3/paho_v3 \
		examples/throughput/paho_v5/paho_v5
