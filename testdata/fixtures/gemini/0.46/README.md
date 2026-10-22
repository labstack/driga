All fixtures are synthetic. No installed agent or user history was accessed.
The directory names the plan's tested format family, not a real captured run.

Verified against public google-gemini/gemini-cli source on 2026-10-18:

- [chatRecordingService.ts](https://github.com/google-gemini/gemini-cli/blob/main/packages/core/src/services/chatRecordingService.ts): metadata, messages, replacement messages, inclusive rewind, tool result patches, recovery rename.
- [chatRecordingTypes.ts](https://github.com/google-gemini/gemini-cli/blob/main/packages/core/src/services/chatRecordingTypes.ts): tool calls use `args` and optional `result`.
- [paths.ts](https://github.com/google-gemini/gemini-cli/blob/main/packages/core/src/utils/paths.ts) and [storage.ts](https://github.com/google-gemini/gemini-cli/blob/main/packages/core/src/config/storage.ts): `GEMINI_CLI_HOME` replaces the home directory; append `.gemini`.
- [projectRegistry.ts](https://github.com/google-gemini/gemini-cli/blob/main/packages/core/src/config/projectRegistry.ts): `.project_root` holds the project path.
- [write-file.ts](https://github.com/google-gemini/gemini-cli/blob/main/packages/core/src/tools/write-file.ts), [edit.ts](https://github.com/google-gemini/gemini-cli/blob/main/packages/core/src/tools/edit.ts) and [base-declarations.ts](https://github.com/google-gemini/gemini-cli/blob/main/packages/core/src/tools/definitions/base-declarations.ts): current tool names are `write_file` and `replace`, both using `file_path`. `absolute_path` and the `edit` name are accepted as compatibility spellings required by the plan; they were not confirmed as current registry spellings.

Ruling: follow verified home-override semantics instead of interpreting the override as the `.gemini` directory itself. Rewind removes the target message as well as all later messages. Reads retain at most 1,024 compact messages per file and 32 cached files; larger backlogs use the existing 8 MiB tail budget and warn about partial coverage.
