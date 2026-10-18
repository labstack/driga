# Fleet transport evaluation — "why SSH, and not a specific protocol?"

Status: research, no decision · Date: 2026-10-18 · Scope: multi-machine ("fleet") mode, v1.5–v2 (design §2 non-goals, §8.1 journey 7). Facts were checked against primary sources on 2026-10-18; anything not verified is marked **[unverified]**. Binary sizes were measured on this Mac with throwaway hello-world programs in a scratch directory (nothing committed).

## 1. Question and constraints

The maintainer asked why the fleet mode should ride on SSH rather than a purpose-built protocol. This document evaluates nine transport families (plus two found during research) against Driga's actual constraints:

- v1 makes zero network calls; one collector writes SQLite, readers are read-only; every row already has `machine_id` (§5, §10).
- The JSON envelope `{"schema":1,"observed_at","mode","freshness","coverage_gaps","data"}` is the stable contract and MCP (v1.5) reuses it (§8).
- Journey 7: `driga machines`, `driga --machine mini inbox`; offline machines stay visible as stale.
- Inbox push: a `waiting` agent on machine A should be visible on machine B within seconds.
- Privacy (§13): titles by default, prompt snippets opt-in; whoever can read the fleet feed can read those.
- Single static cgo-free Go binary (currently 13.3 MB stripped); OSS users with 2–3 Macs and maybe a Linux box; no telemetry, no mandatory third party.

Three layers must be decided separately, because most options only differ in one of them:

| Layer | Question | Decided by |
|---|---|---|
| **Wire format** | What bytes mean | Driga (JSON envelope + an event feed) |
| **Transport** | How bytes move, encrypted and authenticated | this document |
| **Discovery / pairing** | How machine B learns A's address and decides to trust it | this document |

One structural fact drives everything: for laptops that sleep and roam, **"no inbound ports", "no hub", and "no overlay network" cannot all hold at once**. A machine with no listener and no always-on relay can only be reached if it dials out to something (a hub) or sits on an overlay that handles NAT (Tailscale/WireGuard, which is itself an outbound UDP socket plus relays). Every option below picks two of the three.

## 2. Options — how each would work for Driga

