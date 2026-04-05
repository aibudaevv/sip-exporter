# CHANGELOG

## 0.5.0
### Added
- SER (Session Establishment Ratio) metric per RFC 6076 (`sip_exporter_ser`)
- E2E tests with SIPp via testcontainers-go (`make test-e2e`)
- SER test scenarios: 100%, 0%, redirect, mixed, no INVITE, mixed 3xx+200
- E2E tests verify `sip_exporter_sessions` returns to 0 after completion
- Comprehensive unit test coverage with MC/DC standard compliance
- Unit tests for all packages: config, dto, exporter, server, service, log

### Changed
- E2E tests use loopback interface automatically (no physical interface required)
- All comments in code translated to English

### Fixed
- SIPp scenarios: proper To tag in 200 OK responses to BYE for correct dialog termination

### Removed
- Absurd tests that tested Go language features instead of business logic
- `docker-compose.test.yml` — replaced by testcontainers-go
- Makefile targets `test-ser-*` — replaced by `make test-e2e` and `make test-e2e-run`

## 0.4.0
### Added
- `sip_exporter_sessions` - sip dialogs metrics

## 0.3.0
### Added
- Initial release with basic SIP metrics
