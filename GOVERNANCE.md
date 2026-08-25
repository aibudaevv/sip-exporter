# Governance

## Project Model

sip-exporter currently uses a lightweight sole-maintainer model. Project decisions are made in
public GitHub Issues and pull requests whenever security, privacy, or Code of Conduct concerns do
not require a private channel.

## Roles

- **Users** run the software and may ask questions or report problems.
- **Contributors** propose issues, documentation, tests, or code under the process in
  [CONTRIBUTING.md](CONTRIBUTING.md).
- **Maintainer:** [@aibudaevv](https://github.com/aibudaevv) currently has final responsibility for
  scope, task IDs, review, merge, releases, security coordination, and Code of Conduct enforcement.

There is no voting body, quorum, or guaranteed response time. The maintainer may delegate a review
without transferring final merge or release responsibility.

## Decisions

Proposals start with a GitHub Issue. The maintainer evaluates them against the documented product
scope, evidence, backward compatibility, security and privacy impact, maintenance cost, and test
coverage. Accepted work receives a sprint task ID before implementation begins.

Material decisions and their reasoning are recorded in the relevant Issue or pull request. The
maintainer may reject, defer, narrow, or request changes to a proposal. Security reports and Code
of Conduct cases are handled privately under their respective policies.

## Branches and Releases

`develop` is the integration branch for contributed work. `master` is the public release branch.
The maintainer decides when reviewed changes move from `develop` to `master`, assigns versions,
publishes releases, and may hold a change until its documentation and release surface are ready.

## Adding Maintainers

The maintainer group may expand after sustained contributions that demonstrate technical quality,
reliable review, respectful collaboration, and familiarity with the project's operational and
security constraints. New maintainers are invited by the current maintainer and recorded through a
reviewed change to this document. Removal or role changes use the same public process unless a Code
of Conduct or security concern requires private handling.
