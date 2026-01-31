.PHONY: all build clean coverage full fuzz fmt help integration lint test examples

EXAMPLE_SRCS := $(wildcard examples/*/main.go)
EXAMPLES := $(foreach dir,$(patsubst %/main.go,%,$(EXAMPLE_SRCS)),$(dir)/$(notdir $(dir)))

all: fmt lint build test
	@echo "✅ Formatted, linted, built, tested."
	@echo "ℹ️  Run 'make help' to see other available targets."

full: clean fmt lint build test examples fuzz integration coverage benchmark

help:
	@echo "Available targets:"
	@echo "  make          - Run format, lint, build, and test"
	@echo "  make full     - Run format, lint, build, test, fuzz, integration, coverage, and benchmark (~3-4m)"
	@echo "  make fmt      - Format code with gofmt"
	@echo "  make lint     - Run linters (golangci-lint and go vet)"
	@echo "  make build    - Build the project"
	@echo "  make examples - Build all example binaries"
	@echo "  make test     - Run unit tests with race detector"
	@echo "  make benchmark - Run benchmark tests"
	@echo "  make fuzz     - Run fuzz tests"
	@echo "  make coverage - Generate coverage report"
	@echo "  make integration - Run integration tests with Podman or Docker"
	@echo "  make clean    - Remove build artifacts"

fmt:
	@echo "🖌️  Formatting: gofmt -w ."
	@gofmt -w .

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		echo "🔍 Linting: golangci-lint run"; \
		golangci-lint run; \
	else \
		echo "⚠️  golangci-lint not installed, skipping"; \
		echo "   To install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi
	@echo "🔍 Linting: go vet ./..."
	@go vet ./...

build:
	@echo "🏗️  Building: go build ./..."
	@go build ./...

examples: $(EXAMPLES)

define EXAMPLE_RULE
$(1)/$(notdir $(1)): $(1)/main.go
	@echo "🔨 Building $$@..."
	@cd $(1) && go build -tags ignore_test -o $$(notdir $$@)
endef

$(foreach dir,$(patsubst %/main.go,%,$(EXAMPLE_SRCS)),$(eval $(call EXAMPLE_RULE,$(dir))))

test:
	@echo "🧪 Testing: go test -race ./..."
	@go test -race ./...

benchmark:
	@echo "🔥 Running benchmarks: go test -bench=. -benchmem -v ./..."
	@go test -bench=. -benchmem -run=^$ -v ./...


fuzz:
	@echo "🌀 Running fuzz tests..."
	@echo "  Fuzzing packet reader..."
	@cd internal/packets && go test -fuzz=FuzzReadPacket -fuzztime=10s
	@echo "  Fuzzing fixed header decoder..."
	@cd internal/packets && go test -fuzz=FuzzDecodeFixedHeader -fuzztime=10s
	@echo "  Fuzzing variable integer decoder..."
	@cd internal/packets && go test -fuzz=FuzzDecodeVarInt -fuzztime=10s
	@echo "  Fuzzing string decoder..."
	@cd internal/packets && go test -fuzz=FuzzDecodeString -fuzztime=10s
	@echo "  Fuzzing CONNECT decoder..."
	@cd internal/packets && go test -fuzz=FuzzDecodeConnect -fuzztime=10s
	@echo "  Fuzzing PUBLISH decoder..."
	@cd internal/packets && go test -fuzz=FuzzDecodePublish -fuzztime=10s
	@echo "  Fuzzing PUBCOMP decoder..."
	@cd internal/packets && go test -fuzz=FuzzDecodePubcomp -fuzztime=10s
	@echo "  Fuzzing PUBREC decoder..."
	@cd internal/packets && go test -fuzz=FuzzDecodePubrec -fuzztime=10s
	@echo "  Fuzzing PUBREL decoder..."
	@cd internal/packets && go test -fuzz=FuzzDecodePubrel -fuzztime=10s
	@echo "  Fuzzing topic matcher..."
	@go test -fuzz=FuzzMatchTopic -fuzztime=10s
	@echo "  Fuzzing publish topic validation..."
	@go test -fuzz=FuzzValidatePublishTopic -fuzztime=10s
	@echo "  Fuzzing subscribe topic validation..."
	@go test -fuzz=FuzzValidateSubscribeTopic -fuzztime=10s
	@echo "  Fuzzing client packet handling..."
	@go test -fuzz=FuzzClientHandleIncoming -fuzztime=10s
	@echo "✅ Fuzzing complete"

coverage:
	@echo "📊 Generating coverage report..."
	@ go test -coverprofile=coverage.out -coverpkg=./... ./...
	@ go tool cover -html=coverage.out -o coverage.html
	@echo "✅ Coverage report generated at coverage.html"

integration:
	@echo "🧪 Running integration tests..."
	@if command -v podman >/dev/null 2>&1; then \
		echo "✓ Using Podman"; \
		if ! systemctl --user is-active --quiet podman.socket 2>/dev/null; then \
			echo "  Starting Podman socket..."; \
			systemctl --user start podman.socket || true; \
		fi; \
		cd integration && go mod tidy && DOCKER_HOST=unix:///run/user/$(shell id -u)/podman/podman.sock TESTCONTAINERS_RYUK_DISABLED=true go test -v -timeout=5m; \
	elif command -v docker >/dev/null 2>&1; then \
		echo "✓ Using Docker"; \
		cd integration && go mod tidy && TESTCONTAINERS_RYUK_DISABLED=true go test -v -timeout=5m; \
	else \
		echo "❌ Error: Neither Podman nor Docker found"; \
		echo "   Please install one of them to run integration tests"; \
		exit 1; \
	fi

clean:
	@echo "🧹 Cleaning up..."
	@rm -fv coverage.html coverage.out coverage.txt cpu.out mem.out mq.test packets.test \
            ${EXAMPLES} \
            examples/throughput/paho_v3/paho_v3 \
            examples/throughput/paho_v5/paho_v5
