# Security policy

## Supported versions

This project has no releases yet. Security fixes go to the `main` branch
only, so run the latest `main`.

## Reporting a vulnerability

Report a vulnerability in this Go port privately, through
[GitHub's private vulnerability reporting](https://github.com/citaylor/gads-go/security/advisories/new)
(the **Report a vulnerability** button on the Security tab). Please don't
open a public issue for it.

Include what you can of:

- the affected route, script or package, and the commit you tested;
- the steps to reproduce it, or a proof of concept;
- the configuration that matters, for example `gads: hardened: 0` or the
  database used.

This is a project maintained by one person in their spare time. You can
expect:

- an acknowledgement within 7 days;
- an assessment (accepted or declined, with the reason) within 30 days;
- for an accepted report, a fix on `main` and a published advisory that
  credits you, unless you prefer not to be named.

## Scope

In scope: the Go code in `gads-go/`, the Docker setup in `gads-go/docker/`
and `docker-compose.yml`, and the CI workflows.

Out of scope:

- **Perl GADS.** The Perl application in `upstream/` belongs to the
  [GADS project](https://github.com/ctrlo/GADS). Report its
  vulnerabilities to its maintainers, Ctrl O, not here.
- **Behaviour that is Perl's on purpose.** With `gads: hardened: 0`, the
  port reproduces Perl GADS exactly, including the weaknesses that
  hardened mode fixes. Those are documented in
  [`CLAUDE.md`](CLAUDE.md#security), and so are the findings that are
  deliberately not fixed.
- **The test tools.** The conformance runner, the benchmark tools and the
  scenarios are for development, not deployment.

## Hardened mode

The application runs in hardened mode unless `gads: hardened: 0` is set.
It fixes the critical, high and medium findings of a security review of
Perl GADS, which is in
[`GADS-security-report-verified.md`](GADS-security-report-verified.md).
