# Contributing

Bug reports, ideas, documentation, tests, and code are welcome. Please follow the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Before contributing

- Use [GitHub Issues](https://github.com/aibudaevv/sip-exporter/issues) for bugs and feature ideas.
  An issue is optional for small fixes.
- Report vulnerabilities privately through the [Security Policy](.github/SECURITY.md).
- Remove credentials, phone numbers, Call-IDs, raw SIP messages, packet captures, and identifiable
  network details.
- Discuss changes to `internal/bpf/` before starting.

## Pull requests

1. Fork the repository and create a branch from `develop`.
2. Keep the pull request focused on one logical change and target `develop`.
3. Follow the existing style and add tests or documentation when relevant.
4. List the checks you ran and anything you could not verify.

For documentation-only changes, run `git diff --check`. For Go changes, run `make test` and
`make lint`. Run E2E and load tests only through their Makefile targets and never concurrently.

By contributing, you agree that your work is licensed under the repository's
[AGPL-3.0 license](LICENSE).
