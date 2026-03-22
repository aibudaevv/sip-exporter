# CHANGELOG
## 0.4.0
### Added
- Comprehensive test coverage with MC/DC standard compliance
- Unit tests for all packages: config, dto, exporter, server, service, log
- Table-driven tests for SIP methods and response codes
- Concurrent tests for dialog storage thread-safety
- Integration tests for server shutdown and signal handling

### Test Coverage
- `internal/config`: 100.0%
- `internal/service`: 100.0%
- `pkg/log`: 95.5%
- `internal/server`: 90.5%
- `internal/exporter`: 60.2%

## 0.3.0
### Added
- `sip_exporter_sessions` - sip dialogs metrics
