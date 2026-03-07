# Release v0.9.5

This release focuses on enhancing codebase maintainability, addressing technical debt, and correcting a performance regression identified in the previous version. It also includes several dependency updates for our integration test suite.

## ⚡ Performance Corrections

### Reverted Serialization Optimization
- **`sync.Pool` Revert**: In v0.9.4, we introduced `sync.Pool` for outgoing packet serialization to reduce memory pressure. Subsequent benchmarking revealed that this optimization was actually counter-productive, doubling allocation memory and increasing GC overhead. The client already utilizes a buffered writer, which provides superior performance for these workloads. This change restores the more efficient direct-to-buffer serialization path.

## 🛠️ Code Quality & Maintenance

### Linter Integration
- **`revive`**: We have replaced `golangci-lint` and `go vet` with `revive` in our `Makefile`. This transition allows for faster linting and better enforcement of idiomatic Go patterns throughout the codebase.
- **Refactoring Pass**: Addressed over 100 linter findings across 50 files. Key improvements include:
    - **Naming Consistency**: Standardized parameter names in functional options and internal methods (e.g., `max` -> `limit` or `maxLength`).
    - **Clean API Callbacks**: Updated all test handlers and public callbacks to use the `_` blank identifier for unused parameters, making the code cleaner and less error-prone.
    - **Docstrings**: Added missing documentation to several internal and public packages, including `internal/packets`.
    - **Wildcard Logic**: Simplified the internal topic matching logic (`MatchTopic`) for better readability and performance.
    - **ClientID Validation**: Refactored the protocol-specific validation logic for empty ClientIDs, ensuring clearer error handling for both MQTT v3.1.1 and v5.0 connections.

## ✅ Testing & Quality

### Dependency Updates
- **Integration Tests**: Updated key dependencies in the integration test suite to their latest stable versions:
    - `go.opentelemetry.io/otel` to **v1.41.0**
    - `golang.org/x/sys` to **v0.41.0**
    - `google.golang.org/protobuf` to **v1.36.11**

## 📦 Installation

```bash
go get github.com/gonzalop/mq@v0.9.5
```