**(1) SSH + `driga serve --stdio`.** The reader runs `ssh <host> driga serve --stdio` and speaks NDJSON over stdin/stdout, exactly like `git` over SSH or MCP's stdio transport. `serve --stdio` is a *reader* (read-only DB connection), so the observed machine's collector stays network-free. The reader keeps one session per peer open and streams events; a dropped session marks the peer stale. Identity, encryption, host keys, agents, jump hosts and `~/.ssh/config` names come from SSH. Zero new dependencies if Driga shells out to the system `ssh` (as it does for `git`); `golang.org/x/crypto/ssh` v0.58.0 (BSD-3, client+server, 2026-10-09) is available if an in-process client is preferred (+2.7 MB measured). Requires sshd reachable on each observed Mac: macOS Remote Login (System Settings › General › Sharing › Remote Login, per Apple's guide; off by default on a fresh install **[unverified against an Apple page]**) or Tailscale SSH (server side supported on Linux and the open-source `tailscaled` macOS variant only; it claims port 22 on the Tailscale IP and leaves `sshd_config` untouched).

**(2) HTTP/JSON + mTLS + pairing.** `driga serve --listen :7xxx` with a self-signed per-machine certificate; device ID = hash of the certificate (Syncthing's model: SHA-256 of the DER cert, base32 with check digits; trust = the ID is in the peer's config). Pairing by typing/scanning a code. Streaming via NDJSON or SSE on `GET /v1/events?follow=1&since=<seq>` — Go stdlib only. Driga owns a small CA/pairing/rotation surface forever. Inbound listener on each laptop (macOS firewall, when on, pops an allow/deny alert for unlisted apps). LAN-only unless the user brings a VPN.

**(3) HTTP over a private network.** Two variants:
- **3a BYO tailnet + Tailscale Serve** — `driga serve` binds localhost; `tailscale serve` exposes it as `https://mac.tailnet.ts.net` with automatic certs and adds `Tailscale-User-Login`, `Tailscale-User-Name`, `Tailscale-User-Profile-Pic` headers (stripped if a client sends them; Funnel traffic gets none). Driga checks the login equals the owner. No Driga-side auth code; MagicDNS gives discovery; NAT/roaming handled; nothing listens on physical interfaces. Requires Tailscale installed (free Personal plan: up to 6 users, unlimited user devices) or Headscale (BSD-3, v0.29.5, "not associated with Tailscale Inc.", single tailnet, min client v1.80 for v0.29.4). Whether `tailscale serve` HTTPS certs work under Headscale **[unverified]**.
- **3b embedded tsnet** — `tailscale.com/tsnet` v1.104.1 (BSD-3, 2026-10-07) runs a full node in-process on a gVisor userspace stack; `Server{ControlURL, AuthKey, Hostname, Dir, Ephemeral}`, `Listen/ListenTLS/ListenFunnel`, `LocalClient().WhoIs()` for caller identity; `ControlURL` points at Headscale. Builds with `CGO_ENABLED=0` (verified). Cost: a hello-world is 20.5 MB vs 1.2 MB empty → **≈ +18–19 MB** on Driga's 13.3 MB; needs an auth key or browser login per machine and a Tailscale/Headscale account; a second Tailscale node per machine.
- **Plain WireGuard** (wireguard-go, MIT-style, macOS via utun) gives encryption but no key distribution, NAT traversal, or naming → everything above has to be built anyway. Not pursued.

**(4) gRPC / Connect with mTLS.** `google.golang.org/grpc` v1.84.0 (Apache-2.0, 2026-09-17): server-streaming `WatchEvents`, mTLS. Measured 11.3 MB hello → ≈ +5 MB. Needs a proto schema that duplicates the JSON envelope (or `Struct`), HTTP/2, and a grpc-web proxy for a PWA. Connect (`connectrpc.com/connect/v2`, Apache-2.0, "stable") removes most of that: plain `net/http`, HTTP/1.1, JSON, browsers, streaming; measured 8.4 MB (v1.21.1 + protobuf). Still codegen and a schema for ~5 endpoints. Pairing/CA problem identical to (2).

**(5) MCP over Streamable HTTP as the fleet protocol.** Current revision is **2026-07-28**: stateless, single POST endpoint, `Mcp-Session-Id` and the GET stream removed, no `Last-Event-ID` resumability ("a broken response stream loses the in-flight request"), server→client change notifications only via a long-lived `subscriptions/listen` POST stream limited to list-changed/resource-updated types. Authorization is OPTIONAL but HTTP transports SHOULD use OAuth 2.1 + RFC 9728 protected-resource metadata + Client ID Metadata Documents (DCR deprecated) — a browser-delegated user flow, not daemon-to-daemon. Official Go SDK v1.8.0 (Apache-2.0/MIT, 2026-09-04) has `NewStreamableHTTPHandler` (+≈1.6 MB over net/http). Verdict: MCP is the *agent-facing* API that should read fleet data; it is the wrong shape to *be* the fleet sync (no cursors, no replay, no resumption, auth designed for humans authorizing clients).

**(6) Pub/sub broker.** `nats-server` v2.15.1 (Apache-2.0, 2026-10-09) embeds in Go: `server.NewServer(&Options{DontListen: true, JetStream: true, …})`, `Server.InProcessConn()`, and `nats.InProcessServer(...)` in nats.go v1.54.0 (verified in source). Leaf nodes dial *out* to a hub ("no inbound firewall rule… on the factory side"); the hub listens on 7422; JetStream on leaf and hub need distinct domains. Subjects `driga.<machine_id>.events`, JetStream retains for replay, MQTT v3.1.1 bridge is built in (sessions/retained in JetStream). Measured embedded server+client: 16.6 MB → **≈ +15 MB**. Needs an always-on hub (a laptop can't be it): a Linux box, VPS, or Synadia cloud. mochi-mqtt v2.7.9 (MIT, MQTT v5, embeddable, 7.8 MB hello) is the lighter broker, with the same hub requirement and no replay cursor semantics beyond retained messages.

**(7) Database replication.** All rejected as a transport, for verified reasons:
- Litestream v0.5.17 (Apache-2.0, 2026-08-31) is "a standalone disaster recovery tool for SQLite" (separate process; S3/GCS/Azure/SFTP/WebDAV/file/NATS JetStream object-store replicas). Reading a replica from another machine requires its experimental *SQLite loadable extension* — and `modernc.org/sqlite` v1.60.1's generated library contains no `sqlite3_load_extension` symbol at all (grepped), so Driga's pure-Go binary cannot load it.
- cr-sqlite (MIT) is a native loadable extension (Rust toolchain, `load_extension(...)`, "main may not be fully stable"): same blocker, and Driga has no multi-writer conflicts to solve — rows are partitioned by `machine_id`.
- LiteFS (Apache-2.0) is FUSE-based and "currently in a beta state"; not a macOS laptop story.
- Syncing `driga.db` with Syncthing/iCloud is unsafe: SQLite's own guidance says a copy taken mid-transaction, or a database separated from its `-wal`, can corrupt.
- **7b file-drop (found during research):** each machine writes an atomic per-machine JSON snapshot/feed file into a directory the user already syncs (Syncthing is MPL-2.0 and OSS; iCloud/Dropbox otherwise). Zero protocol, zero ports, offline-friendly. Latency is whatever the sync provider does (seconds to minutes, unpredictable), there is no push, and privacy depends on the provider. Cheap to add later as a third carrier (`driga fleet export/import`), not a primary.

**(8) P2P libraries.** go-libp2p v0.50.0 (MIT, 2026-09-21) hello with hole-punching + relay: 23.6 MB → ≈ +22 MB, and it needs relay/bootstrap infrastructure to traverse NAT. iroh is Rust with FFI bindings only and public relays run by n0 — cgo, so excluded. tsnet is the Go-world answer to this need, cheaper and more mature.

**(9) Hand-rolled TCP.** Rejected: it would re-implement framing, encryption, auth, reconnect and NAT handling that SSH, TLS and HTTP already provide in the stdlib, and gains nothing (Go's `net/http` is already linked once any HTTP carrier exists). The only acceptable "raw" stream is stdio over SSH, where SSH owns all of the hard parts.

## 3. Comparison table

Scores 1 (bad) – 5 (good). "Weight" is measured added binary size over a cgo-free build.

| Option | Auth by default | No inbound ports | NAT / roam | Push latency | Offline/stale | Setup (2–3 Macs) | Weight | JSON reuse | MCP / web fit | Ops burden | Maturity / license |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 SSH + stdio | 5 (SSH keys; reader = shell-level trust) | 3 (sshd exists already; 5 on Tailscale SSH) | 2 bare / 5 on tailnet | 4 (persistent session, ~1–3 s) | 5 (session drop ⇒ stale) | 5 | 0 (system ssh) / +2.7 MB | 5 | 4 / 2 (phone can't SSH) | 5 (none) | 5 (OpenSSH; x/crypto BSD-3) |
| 2 HTTP + mTLS + pairing | 3 (own CA/pairing code) | 1 | 1 | 5 (SSE) | 5 | 3 (pair each machine) | ~+4 MB (tls) | 5 | 5 / 5 | 4 | 4 (stdlib; pairing is ours) |
| 3a HTTP via Tailscale Serve | 5 (tailnet identity headers) | 5 | 5 | 5 (SSE) | 5 | 4 (if on Tailscale) | 0 | 5 | 5 / 5 | 4 (Tailscale/Headscale) | 4 (SaaS or Headscale) |
| 3b embedded tsnet | 5 (WhoIs) | 5 | 5 | 5 | 5 | 3 (auth key per machine) | **+18–19 MB** | 5 | 5 / 5 | 3 | 4 (BSD-3; account needed) |
| 4 gRPC / Connect + mTLS | 3 (same as 2) | 1 | 1 | 5 (stream) | 5 | 3 | +5 MB / +2 MB | 2 (proto schema) | 2 / 3 (Connect 4) | 4 | 5 (Apache-2.0) |
| 5 MCP Streamable HTTP | 2 (OAuth 2.1 user flows) | 1 | 1 | 2 (listen stream, no resume) | 2 (no cursors) | 2 | +1.6 MB | 3 | 5 / 3 | 3 | 3 (spec moves yearly) |
| 6 NATS leaf + hub | 4 (creds/TLS; hub sees all) | 5 (leaves dial out) | 5 | 5 | 4 (JetStream replay) | 2 (needs a hub) | **+15 MB** | 4 (payloads) | 4 / 4 (WebSocket) | 2 (hub) | 5 (Apache-2.0) |
| 6b MQTT (mochi) | 3 | 5 | 5 | 5 | 3 (retained only) | 2 | +6.6 MB | 4 | 3 / 3 | 2 | 4 (MIT) |
| 7 DB replication | 2 (cloud bucket) | 5 | 5 | 1 (restore-based) | 3 | 1 | n/a (ext. process / cgo) | 2 | 2 / 2 | 2 | 3 (ext. loading impossible) |
| 7b file-drop sync | 3 (provider) | 5 | 5 | 1–2 (no push) | 4 | 4 | 0 | 5 | 3 / 2 | 3 | 4 |
| 8 libp2p / iroh | 3 | 4 | 4 (needs relays) | 4 | 4 | 2 | **+22 MB** / cgo | 4 | 2 / 2 | 2 | 3 |
| 9 raw TCP | 1 | 1 | 1 | 5 | 4 | 2 | 0 | 5 | 1 / 1 | 3 | 1 (all ours) |

## 4. Deep-dive on the top three

### 4.1 SSH + `driga serve --stdio` (v1.5 candidate)

Protocol over the pipe (same JSON as `--json`): a `hello` line (`machine_id`, hostname, schema, capabilities), then request lines (`{"op":"snapshot","view":"inbox"}`) answered by one envelope each, and `{"op":"follow","since_seq":N}` which turns the stream into NDJSON events `{"schema":1,"machine_id","seq","ts","kind","data"}` until EOF. `seq` is a per-machine monotonic cursor backed by a rowid in `events`/`inbox_items`, so a reconnect resumes without replaying 30 days. The reader side is a new collector loop ("fleet puller") that writes remote rows under their own `machine_id`, keeping the one-writer rule; `driga machines` lists peers with `last_seen`/`freshness`, and `--machine mini` filters. Latency path: hook → spool → collector → DB → `serve` polls `data_version` at 1 s → ssh → reader DB: ~1–3 s, within the "seconds" target. Failure semantics are exactly journey 7: session gone ⇒ machine shown stale with its last `observed_at`, `coverage_gaps` records the hole.

Why this answers "why SSH": it is the only option with **no new trust surface** (an SSH login already implies full read access to `~/.claude`, so the fleet feed strictly reduces exposure), **no new listener** (uses the one the user already opened), **no new dependency** (system `ssh`, like `git`), and it works over whatever network the user already has (LAN, Tailscale, WireGuard, ZeroTier). Its real costs: Remote Login or Tailscale SSH must be on; reaching a sleeping Mac is impossible (true for every option); a phone/PWA cannot be the reader; and it is reader-pull, so the reader's daemon must run for push-like latency.

### 4.2 HTTP/1.1 + SSE over a tailnet or mTLS (v2 carrier)

Same feed and ops, mapped to `GET /v1/{summary,agents,inbox,…}` (envelopes) and `GET /v1/events?follow=1&since=N` (NDJSON/SSE), Go stdlib only. Two identity modes: (a) **Tailscale Serve** — bind 127.0.0.1, run `tailscale serve --bg`, trust `Tailscale-User-Login` only when the request arrived via the loopback proxy; zero crypto code, MagicDNS discovery, works for a PWA on a phone on the tailnet; (b) **mTLS + device IDs** when no tailnet exists — Syncthing-style certificate-hash IDs, a `driga fleet pair` code, cert pinning in config. The MCP server (v1.5) and the web UI (v2) mount on the same mux (`/mcp` via the Go SDK's Streamable HTTP handler, `/` for the PWA). This is the carrier that unlocks phones and browsers; its only weakness is that the mTLS path means owning pairing code, which is why Serve should be the recommended mode and mTLS the fallback.

### 4.3 NATS hub with leaf nodes (v2 contender, only if a hub is accepted)

Each machine embeds `nats-server` (`DontListen`, in-process client) as a leaf that dials the hub; it publishes its feed to `driga.<machine_id>.events` and the hub's JetStream retains it. Readers (and a PWA via NATS WebSocket) subscribe; `seq` maps to stream sequence; offline peers show as stale by last message time. Strengths: true push, no inbound ports anywhere, replay for free, and Litestream could replicate the SQLite file itself into the hub's object store for history. Costs: +15 MB in every binary, a hub that someone runs 24/7 (a Linux box qualifies; a laptop doesn't), the hub sees every title/prompt (self-hosting it is the privacy answer), and JetStream domain/leaf configuration that OSS users will have to get right. Keep it as the documented upgrade path, not the default.

## 5. Recommendation and phased path

**Wire format (decide now, independent of transport):** keep the envelope unchanged and add one thing: a per-machine event feed with a monotonic `seq` cursor, `machine_id` on every line, and a `hello` capability record. Every carrier below moves exactly this. MCP tools return the same envelopes, so agents on any machine see the merged fleet view from their local DB.

**v1.5 — transport = SSH, discovery = `~/.ssh/config`.** Ship `driga serve --stdio`, `driga machines add <name> [--ssh dest]`, the fleet-puller loop in the collector, `driga machines`, and `--machine`. Fleet is opt-in; a machine shares nothing until a peer with SSH access asks, and `fleet.share_text = titles|snippets|none` gates what leaves the machine regardless of local `capture_text`. Document two ways to make a Mac reachable: Remote Login (LAN/VPN) or Tailscale SSH. No pairing code, no CA, no new dependency, no change to the observed machine's collector. MCP v1.5 stays stdio-local and reads the merged DB.

**v2 — add the HTTP carrier, same feed.** `driga serve --listen 127.0.0.1:port` with the envelope routes, SSE follow, `/mcp`, and the PWA. Identity via Tailscale Serve headers first; mTLS + Syncthing-style device-ID pairing as the no-tailnet fallback. Push notifications for the PWA need an always-on reachable endpoint: on a tailnet that's any always-on machine running `driga serve`; without one it's a hub, at which point evaluate NATS leaf nodes (4.3) against a plain `driga hub` that is just the HTTP server in aggregate mode. Do **not** embed tsnet by default (+18–19 MB, account dependency); if demand appears, make it a build tag.

**Rejected:** MCP as the fleet protocol (wrong shape; yearly spec churn), gRPC (schema duplication, browser hostility), DB replication (pure-Go cannot load the extensions; Litestream is DR), libp2p/iroh (weight/cgo), raw TCP (nothing to gain). Connect and file-drop sync are the two "reasonable if needs change" alternatives.

## 6. Risks and open questions (only the maintainer can answer; most important first)

1. **Is "SSH must be reachable" an acceptable precondition for OSS users?** If a meaningful share of users won't enable Remote Login or run Tailscale, option 2's pairing code moves from v2 into v1.5 and the no-new-trust-surface argument weakens.
2. **Will v2 require a hub?** PWA push to a phone that is not on a tailnet needs an always-on endpoint. If "no hub, ever" is a principle, Tailscale/Headscale becomes the documented v2 requirement for phones.
3. **May Driga recommend Tailscale (a commercial service) in its docs, with Headscale as the self-hosted alternative?** Whether `tailscale serve` certificates and Tailscale SSH work under Headscale is unverified and should be tested before docs promise it.
4. **Which direction, and who writes?** Reader-pull (recommended) keeps the observed collector network-free but means the reader's daemon makes outbound SSH calls — the first network activity in a Driga daemon. Confirm this is acceptable and that the puller lives inside the collector (one writer) rather than writing via the spool.
5. **Cross-machine acks.** Does `driga inbox ack` on machine B acknowledge on A? Over SSH that is a `{"op":"ack"}` line that `serve --stdio` turns into a spool event on A — a remote *write*, which changes the "serve is read-only" story. Decide now; it shapes the hello capabilities.
6. **What leaves the machine by default?** Titles only (`fleet.share_text = titles`) matches §13, but `--machine mini agent <id>` detail views will want prompts if the user opted in locally. Confirm the fleet gate is separate from `capture_text`.
7. **Linux box role.** Driga's Linux collection returns `ErrUnsupported` today; a Linux box can be a reader/hub in v2 but has nothing to serve until platform support lands.
8. **Feed retention vs. cursor.** `seq` resumption only works within retention (events 30 d, inbox items longer); define what a reader does when `since_seq` is older than retained history (full snapshot + fresh cursor).
9. **LAN discovery.** `grandcat/zeroconf` (MIT, pure Go) says it is untested with Apple Bonjour; shelling out to macOS `dns-sd` or skipping discovery in v1.5 are the choices.
10. **Identity stability.** How `machine_id` survives hostname changes and OS reinstalls, and whether a peer's `machine_id` is pinned on first connect to detect substitution.

## 7. Sources and measurements

Primary sources, all accessed 2026-10-18:

- MCP: versioning (current = 2026-07-28) https://modelcontextprotocol.io/specification/versioning · Streamable HTTP https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http · authorization https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization · changelog https://modelcontextprotocol.io/specification/2026-07-28/changelog · Go SDK https://github.com/modelcontextprotocol/go-sdk (v1.8.0, 2026-09-04/14) https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp
- Tailscale: tsnet https://tailscale.com/kb/1244/tsnet · https://pkg.go.dev/tailscale.com/tsnet (v1.104.1, 2026-10-07, BSD-3) · Serve identity headers https://tailscale.com/kb/1312/serve · Tailscale SSH https://tailscale.com/kb/1193/tailscale-ssh · small binaries https://tailscale.com/kb/1207/small-tailscale/ · pricing https://tailscale.com/pricing · repo (Go 1.27) https://github.com/tailscale/tailscale · LocalAPI `WhoIs` https://pkg.go.dev/tailscale.com/client/local
- Headscale https://github.com/juanfont/headscale · releases (v0.29.5, Oct 13; year not shown on page) https://github.com/juanfont/headscale/releases
- wireguard-go https://git.zx2c4.com/wireguard-go/about/
- NATS: leaf nodes https://docs.nats.io/running-a-nats-service/configuration/leafnodes · MQTT https://docs.nats.io/running-a-nats-service/configuration/mqtt · server (Apache-2.0, v2.15.1, 2026-10-09) https://github.com/nats-io/nats-server · https://pkg.go.dev/github.com/nats-io/nats-server/v2/server · nats.go v1.54.0 `InProcessServer` https://pkg.go.dev/github.com/nats-io/nats.go · `DontListen`/`InProcessConn` confirmed in module source `server/opts.go:424`, `server/server.go:2913`
- mochi-mqtt https://github.com/mochi-mqtt/server (MIT; v2.7.9 measured)
- Litestream https://litestream.io/ · https://github.com/benbjohnson/litestream (Apache-2.0) · releases (v0.5.17, 2026-08-31) https://github.com/benbjohnson/litestream/releases · NATS replica https://litestream.io/guides/nats/
- cr-sqlite https://github.com/vlcn-io/cr-sqlite · LiteFS https://github.com/superfly/litefs · SQLite corruption guidance https://www.sqlite.org/howtocorrupt.html · `modernc.org/sqlite` v1.60.1 generated library has no `sqlite3_load_extension` symbol (grep of `lib/sqlite_darwin_arm64.go` in the module cache)
- Syncthing https://github.com/syncthing/syncthing (MPL-2.0) · device IDs https://docs.syncthing.net/dev/device-ids.html
- go-libp2p https://github.com/libp2p/go-libp2p (MIT; v0.50.0, 2026-09-21) · iroh https://github.com/n0-computer/iroh (Rust, FFI bindings, Apache/MIT)
- gRPC-Go https://github.com/grpc/grpc-go/releases (v1.84.0, 2026-09-17) · Connect https://connectrpc.com/docs/go/getting-started/ · https://github.com/connectrpc/connect-go (Apache-2.0, v2 stable)
- `golang.org/x/crypto/ssh` https://pkg.go.dev/golang.org/x/crypto/ssh (v0.58.0, 2026-10-09, BSD-3)
- Apple: Remote Login https://support.apple.com/guide/mac-help/allow-a-remote-computer-to-access-your-mac-mchlp1066/mac · firewall https://support.apple.com/guide/mac-help/block-connections-to-your-mac-with-a-firewall-mh34041/mac
- zeroconf https://github.com/grandcat/zeroconf (MIT)

Binary-size measurements (Go 1.27.2, darwin/arm64, `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`, minimal programs that exercise each library's server/client path; absolute sizes, not additive):

| Program | Size |
|---|---|
| empty `main` | 1.18 MB |
| net/http + crypto/tls + encoding/json server | 6.70 MB |
| x/crypto/ssh v0.58.0 client + agent | 3.91 MB |
| coder/websocket v1.8.15 server | 5.73 MB |
| MCP go-sdk v1.8.0 Streamable HTTP server | 8.32 MB |
| connect v1.21.1 + protobuf v1.36.12 handler | 8.42 MB |
| mochi-mqtt v2.7.9 broker | 7.80 MB |
| grpc v1.84.0 server + TLS creds | 11.29 MB |
| nats-server v2.15.1 embedded (JetStream) + nats.go v1.54.0 | 16.56 MB |
| tsnet v1.104.1 listener + WhoIs | 20.52 MB |
| go-libp2p v0.50.0 host (hole punching, relay) | 23.65 MB |
| **Driga today** (`./cmd/driga`, same flags) | **13.31 MB** |

---

## Addendum (2026-10-18, later the same day) — decisions taken, options revisited, recommendation revised

Status: the maintainer has since decided the fleet's shape (recorded in `docs/superpowers/specs/2026-10-18-driga-v1.5-design.md` §4.1). This addendum keeps the original text above untouched, records the facts checked for the decision, adds the options that were not in §2, and replaces §5's recommendation. Everything marked **[unverified]** was not confirmed against a primary source.

### A.1 What the maintainer decided (summary; binding text is in the design)

- Standard building blocks if a standard fits; a custom schema on top is acceptable. **No hub** (no central server, broker or relay). Machines are on a private network (LAN, VPC, or an overlay such as Tailscale/Headscale/WireGuard); a VM cannot reach a laptop behind NAT without an overlay, and Driga will not add relays.
- **Viewer pulls.** Peers run `driga serve` (opt-in, explicit private address). Viewer pairs once (verified pairing, mutual TLS), fetches an atomic versioned snapshot, then follows incremental events over **SSE over HTTPS**. Stale peers stay visible. Remote rows are written by the viewer's own collector (single writer). Read-only except inbox acks, a per-viewer permission.
- **Linux collection is in v1.5** (cloud VMs are Linux). `driga mcp` is local stdio. OTLP is only an optional *local* intake from Claude Code/Codex.
- Rejected for the fleet, with the one-line reasons now in the design: OTLP, A2A, MCP Streamable HTTP, Prometheus/OpenMetrics, ActivityPub/Syncthing BEP/CRDTs, SSH as core transport, Raft, libp2p/iroh/relays/brokers. SSH *port-forwarding* is documented for bastion-only hosts, not built.

### A.2 Options revisited or added

**A2A (Agent2Agent) v1.0.** The specification site lists **1.0.0** as the latest released version (previous 0.3.0, 0.2.6, 0.1.0). Core objects: `AgentCard` (identity, capabilities, skills, endpoint, `securitySchemes`/`security`), `Task` (`id`, `contextId`, `status`, `artifacts`, `history`), `Message`, `Artifact`, `Part`. Task states: `TASK_STATE_SUBMITTED`, `WORKING`, `INPUT_REQUIRED`, `AUTH_REQUIRED` (interrupted), `COMPLETED`, `FAILED`, `CANCELED`, `REJECTED` (terminal). Operations: `SendMessage`, `SendStreamingMessage`, `GetTask`, `ListTasks`, `CancelTask`, `SubscribeToTask`, push-notification-config CRUD, `GetExtendedAgentCard`. Three bindings — JSON-RPC, gRPC, HTTP+JSON/REST — required to be functionally equivalent; security scheme types include APIKey, HTTPAuth, OAuth2, OpenIdConnect and **MutualTls**. Go SDK `github.com/a2aproject/a2a-go/v2`: v2.6.0 (2026-09-25), Apache-2.0, Go ≥ 1.25, "adheres to the A2A v1.0 Protocol Specification", bindings for gRPC, REST and JSON-RPC; no stability label on the README **[verified]**. The well-known discovery path and the JSON-RPC method strings were in truncated sections **[unverified]**. **Verdict:** right standard for *dispatching work* (v2): a task lifecycle with `INPUT_REQUIRED` maps onto "waiting for the user" and client→server HTTP matches viewer-pull. Wrong shape for *observing* a machine: projects, processes, orphans and machine health are not tasks, and a snapshot+feed of state is not a message exchange. Reserved for v2; the v1.5 wire format neither assumes nor blocks it.

**OpenTelemetry / OTLP.** Facts: OTLP spec **1.11.1**, status *Stable* for traces, metrics and logs; OTLP/HTTP paths `/v1/logs`, `/v1/metrics`, `/v1/traces`; `Content-Type: application/x-protobuf` or `application/json`; JSON uses lowerCamelCase keys, hex trace ids, integer enums, 64-bit integers as decimal strings; receivers MUST ignore unknown fields; 200 = success (with optional `partial_success`), 400 must not be retried, 429/502/503/504 should be retried honouring `Retry-After`; servers MUST support gzip; HTTP/1.1 or HTTP/2; default ports 4317 (gRPC) / 4318 (HTTP) **[verified]**. Go: `go.opentelemetry.io/otel` **v1.47.0** (2026-10-02) is "the first stable release of the OpenTelemetry Go Logs API and SDK" (`otel/log`, `otel/sdk/log`) **[verified]**; collector `pdata` v1.69.0 (2026-10-14, logs stable) **[verified]**; `go.opentelemetry.io/proto/otlp` v1.11.1 (2026-09-30) **[verified]**. GenAI semantic conventions **moved** (June 2026, semconv v1.42.0) to `github.com/open-telemetry/semantic-conventions-genai`; the agent-span document there is marked **Status: Development** and defines `gen_ai.operation.name` (`create_agent`/`invoke_agent`/`execute_tool`), `gen_ai.agent.{id,name,description,version}`, `gen_ai.conversation.id`, `gen_ai.provider.name`, `gen_ai.request.model`, `gen_ai.usage.{input,output}_tokens`, opt-in `gen_ai.input.messages`/`gen_ai.output.messages`; only `error.type`, `server.address`, `server.port` are Stable **[verified]**. *Why it led for a while:* under "standard + no hub" a push of Driga state as OTel logs/metrics with `driga.*` attributes over OTLP/HTTP looked like the only fully standard, hub-less option, and the agents already speak it. *Why it was dropped for the fleet:* OTLP is one-way telemetry export. It has no state query (no "give me everything now"), no cursor or resume (a lost batch is lost; partial success is advisory), no channel for acks, and its receivers are heavy — a `pdata` receiver-only program is 12.69 MiB and the `proto/otlp` collector packages link gRPC (11.90 MiB); the cheapest protobuf route (message types + `protowire`) is 8.23 MiB vs 6.39 MiB for plain HTTP+TLS+JSON. Everything Driga needs (snapshot, cursor, resume, ack, pairing) would be invented on top anyway, at which point JSON+SSE on the same TLS connection is simpler and smaller. *Retained:* as the **local intake** for agent telemetry (JSON only, stdlib), where the agent, not Driga, chose the protocol.

**Claude Code telemetry (for the intake).** `CLAUDE_CODE_ENABLE_TELEMETRY=1`; `OTEL_LOGS_EXPORTER=otlp|console|none`, `OTEL_METRICS_EXPORTER=otlp|prometheus|console|none`; `OTEL_EXPORTER_OTLP_PROTOCOL` has **no default** and accepts `grpc`, `http/protobuf`, `http/json`; per-signal `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT`/`_PROTOCOL`; `OTEL_LOGS_EXPORT_INTERVAL` default 5000 ms (metrics 60000 ms); prompts redacted by default (`OTEL_LOG_USER_PROMPTS`), tool details off by default (`OTEL_LOG_TOOL_DETAILS`); events `claude_code.user_prompt`, `assistant_response`, `tool_result` (`decision_source`), `api_request`, `api_error`, `api_refusal`, `tool_decision` (`decision` accept/reject; `source` ∈ `config`, `hook`, `user_permanent`, `user_temporary`, `user_abort`, `user_reject`), `permission_mode_changed`, `auth`, `mcp_server_connection`, `subagent_completed`, `skill_activated`, `at_mention`, plugin events; standard attributes include `session.id` (default on), `user.id`, `user.email` (when available), `organization.id`, `terminal.type`; traces are beta (`CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1`, `OTEL_TRACES_EXPORTER`) with spans `claude_code.interaction` → `llm_request`/`tool` → `claude_code.tool.blocked_on_user` (permission wait, `decision`, `source`) and `tool.execution`; repository-level `.claude/settings.json` cannot set exporter variables (user/managed settings can); `service.name` is `claude-code` or `claude-code-desktop` **[verified, code.claude.com/docs/en/monitoring-usage]**. Hooks: `Notification` matcher values include `permission_prompt`, `idle_prompt`, `agent_needs_input`, `agent_completed`; `PermissionRequest` fires when a tool call needs a decision and can decide **[verified, code.claude.com/docs/en/hooks]**. Whether Claude Code can export to more than one endpoint per signal: the page shows one endpoint per signal **[not explicitly stated — unverified]**.

**Codex telemetry.** Official config reference (`learn.chatgpt.com/docs/config-file/config-reference`, redirected from developers.openai.com): `[otel]` keys `environment` (default `dev`), `log_user_prompt` (bool), `exporter` = `none | otlp-http | otlp-grpc` with `otel.exporter.<id>.endpoint`, `.protocol` = `binary | json`, `.headers`, `.tls.{ca-certificate,client-certificate,client-private-key}`; `metrics_exporter` = `none | statsig | otlp-http | otlp-grpc` (default `statsig`); `trace_exporter` with the same sub-keys; project-local `.codex/config.toml` `[otel]` is ignored **[verified]**. Event names (`codex.user_prompt`, `codex.tool_result`, `codex.api_request`, `codex.sse_event`, `codex.conversation_starts`) appear only in third-party vendor docs **[unverified]**; whether the endpoint is used verbatim (no `/v1/logs` appended) is also from vendor docs **[unverified]**.

**GitHub self-hosted runner pattern.** The runner "connect[s] to GitHub to receive job assignments and download new versions of the runner application"; "the host machine must be able to make outbound HTTPS connections over port 443"; required hosts `github.com`, `api.github.com`, `*.actions.githubusercontent.com` **[verified, docs.github.com reference page, section "Communication"]**. The ~50-second long-poll detail was not on the fetched page **[unverified]**. **Verdict:** outbound-only is attractive for NAT, but the thing being dialled is a coordinator that holds the queue — a hub. Rejected under "no hub". (If the maintainer ever relaxes that, this is the pattern to copy: outbound HTTPS long-poll to a self-hosted `driga hub`.)

**Raft.** "Typical consensus algorithms make progress when any majority of their servers is available … a cluster of 5 servers can continue to operate even if 2 servers fail" **[verified, raft.github.io]**. **Verdict:** Driga has no shared writes to agree on (every row has one authority, its machine), and a fleet of laptops that sleep cannot keep a quorum. Rejected.

**Windows OpenSSH.** Microsoft Learn (page updated 2026-09-08): prerequisites Windows Server 2019 / Windows 10 build 1809 or later; OpenSSH Client and OpenSSH Server are *Features on Demand* installed via Settings › Optional Features or `Add-WindowsCapability` by an administrator (`Get-WindowsCapability` shows both `NotPresent` when neither is installed); installing the server creates firewall rule `OpenSSH-Server-In-TCP` (port 22); "Beginning with Windows Server 2025, OpenSSH is now installed by default" but `sshd` must be enabled (Server Manager › Remote SSH Access, or `Start-Service sshd` + `Set-Service -StartupType Automatic`) **[verified]**. The claim that the *client* is usually present on Windows 10 1809+/11 is not stated on that page **[unverified]**. **Consequence:** SSH as the core transport would require an administrator-installed, enabled sshd on every observed Windows host (and Remote Login on every Mac); dropped as core transport, kept as a documented port-forward for bastion-only hosts.

**MCP Streamable HTTP, Prometheus/OpenMetrics, ActivityPub, Syncthing BEP, CRDTs.** MCP over HTTP remains as analysed in §2 option 5 (no cursors/resume; auth designed for human-authorized clients; Go SDK v1.8.0 negotiates 2026-07-28 and caps stdio frames via `StdioTransport.MaxLineLength` **[verified]**) — credible but more complex than JSON+SSE for the same job; `driga mcp` stays stdio. Prometheus exposition is numeric samples only and would turn session titles/paths into labels (cardinality). ActivityPub, Syncthing's BEP and CRDT replication solve multi-writer convergence; Driga has exactly one writer per row, so they add machinery without removing a problem.

### A.3 Pairing: what a short code over the network can and cannot do

- A pairing code typed on one side and sent over an unauthenticated TLS connection can be intercepted by an active attacker on the path (the Codex review's point). Two standard remedies:
  1. **PAKE** (the code is a password that both sides prove knowledge of without revealing it). **SPAKE2 is RFC 9382** (Informational, IRTF/CFRG, September 2023) **[verified]**. **CPace is still an Internet-Draft** (draft-irtf-cfrg-cpace-21, 23 April 2026; Datatracker "In IESG Review", last updated 2026-06-08) **[verified]**. Go implementations found: `github.com/CzarJoti/gospake2` v0.1.6 (2026-02-10, MIT, claims RFC 9382; README imports a different module path, `ValiantChip/gospake2`), `salsa.debian.org/vasudev/gospake2` (pseudo-version 2021-05-10, MIT/GPL-3, Ed25519 group, interoperates with python-spake2, "has not been formally audited by a cryptographer"), `jtejido/spake2` ("NOT FOR PROD") **[verified]**. No audited, maintained, cgo-free Go PAKE was found; no Go CPace at all.
  2. **Short authentication string with hash commitment** (ZRTP, RFC 6189 §4.4.1.1): "The use of hash commitment in the DH exchange constrains the attacker to only one guess to generate the correct Short Authentication String (SAS), which means the SAS can be quite short. A 16-bit SAS, for example, provides the attacker only one chance out of 65536 of not being detected." Without the commitment an attacker could run trial computations until two values produced a matching SAS **[verified]**. This needs only hashing and randomness from the standard library.
- **Binding the code to the TLS session.** Go exposes `ConnectionState.ExportKeyingMaterial(label, context, length)` per RFC 5705; it errors unless the connection is TLS 1.3 or uses Extended Master Secret **[verified, pkg.go.dev crypto/tls]**. RFC 9266 ("Channel Bindings for TLS 1.3", Standards Track, July 2022) defines `tls-exporter` as exporter label `EXPORTER-Channel-Binding`, empty context, 32 bytes, valid for TLS 1.3 and for earlier versions only with Extended Master Secret **[verified]**. Driga uses the same mechanism with its own label so that a relayed pairing (two different TLS sessions) yields two different codes.
- **Certificate pinning in Go.** `tls.Config.InsecureSkipVerify` "should be used only for testing or in combination with VerifyConnection or VerifyPeerCertificate"; `VerifyPeerCertificate` "receives the raw ASN.1 certificates provided by the peer" and is considered even when normal verification is disabled (with `verifiedChains` nil); `RequireAnyClientCert` requires a client certificate "but that certificate is not required to be valid" **[verified]**. This is the documented way to replace PKI validation with fingerprint pinning (Syncthing's model, §7 above).
- **Chosen for v1.5:** commitment-bound confirmation code on both screens, with identity pre-authorization (`--allow <viewer-id>` / `--id <peer-id>`) for headless peers; PAKE as the documented upgrade once an audited Go implementation exists. Rationale and threat table are in the design §7.

### A.4 Server-Sent Events (chosen follow transport)

WHATWG HTML Living Standard: MIME type `text/event-stream`; fields `event`, `data`, `id` (sets the last event ID, persists until changed), `retry` (reconnection time in ms); on reconnection the client sends the last event ID in the `Last-Event-ID` request header; clients reconnect when the connection closes (initial delay implementation-defined, "probably in the region of a few seconds"); HTTP **204** tells a client to stop reconnecting **[verified]**. Go needs nothing beyond `net/http` and `http.Flusher`; the viewer implements the same header semantics in its own client.

### A.5 Linux facts (for the platform port)

- PSI: files `/proc/pressure/{cpu,memory,io}`; two lines `some avg10= avg60= avg300= total=` and `full …`; averages are percentages of time over 10/60/300 s windows, `total` is cumulative stall time in µs; `some` = at least some tasks stalled, `full` = all non-idle tasks stalled **[verified, docs.kernel.org]**. The introducing kernel version (4.20) and `CONFIG_PSI`/`psi=1` were not on the fetched page **[unverified]**.
- `kardianos/service` v1.3.0: supports "Windows, Linux/(systemd | Upstart | SysV | OpenRC), and OSX/Launchd"; POSIX option `UserService` (bool, default false) "Install as a current user service"; `Restart` (default `always`), `LimitNOFILE`, `SystemdScript` for a custom unit template **[verified]**. Driga already sets `UserService: true` for launchd; the same option yields a systemd user unit.
- gopsutil v4 (already a dependency) implements Linux process and memory collection over `/proc` without cgo; field-level behaviour on a real VM is **[unverified]** until the Linux fixture exists.

### A.6 mDNS / Bonjour (optional LAN discovery — not in v1.5)

`github.com/libp2p/zeroconf/v2` v2.2.0 (2022-08-07, MIT, pure Go): "should be compatible to Avahi (tested) and Apple's Bonjour (untested)"; `Register(instance, service, domain, port, text, ifaces, …)`, `Browse(ctx, service, domain, entries, …)` **[verified]**. `github.com/brutella/dnssd` v1.2.14 (2024-10-22, MIT): "passes the multicast DNS tests of Apple's Bonjour Conformance Test"; API references `miekg/dns` types **[verified]**. A throwaway build of `libp2p/zeroconf/v2` on Go 1.27.2 failed to link (`golang.org/x/net/internal/socket: invalid reference to syscall.recvmsg`) **[observed once; not investigated]**. On macOS the system `dns-sd` CLI, and on Linux `avahi-browse`, are the cgo-free way to shell out if discovery is ever wanted. Recommendation unchanged: no discovery in v1.5; addresses are typed.

### A.7 Measurements (sizes2; Go 1.27.2, darwin/arm64, `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`, absolute sizes, MiB = 1 048 576 bytes)

| Program | Size |
|---|---|
| empty `main` | 1.12 MiB |
| net/http + crypto/tls + encoding/json server (mTLS required) | 6.39 MiB |
| same + self-signed P-256 cert generation + fingerprint pinning via `VerifyPeerCertificate` (client and server) | 6.89 MiB |
| MCP go-sdk v1.8.0 **stdio** server with one typed tool | 7.29 MiB |
| OTLP/HTTP receiver, `go.opentelemetry.io/proto/otlp` v1.11.1 message types + `protowire` (no collector packages, no gRPC) | 8.23 MiB |
| OTLP/HTTP receiver, `proto/otlp` *collector* packages (`ExportLogsServiceRequest`) — links `google.golang.org/grpc` v1.84.0 | 11.90 MiB |
| OTLP/HTTP receiver, `go.opentelemetry.io/collector/pdata` v1.69.0 (`plogotlp`/`pmetricotlp`) — links gRPC | 12.69 MiB |
| **whole v1.5 network stack** in one binary: mTLS pinning + JSON snapshot + SSE follow + OTLP intake (protowire route) + MCP stdio | **10.36 MiB** |
| **Driga today** (`./cmd/driga`, same flags, 2026-10-18 evening build) | **12.69 MiB** (13 307 202 bytes; the 13.31 MB in §7 is the same binary in decimal MB) |

Notes: `go list -deps ./cmd/driga` shows Driga links `encoding/json` and `net` but **not** `net/http` or `crypto/tls`, so the HTTP+TLS stack is new weight for the fleet (≈ 5 MiB standalone; less after overlap with packages Driga already has). The OTel Go *sender* SDK was not measured (the throwaway program used stale API names and was not needed once Driga stopped emitting OTLP). The JSON-only OTLP intake adds no dependency; the protowire route is the cheapest protobuf option (+1.8 MiB) if ever wanted.

### A.8 Revised recommendation (replaces §5)

**v1.5 — viewer pulls over HTTPS + mTLS; JSON snapshot + SSE feed in Driga's own versioned schema.** Peers opt in (`[fleet] serve = true`, explicit private `listen`). Pairing by a commitment-bound confirmation code shown on both screens (or identity pre-authorization for headless peers), yielding pinned self-signed certificates on both sides; per-viewer permissions (`read`, optional `ack`); revocation closes streams immediately. Feed rows with monotonic `seq` and an epoch; snapshot and cursor taken in one read transaction; re-snapshot on epoch change or retention gap. Remote rows written by the viewer's collector under the peer's `machine_id`. Linux collection ships in the same release so VMs have something to serve. `driga mcp` is local stdio over the same view builders. OTLP is accepted only as a loopback intake of the agents' own telemetry, JSON encoding, stdlib decoder. Standard library carries the whole fleet; MCP is the only new dependency.

**Documented, not built:** overlays (Tailscale/Headscale/WireGuard) for laptops behind NAT; SSH `-L` port-forwarding for bastion-only hosts; host firewall / security-group rules; `loginctl enable-linger` for headless systemd user services.

**v2:** A2A v1.0 for dispatching tasks to machines (same mTLS identities can back its `MutualTls` security scheme); web UI/PWA mounted on the same HTTPS mux; an audited PAKE replacing the confirmation code when one exists in Go; mDNS discovery if asked for.

**Still rejected:** everything in §5's list plus OTLP as the fleet carrier, A2A for observation, Prometheus/OpenMetrics, ActivityPub/BEP/CRDTs, the runner-style outbound-to-coordinator pattern (hub), and Raft.

### A.9 Open questions carried into the design

See the design document §18 (listen-address policy, merged views default, pairing method acceptance, `share_text` default, Linux service scope, OTLP intake encoding). The original §6 questions are resolved as follows: Q1 (SSH precondition) — moot, SSH dropped; Q2 (hub for v2) — no hub, overlays are the documented path; Q3 (recommending Tailscale) — documented as one overlay option among Headscale/WireGuard, no endorsement; Q4 (direction/writer) — viewer pulls, puller lives in the collector; Q5 (cross-machine acks) — yes, per-viewer permission, via the peer's spool; Q6 (what leaves by default) — `share_text = none`, separate from `capture_text` (design §18 Q4 asks whether `titles` should be the default); Q7 (Linux role) — Linux collection is in v1.5; Q8 (feed retention vs cursor) — re-snapshot; Q9 (LAN discovery) — none in v1.5; Q10 (identity stability) — certificate fingerprint pinned at pairing, `machine_id` bound in hello, `driga machine reset-id` for clones.

### A.10 Additional sources (accessed 2026-10-18)

- A2A specification https://a2a-protocol.org/latest/specification/ · a2a-go https://github.com/a2aproject/a2a-go · https://pkg.go.dev/github.com/a2aproject/a2a-go/v2 (v2.6.0)
- OTLP specification 1.11.1 https://opentelemetry.io/docs/specs/otlp/ · OTel Go releases (v1.47.0 "first stable release of the OpenTelemetry Go Logs API and SDK") https://github.com/open-telemetry/opentelemetry-go/releases · https://pkg.go.dev/go.opentelemetry.io/otel/sdk/log · pdata https://pkg.go.dev/go.opentelemetry.io/collector/pdata (v1.69.0) · proto/otlp https://pkg.go.dev/go.opentelemetry.io/proto/otlp (v1.11.1)
- GenAI semantic conventions: moved notice https://opentelemetry.io/docs/specs/semconv/gen-ai/ · new repo https://github.com/open-telemetry/semantic-conventions-genai · agent spans (Status: Development) https://raw.githubusercontent.com/open-telemetry/semantic-conventions-genai/main/docs/gen-ai/gen-ai-agent-spans.md
- Claude Code monitoring https://code.claude.com/docs/en/monitoring-usage · hooks https://code.claude.com/docs/en/hooks
- Codex configuration reference https://learn.chatgpt.com/docs/config-file/config-reference (redirect target of https://developers.openai.com/codex/config-reference)
- MCP Go SDK releases (v1.8.0, 2026-09-14) https://github.com/modelcontextprotocol/go-sdk/releases
- GitHub self-hosted runners reference ("Communication") https://docs.github.com/en/actions/reference/runners/self-hosted-runners
- Raft https://raft.github.io/
- Windows OpenSSH https://learn.microsoft.com/en-us/windows-server/administration/openssh/openssh_install_firstuse
- SPAKE2 RFC 9382 https://datatracker.ietf.org/doc/rfc9382/ · CPace draft https://datatracker.ietf.org/doc/draft-irtf-cfrg-cpace/ · Go SPAKE2 candidates https://pkg.go.dev/github.com/CzarJoti/gospake2 · https://pkg.go.dev/salsa.debian.org/vasudev/gospake2
- ZRTP RFC 6189 (§4.4.1.1 hash commitment and SAS) https://www.rfc-editor.org/rfc/rfc6189.html · RFC 9266 tls-exporter https://www.rfc-editor.org/rfc/rfc9266.html · Go `crypto/tls` (`ExportKeyingMaterial`, `VerifyPeerCertificate`, `InsecureSkipVerify`, `RequireAnyClientCert`) https://pkg.go.dev/crypto/tls
- Server-Sent Events https://html.spec.whatwg.org/multipage/server-sent-events.html
- PSI https://docs.kernel.org/accounting/psi.html · kardianos/service https://pkg.go.dev/github.com/kardianos/service
- mDNS: https://pkg.go.dev/github.com/libp2p/zeroconf/v2 · https://pkg.go.dev/github.com/brutella/dnssd
- Measurement programs: `/private/tmp/claude-501/-Users-v-Projects-labstack-driga/4641c674-18bb-43aa-b7e2-66e8f3fa3a02/scratchpad/sizes2/` (throwaway; nothing committed)
