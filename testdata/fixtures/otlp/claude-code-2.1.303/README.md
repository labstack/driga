Captured from Claude Code 2.1.303 through a logging proxy forwarding
OTLP/HTTP JSON `POST /v1/logs` requests to Driga (`application/json`, no
compression). The numbered files retain one record per event name per request
from seven captures, preserving the original numeric `intValue`, double, bool,
array, empty string, body, timestamp, and dropped-attribute shapes.

Account, organization, session, message, request, plugin, and other identifiers
were replaced with synthetic values; home paths were replaced with
`/home/fixture`. Prompt and response text was already `<REDACTED>` by Claude Code.

`encoding-variants.json` is synthetic spec coverage, not a captured request:
string and numeric int64/timestamps, nested AnyValues, empty values, bytes,
hex trace/span IDs, flags, enum numbers, schema URLs, and unknown fields. See
[OTLP JSON encoding](https://opentelemetry.io/docs/specs/otlp/#json-protobuf-encoding).
Only metadata is expected to reach Driga's collector. Hook execution,
registration, settings, and plugin events have no existing state meaning and
are acknowledged as ignored.
