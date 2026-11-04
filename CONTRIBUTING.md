# Contributing

Use Go 1.27+ and Python 3. Docker Engine/Compose are needed for container checks.
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
a version tag. The one publishing workflow builds Docker Hub images and GitHub
Releases on tags; it does not provide PR-test CI. Review dependency bot PRs
rather than auto-merging them.
