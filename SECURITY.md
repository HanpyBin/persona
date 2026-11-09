# Security policy

## Supported versions

Persona ships rolling releases, not numbered long-term-support branches. Security fixes land on the default branch
and go out with the next release; the update feed keeps only the newest build.

| What you run | Security support |
| --- | --- |
| The latest hub / node release | Supported — update in place, or let the hub auto-update |
| The latest TestFlight client build | Supported |
| Any older release or client build | Not supported separately; please update before reporting |
| A build from the default branch | Fixes land here first; rebuild and redeploy from the current branch |

The hub and its clients interoperate across rolling upgrades
([docs/protocol/rolling-upgrade-compatibility.md](docs/protocol/rolling-upgrade-compatibility.md)), so staying current
is always safe.

## Reporting a vulnerability

**Do not open a public issue, pull request or discussion comment for a vulnerability.** Report it privately through
GitHub Security Advisories:

[Report a vulnerability](https://github.com/nwverse/persona/security/advisories/new)

The report stays private; only the maintainers can see it. Include:

- the affected component and version, build number or commit;
- the surface involved (hub TLS listener, admin console, pairing, a client, an extension);
- reproduction steps and a proof of concept where possible;
- the impact you expect and any configuration needed to reach it;
- any mitigation you have already applied.

Redact your own tokens, hub addresses, emails and message content. Please give the maintainers a reasonable window to
ship a fix before any public disclosure; the advisory can be published and, if appropriate, used to request a CVE once
a fix is available.

## Hub exposure surface

A hub is designed to be reachable directly from the internet, so its trust boundaries are worth understanding before
you expose one. The complete references are [docs/operations/network.md](docs/operations/network.md) and
[docs/operations/admin.md](docs/operations/admin.md).

### Pairing tokens and TLS

- Expose only the hub's TLS listener publicly. Public plaintext HTTP is refused with `403 tls_required`, apart from
  the unauthenticated health endpoint; clients must verify HTTPS / WSS before sending any token.
- `personad hub pair` creates a **temporary pairing token** that lasts ten minutes by default and is redeemed exactly
  once for a device token. Treat pairing links and QR codes as credentials: anyone holding one can pair a device.
- The long-lived main token is never accepted from a public source; only a loopback pairing command can print it.
- With the managed self-signed certificate, clients pin the certificate public key obtained through a trusted scan
  (minimum TLS 1.2). A fingerprint from an unauthenticated public response cannot establish initial trust.
- Repeated authentication failures from one source are banned (by default ten failures within sixty seconds blocks the
  source for fifteen minutes); local-network sources are not counted.

### Admin console first-time setup

- The admin console at `/admin` authenticates separately with its own password, stored as an argon2id hash; app
  tokens are not accepted there.
- The default access scope is the local network. The first admin password can only be set from a source that is in
  scope by address; a visitor arriving over the public internet cannot claim the password first.
- Setting the password from loopback additionally requires a single-use setup code printed on the hub machine, and new
  installs require a setup code from every source so that a shared LAN cannot claim the console either.
- In containers, the Docker gateway is treated as a public source, so port-mapped public traffic cannot pass itself
  off as local.

### PROXY protocol trust

- The hub never uses `X-Forwarded-For` or `Forwarded` for trust decisions. It reads PROXY-protocol headers only when
  `security.proxy_protocol` explicitly lists trusted upstream addresses; open-wide ranges are rejected and a
  misconfigured listener refuses to start.
- If you place a same-machine TCP forwarder in front of the hub without PROXY protocol, every forwarded connection
  looks like loopback: the admin scope, the public main-token prohibition and the rate limiter would all be bypassed.
  Enable PROXY protocol across the whole forwarding chain in that topology, or prefer the hub's own TLS listener.

### Extensions and the auth relay

- Extensions run as separate programs and must declare every permission; install and permission growth both require an
  explicit confirmation in the admin console.
- The auth-relay contribution is off by default and only completes authorization links that appear verbatim in the
  output of a command from one of the hub owner's own computers; it never logs the link or its token
  ([docs/operations/auth-relay.md](docs/operations/auth-relay.md)).

### Data at rest

- All hub state lives in the Postgres container, which should stay loopback-bound and never be published. The hub
  token and database password stay in `~/.persona/hub.json` with owner-only permissions, and mail credentials are
  encrypted in the database with a key file kept outside it.
- Revoke a lost device's credentials from the admin console (`/admin/devices`); revoke a computer's pairing from
  `/admin/machines`.
