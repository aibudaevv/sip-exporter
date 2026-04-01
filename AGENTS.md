# AGENTS.md

Coding agent guidelines for SIP-Exporter - an eBPF-based SIP monitoring service.

## Agent Role

You are a **Senior Go Developer** with extensive experience in telephony systems, SIP protocols, and high-performance networking. Apply deep knowledge of SIP/RTP protocols, eBPF, Prometheus metrics, and distributed systems best practices.

## Critical Rules

- **NEVER push to git without explicit user request**
- **NEVER commit without explicit user request**
- **Design tests using MC/DC (Modified Condition/Decision Coverage) approach**
- **Do not write absurd or meaningless tests** - each test must verify real behavior
- **Communicate in Russian language with the user**
- **NEVER modify eBPF code when developing tests** - eBPF code is correct and tested
- **NEVER use loopback interface (`lo`) for integration tests** - packets are duplicated (sent + received), causing incorrect metrics
- **NEVER create shell scripts (`.sh`) for running tests** - use Go test framework directly
- **NEVER modify service code to make integration tests pass** - tests must adapt to the service, not vice versa

## Domain Knowledge

### SIP Protocol

- Reference **RFC 6076** for SIP Performance Metrics when implementing new metrics
- Dialog ID format: `{call-id}:{min-tag}:{max-tag}` (tags sorted lexicographically)
- Dialogs created on `200 OK` response to `INVITE`, removed on `200 OK` to `BYE`
- Standard SIP ports: 5060 (UDP/TCP), 5061 (TLS)

### eBPF Architecture

```
SIP Traffic → NIC → eBPF socket filter → ringbuf → Go poller → SIP parser → Prometheus
```

- Socket filter attached via `AF_PACKET` socket
- Handles VLAN-tagged and regular Ethernet frames
- Zero-copy packet transfer from kernel to userspace via ringbuf
- **Known limitation**: Packet copying in 64-byte blocks due to eBPF verifier constraints

## Build Commands

```bash
make build          # Full build (eBPF + Go binary)
make ebpf_compile   # Compile eBPF C code only: clang -O2 -target bpf -c internal/bpf/sip.c -o bin/sip.o -g -fno-stack-protector
make go_build       # Build Go binary only: go build -o bin/main cmd/main.go
make docker_build   # Build Docker image (default target)
make clean          # Remove built artifacts
```

## Test Commands

```bash
make test           # Run all tests: go test -v ./...
go test -v ./internal/service/...                              # Run tests for specific package
go test -v -run TestMetricser_Request_AllMethodsSingleRun ./internal/service/...   # Run single test
go test -v -run TestName ./path/to/package/...                 # Run single test by name
go test -bench=. -benchmem ./internal/exporter/...             # Run benchmarks
go test -v -coverprofile=coverage.out ./...                    # Run with coverage
```

## Lint Commands

```bash
make lint           # Run golangci-lint
make vet            # Run go vet: go vet -unsafeptr ./...
make imports        # Format imports: goimports -l -w .
```

## Code Style

### Imports

Group imports in three sections separated by blank lines:
1. Standard library
2. External packages
3. Local packages (gitlab.com/sip-exporter/...)

```go
import (
    "bytes"
    "errors"
    "fmt"

    "github.com/cilium/ebpf"
    "github.com/prometheus/client_golang/prometheus"
    "go.uber.org/zap"

    "gitlab.com/sip-exporter/internal/dto"
    "gitlab.com/sip-exporter/internal/service"
)
```

### Naming Conventions

- **Interfaces**: Use `-er` suffix (e.g., `Metricser`, `Dialoger`, `Exporter`, `Server`)
- **Constructors**: Use `New` prefix (e.g., `NewMetricser()`, `NewExporter()`, `NewServer()`)
- **Private structs**: lowercase (e.g., `metrics`, `exporter`, `dialogs`)
- **Constants**: PascalCase for exported, camelCase for private
- **Sentinel errors**: `Err` prefix (e.g., `ErrUserNotRoot`)

### Type Definitions

Group related types using the `type (...)` block syntax:

