# Security Policy

## Supported Versions

Security fixes are considered for the current `master` branch and the latest stable release. Older
releases are not supported; upgrade before requesting a backport.

| Version | Supported |
|---|---|
| `master` | Yes |
| Latest stable release | Yes |
| Older releases | No |

## Report a Vulnerability

Use [GitHub Private Vulnerability Reporting](https://github.com/aibudaevv/sip-exporter/security/advisories/new).
Do not disclose a suspected vulnerability in a public Issue, discussion, pull request, log, or
packet capture.

Include:

- the affected release or commit;
- the affected component and deployment conditions;
- reproducible steps or a minimal proof of concept;
- the expected impact and any known mitigations;
- only sanitized logs or traffic details.

Never include credentials, phone numbers, Call-IDs, raw SIP messages, packet captures, or
identifiable third-party network details. Test only systems and data you are authorized to access.

The maintainer will evaluate the report, coordinate a fix and advisory when appropriate, and keep
the reporter informed on a best-effort basis. The project does not promise a fixed response or fix
time. Keep the report confidential until the maintainer and reporter coordinate disclosure.

For ordinary bugs, feature requests, deployment questions, and non-sensitive hardening ideas, use
[GitHub Issues](https://github.com/aibudaevv/sip-exporter/issues). For the runtime privilege model,
data boundaries, telemetry, exposed labels, and operational hardening, see
[Operational Security](../docs/SECURITY.md).
