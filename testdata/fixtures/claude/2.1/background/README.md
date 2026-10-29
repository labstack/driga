These fixtures reproduce the Claude Code 2.1.31x command shapes supplied in the
helper-process bug brief. User paths are scrubbed to `/Users/fixture`; socket
names, the second spare tree, parent relationships, version patch number,
timestamps, resource measurements, and conversation contents are synthetic.
They are reproductions of the supplied shapes, not a full captured process dump.

`registry.json` represents the ambiguous `kind: "bg", status: "idle"` case:
without a transcript or previously observed busy conversation it is a spare.
Tests also change its status to busy or add `transcript.jsonl` to prove a claim.
All registry and transcript reads in these tests use temporary home directories.
