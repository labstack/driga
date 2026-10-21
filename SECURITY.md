# Security policy

Driga reads agent histories, process metadata, and local Git state. It can
share collected data with paired machines and perform explicitly requested
cleanup. Security reports are taken seriously and are welcome.

## Reporting a vulnerability

**Do not open a public issue.**

Report privately through either channel:

- [GitHub private vulnerability reporting](https://github.com/labstack/driga/security/advisories/new)
  — preferred, keeps the report and the fix in one place.
- Email <security@labstack.com>. General open-source questions can go to
  <opensource@labstack.com>.

Please include what you can: affected version (`driga version`), a description
of the issue, the impact you believe it has, and steps or scrubbed fixtures that
reproduce it. A partial report is better than no report; do not wait until you
have a full exploit. Do not include private transcripts or credentials.

We will acknowledge your report within 3 business days and give you an
assessment and expected timeline within 10 business days. You will be kept
informed as the fix progresses, and credited in the advisory unless you prefer
otherwise.

Please give us a reasonable opportunity to release a fix before disclosing
publicly. We will not pursue legal action against anyone acting in good faith
under this policy.

There is no bug bounty program.

## Scope

Driga assumes the operator's workstation and deliberately paired machines are
trusted. In scope:

- Unintended disclosure of agent data or credentials through storage, logs,
  exports, the web dashboard, MCP tools, or fleet sharing.
- Bypasses of fleet identity pinning, pairing, or per-viewer grants.
- Web access-token or loopback-listener bypasses.
- Command injection or cleanup that acts outside the confirmed target.
- Process identity checks that allow signaling an unrelated process.
- Hook installation that overwrites unrelated agent configuration.

Out of scope:

- Vulnerabilities in the operating system, Git, or agent CLIs themselves.
- An operator deliberately performing an action they are authorized to perform.
- Anything requiring an already compromised workstation or malicious trusted peer.
- Secret scrubbing as a guarantee that arbitrary text contains no private data;
  reports of specific unintended captures or policy bypasses are welcome.

## Supported versions

Driga is in alpha. Fixes land on the latest release only. There is no
backporting to earlier versions and no long-term support branch.
