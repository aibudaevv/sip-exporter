# Contributing to sip-exporter

Thank you for helping improve sip-exporter. By participating, you agree to follow the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Before You Start

- Use [GitHub Issues](https://github.com/aibudaevv/sip-exporter/issues) for support, bug reports,
  and feature requests.
- Report vulnerabilities through the private process in the [Security Policy](.github/SECURITY.md),
  not through a public issue.
- Remove phone numbers, Call-IDs, credentials, raw SIP messages, packet captures, and identifiable
  network details from every issue, test fixture, log, and pull request.

Every pull request needs a sprint task ID assigned by the maintainer. Open an issue before starting
work, including small documentation fixes, and wait for the maintainer to confirm the scope and
assign an ID such as `S29-4.1`.

## Development Workflow

1. Fork the repository and update your local `develop` branch.
2. Create `feature/<task-ID>/<lowercase_snake_case>` from `develop`, for example
   `feature/S30-1.1/fix_dialog_expiry`.
3. Keep one pull request to one logical change. Production-code changes are limited to 50 lines of
   diff, excluding generated files, tests, and documentation. Split structural and behavioral
   changes into separate pull requests.
4. Follow the existing style. Go code follows the
   [Google Go Style Guide](https://google.github.io/styleguide/go/); run `gofmt` and `goimports`.
5. Commit as `<type>(<scope>): [<task-ID>] <subject>`, for example
   `fix(rtp): [S30-1.1] preserve dialog expiry state`.
6. Open the pull request against `develop`, link the issue, and include the commands and results
   used for verification. Do not target `master`; it is the public release branch.

Do not change service behavior only to make a test pass. Do not add `//nolint` directives. Changes
to `internal/bpf/` or rebuilds of `bin/sip.o` require explicit maintainer approval before work
starts; agree on their verification separately in the issue.

## Verification

For documentation-only changes, run:

```bash
git diff --check
```

For Go changes, run:

```bash
make test
make lint
```

E2E and load tests require Docker, root privileges, and coordinated access to packet sockets. Run
them only through the corresponding Make targets, never concurrently and never through a raw
`go test -tags=e2e` command. If you cannot run a required check, state that explicitly in the pull
request instead of claiming it passed.

## Review and Licensing

The maintainer reviews scope, correctness, tests, documentation, compatibility, and operational
risk. A pull request may be asked to split or narrow before review. Merge and release decisions
follow [Governance](GOVERNANCE.md).

By submitting a contribution, you confirm that you have the right to provide it under the
repository's [AGPL-3.0 license](LICENSE). The project does not currently require a Contributor
License Agreement or Developer Certificate of Origin sign-off.
