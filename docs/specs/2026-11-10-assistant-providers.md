# Assistant providers

Issue: https://github.com/labstack/driga/issues/103

Chat and `driga ask` must work when only one supported assistant CLI is installed.
Collection must remain independent of assistant installation and login.

## Selection and identity

`assistant.provider` defaults to `auto`, selecting the first compatible CLI in
Claude → Codex → Copilot order. `DRIGA_ASSISTANT_PROVIDER` or the config file can
select one explicitly. Selection runs only when creating a conversation; no
background probes, installation, login, or model requests are performed.
Compatibility probes inspect CLI metadata. Users authenticate with their CLI.
Authentication or quota errors do not trigger automatic replay elsewhere.

The selected provider is persisted before the first prompt. Resumption always
uses that provider and exact session ID, even if the preferred CLI changes.
Missing/incompatible CLIs produce an actionable error and leave monitoring alive.
Internal-session exclusion keys are `(provider, session ID)`, including hooks,
so equal vendor IDs cannot hide another provider's workload. Unknown identities
are handled conservatively.

## Provider adapters

The existing `chat.Agent.Run` interface remains the seam. Requests carry the
persisted provider. The broker owns conversation state, cancellation, capture,
trust modes and command approvals for every provider.

Claude keeps its strict MCP configuration and permission bridge. Codex uses
config-free `exec`, retaining authentication in `CODEX_HOME`, ignoring user exec
rules and project configuration through a private working directory. Its native
execution environment is disabled with `CODEX_EXEC_SERVER_URL=none`, including
runtime filesystem/command backstops. The adapter accepts the audited 0.167.0
release; unknown registries fail closed. Only Driga's read catalog and, for action
chat, `list_prompts` and broker-gated `run_command` are enabled. Credentials and
prompts stay out of process arguments. Dashboard edits are currently unavailable
through this Codex adapter; read and panel tools remain available.

Copilot uses framed SDK protocol 3 and a host-owned MCP tool bridge. It retains
the CLI's authentication home but uses a private session config directory and
working directory. Session configuration disables ambient hooks, plugins,
instructions, skills, MCP servers and host Git operations. Before sending a
prompt, Driga requires the safety-options acknowledgment and an exact tool
catalog containing only its custom declarations. Permission requests for native
operations are denied. Unsupported protocol/options/catalogs fail closed.

The host bridge exposes read tools and a bounded action subset. It excludes
model-direct approval, Claim tools and dashboard replace/restore/delete calls.
`run_command` performs approval and execution through the existing broker.
Duplicate host-dispatched provider call IDs, foreign sessions, oversized frames
and incomplete streams fail the turn; children are canceled together. Codex owns
its MCP dispatch; Driga validates its reported tool catalog and call identities
in the output stream, while command authorization remains inside the MCP broker.

Project summaries remain Claude-specific because their model and USD budget
contract needs a separate provider-specific design. Selecting a Chat provider
does not change summary behavior or monitoring-adapter enablement.

## Product presentation

Reference lock: retain the existing Chat and TUI typography, muted status text,
trust-mode controls and layout. The bundled Refero copywriting reference's
orientation/status rule applies: show the actual answering provider, avoid a
Claude label on Codex/Copilot answers. No new provider picker or onboarding flow
is needed; the existing config mechanism provides the explicit override.

## Validation

Tests use disposable homes and synthetic executable/protocol fixtures only.
Cover ordered automatic selection, explicit failures, lazy initialization,
provider-pinned resume after restart, cancellation without replay, config/env
precedence, provider-ID collisions, hook/spool exclusion, native tool rejection,
permission denial, frame limits and child cleanup. Story fixtures verify provider
labels. Live authentication/model turns are deliberately outside test execution;
metadata checks alone are not an end-to-end vendor-service certification.

Primary protocol evidence:

- [Codex environment-disabled tools and runtime backstops](https://github.com/openai/codex/commit/a504d8f0fa70c88f38cbf6c5dae26a1e1223fd63)
- [Codex app-server protocol](https://learn.chatgpt.com/docs/app-server)
- [Codex CLI source/configuration](https://github.com/openai/codex)
- [Copilot SDK protocol and empty mode](https://github.com/github/copilot-sdk)
- [Copilot CLI command reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-command-reference)
