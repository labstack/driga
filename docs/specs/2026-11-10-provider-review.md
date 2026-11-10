# Assistant provider review

Base: `affa6e64807cb775181af3d4a2d6f4ae3bc35aa5` (released main).
Source candidate: `3f8caa7` (including `2b89669`).
Spec: [Assistant providers](2026-11-10-assistant-providers.md), issue #103.

## Scope

Reviewed the entire feature diff: provider routing, Claude/Codex/Copilot
adapters, host MCP forwarding, subprocess environment/lifetimes, config,
provider-qualified store and collector exclusion, hook/spool handling, CLI/service
transport, TUI/UI labels, stories, generated docs and host bundle. No migration,
agent-settings edits, real vendor turns or production cleanup were performed.

## Standards

A fresh independent reviewer checked repository standards and the code-review
skill's smell baseline. Initial findings were resolved:

- Removed the variadic `Open` compatibility signature; callers supply one
  preference explicitly.
- Normalize inherited environment keys before filtering, including on Windows.
  Hostile mixed-case approval, permissive Copilot and Node settings are covered.

Final candidate and the service-override follow-up have no remaining standards
blocker. The reviewer performed read-only review; execution evidence is below.

## Spec

A separate fresh reviewer checked implementation against the issue and spec.
Missing/null Copilot plugin-hook acknowledgment previously looked like zero;
the adapter now requires a present, non-null zero count before sending a prompt.
Regression fixtures prove missing/null/nonzero responses never receive it.

Provider choice is stored before dispatch, resumes retain provider/session IDs,
and failures are not replayed elsewhere. The final service override is scoped to
new conversations, validates requests before persistence, and leaves the daemon
default and existing sessions unchanged. Its additive acknowledgment rejects
older services before a prompt is submitted. No remaining spec blocker.

## Astra and user checks

Astra audited tool/approval ownership and independently reviewed the transports.
Resolved findings include per-turn Codex tool-name checks, call identity checks,
panel capture/result mapping, and inherited `CODEX_EXEC_SERVER_NOISE_*` settings
that could override a disabled execution environment. The launcher now strips
the entire inherited namespace before explicitly setting `URL=none`.

Official tag `rust-v0.167.0` resolves to `086ccc833bc9cf01024ad2dd6af7dbd421b6bf9a`
and contains the environment-disable implementation. The adapter accepts only
that exact release; future patches require another audit.

A separate agent exercised isolated help/config and fake-provider workflows as
a user. It found that the service silently ignored CLI preferences and that help
did not explain selection. Both are fixed and independently re-reviewed. The
user agent reran provider/remote/old-service cases and found no new blocker.

## Validation and limits

- Isolated Go suite; affected-package race checks across chat, llm, collector,
  config, store and TUI; targeted remote-override normal/race checks.
- Native and Windows vet; Linux build; generated-doc consistency.
- 212 UI unit tests and 1,224 browser-story tests; UI bundles regenerated.
- Gitleaks and diff whitespace checks pass.
- An initial macOS open-file timeout passed when retried and in the subsequent
  full suite. The collector heap-budget check passed in that full run.

Synthetic subprocess and protocol fixtures establish Driga's routing and refusal
behavior, not live vendor-service certification. No real authentication/model
turns were run. Copilot requires protocol 3, safety acknowledgment and an exact
custom-tool catalog. Codex requires 0.167.0 and default OpenAI configuration.
Codex/Copilot expose a smaller action catalog than Claude; direct MCP cleanup,
acknowledgment, jump, delegated prompt and summary actions remain Claude-only.
Project summaries retain their existing Claude model and budget contract.