```go
type (
    exporter struct {
        collection *ebpf.Collection
        sock       int
        messages   chan []byte
        services   services
    }
    services struct {
        metricser service.Metricser
        dialoger  service.Dialoger
    }
    Exporter interface {
        Initialize(interfaceName string, path string, sipPort, sipsPort int) error
        Close()
    }
)
```

### Error Handling

- Wrap errors with context using `fmt.Errorf`: `return fmt.Errorf("failed to load BPF collection: %w", err)`
- Use sentinel errors for package-level errors: `var ErrUserNotRoot = errors.New("this program requires root privileges")`
- For intentional error ignores, use `// nolint:gosec // explanation` comment
- **NEVER use `if err == nil`** — always use `if err != nil` pattern for error handling

### Logging

Use Uber Zap logger via global logger `zap.L()`:

```go
zap.L().Info("eBPF program attached",
    zap.String("interface", interfaceName),
    zap.Int("sip_port", sipPort))
zap.L().Error("socket read error", zap.Error(err))
zap.L().Debug("packet raw", zap.ByteString("sip_data", sipData))
```

### Struct Tags

Use `env` tags for configuration with cleanenv:

```go
type App struct {
    LogLevel      string `env:"SIP_EXPORTER_LOGGER_LEVEL" env-default:"info"`
    Port          string `env:"SIP_EXPORTER_HTTP_PORT" env-default:"2112"`
    Interface     string `env:"SIP_EXPORTER_INTERFACE" env-required:"true"`
}
```

## Testing

- Use `github.com/stretchr/testify/require` for assertions
- Use table-driven tests with `t.Run()` for subtests
- Test files follow `*_test.go` pattern
- Benchmarks follow `*_bench_test.go` pattern

```go
func TestSomething(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    int
    }{
        {"case1", "input1", 1},
        {"case2", "input2", 2},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := process(tt.input)
            require.Equal(t, tt.want, got)
        })
    }
}
```

## Linter Configuration

The project uses golangci-lint with strict settings (see `.golangci.yaml`):

- **Max line length**: 120 characters (golines)
- **Max function length**: 100 lines, 50 statements (funlen)
- **Max cyclomatic complexity**: 30 (cyclop)
- **Max cognitive complexity**: 20 (gocognit)
- Tests are excluded from linting (`run.tests: false`)

### Key Linters Enabled

- `errcheck` - unchecked errors (type assertions included)
- `govet` - suspicious constructs (all analyzers, strict shadowing)
- `staticcheck` - static analysis
- `gosec` - security issues
- `revive` - style issues
- `testifylint` - testify usage
- `exhaustive` - enum switch exhaustiveness

### nolint Directives

Must include explanation: `//nolint:funlen // complex function with many cases`

## Project Structure

```
cmd/main.go                 # Application entry point
internal/
├── bpf/sip.c               # eBPF C program
├── config/                 # Configuration management
├── dto/                    # Data transfer objects
├── exporter/               # Main exporter logic (eBPF, packet parsing)
├── server/                 # HTTP server with Prometheus metrics
├── service/                # Business logic (metrics, dialogs)
├── constant.go             # Global constants
pkg/log/                    # Zap logger configuration
```

## Requirements

- Go 1.24+
- Clang/LLVM for eBPF compilation
- golangci-lint for linting
- goimports for import formatting
- Root privileges required at runtime (eBPF requires CAP_BPF or root)

## Environment Variables

All configuration via environment variables with `SIP_EXPORTER_` prefix:

| Variable | Default | Description |
|----------|---------|-------------|
| `SIP_EXPORTER_LOGGER_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `SIP_EXPORTER_HTTP_PORT` | `2112` | Prometheus metrics HTTP port |
| `SIP_EXPORTER_INTERFACE` | required | Network interface to monitor |
| `SIP_EXPORTER_OBJECT_FILE_PATH` | `/usr/local/bin/sip.o` | Path to eBPF object file |
| `SIP_EXPORTER_SIP_PORT` | `5060` | SIP port to monitor |
| `SIP_EXPORTER_SIPS_PORT` | `5061` | SIPS (TLS) port to monitor |
