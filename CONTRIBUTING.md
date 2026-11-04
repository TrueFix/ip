# Contributing

Use the Go version in `go.mod` and Python 3. Docker Engine/Compose are needed for container checks.
Never use real provider credentials in tests or commit production MMDB files.

```sh
make hooks
make check
make audit
```

The repository-local pre-commit hook checks **staged** contents for forbidden
local files and common provider-secret assignments, without displaying values.
The hook requires Python 3. It is an extra safeguard, not a complete secret
scanner. Review `git diff --cached` before committing.

Keep the public JSON schema stable. Missing data should remain null rather than
be guessed. Provider adapters must preserve source attribution and licensing,
never merge incompatible ASN/location data, and avoid outbound lookup calls.
Use synthetic fixtures for tests; opt-in live tests must not require CI secrets.

For a release, follow `deploy/PRODUCTION.md`; run checks/audits before creating
a version tag. The single workflow verifies main pushes, manual runs and tags;
only verified tag pushes publish Docker Hub images and GitHub Releases. There
is no separate PR-test workflow. Review dependency bot PRs
rather than auto-merging them.

Keep documentation focused: overview in `README.md`, installation in
`DEPLOYMENT.md`, variables in `docs/ENVIRONMENT.md`, API behavior in
`docs/REFERENCE.md`, and release operations in `deploy/PRODUCTION.md`.
Maintain one root [llms.txt](llms.txt) guide rather than duplicating LLM instructions.
