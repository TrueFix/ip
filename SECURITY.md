# Security policy

Never publish provider tokens, MaxMind license keys, `.env`, private keys, or
production database snapshots in issues, commits, build logs or images.
Credentials shared in chat or otherwise disclosed should be revoked and replaced
before deploying publicly. Deleting a file does not remove it from Git history.

For a suspected vulnerability, do not open a public issue with exploitation
details or credentials. Use the repository's **Security → Report a vulnerability**
when private reporting is enabled. If unavailable, request a private contact
without disclosing exploit details. No dedicated security email is published.

## Deployment boundary

- The API does not need provider credentials; only the updater receives them.
- Explicitly trust only your ingress CIDRs. Sanitize both origin and client-IP
  forwarding headers, and use an allowed-host list in production.
- Keep port 9090 private. Local binaries bind metrics to loopback by default;
  Docker exposes it only inside the container network unless explicitly published.
- Terminate TLS at your ingress, use Cloudflare Full (strict) when applicable,
  and prevent direct origin access if Cloudflare is your security boundary.
- Use bounded limits at the app and edge. App-local RPM budgets are not shared
  across replicas and are not DDoS protection. Rotating IPv6 /64s can fill the
  client table and deny new clients; verify aggregate/prefix admission controls
  at ingress and prevent origin bypass before public deployment.
- Restrict data-volume writes to one updater; an OS file lock rejects overlapping
  attempts on a local filesystem. NFS/distributed filesystems need explicitly
  verified locking semantics or an external single-writer controller.
- Keep images, dependencies, database snapshots and IANA metadata current.
- Disabling proxy access logs does not remove client IPs/URLs from error logs.
  Set access restrictions, retention and redaction for local and centralized logs.

## Checks

`make check` runs tracking safeguards, tests, vet and Compose validation.
`make audit` scans the application and separately built official downloader.
The single workflow verifies main pushes, manual runs and release tags; both
publishing jobs require tests, audits and a production-image smoke test. It is
not a pull-request workflow. Run checks before tagging. The focused guard does not
replace a comprehensive secret scanner; enable GitHub's scanning as well.
