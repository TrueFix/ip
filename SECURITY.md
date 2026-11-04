# Security policy

Never publish provider tokens, MaxMind license keys, `.env`, private keys, or
production database snapshots in issues, commits, build logs or images.
Credentials shared in chat or otherwise disclosed should be revoked and replaced
before deploying publicly. Deleting a file does not remove it from Git history.

For a suspected vulnerability, do not open a public issue with exploitation
details or credentials. Once hosted on GitHub, use **Security → Report a
vulnerability** if private reporting is enabled. Repository maintainers should
enable private vulnerability reporting, secret scanning/push protection and
branch protections before accepting contributions. No private reporting address
is configured in this repository yet.

## Deployment boundary

- The API does not need provider credentials; only the updater receives them.
- Explicitly trust only your ingress CIDRs. Sanitize both origin and client-IP
  forwarding headers, and use an allowed-host list in production.
- Keep port 9090 private. Local binaries bind metrics to loopback by default;
  Docker exposes it only inside the container network unless explicitly published.
- Terminate TLS at your ingress, use Cloudflare Full (strict) when applicable,
  and prevent direct origin access if Cloudflare is your security boundary.
- Use bounded limits at the app and edge. App-local RPM budgets are not shared
  across replicas and are not DDoS protection.
- Restrict data-volume writes to one updater; an OS file lock rejects overlapping
  attempts on a local filesystem. NFS/distributed filesystems need explicitly
  verified locking semantics or an external single-writer controller.
- Keep images, dependencies, database snapshots and IANA metadata current.

## Checks

`make check` runs tracking safeguards, tests, vet and Compose validation.
`make audit` runs the pinned Go vulnerability checker. The single tag workflow
builds/pushes the Docker image and publishes binary releases; it does not run
automatic PR tests or audits. Run checks and the container smoke test before
tagging. The focused commit guard does not
replace a comprehensive secret scanner; enable GitHub's scanning as well.
