-- +goose Up
ALTER TABLE sessions ADD COLUMN conversation_claimed INTEGER NOT NULL DEFAULT 0;
-- Preserve recoverable evidence from older collectors. An idle registry row
-- alone cannot distinguish a spare from a conversation.
-- Stored turn-hook kinds prove a claim, unlike spare lifecycle notifications.
-- Panel evidence can outlive the capped raw event tail.
UPDATE sessions SET conversation_claimed=1
WHERE adapter='claude' AND (registry_status='busy' OR source_path<>'' OR (
    external_id<>'' AND external_id NOT LIKE 'pid:%' AND (EXISTS (
        SELECT 1 FROM events e
        WHERE e.machine_id=sessions.machine_id AND e.adapter=sessions.adapter
          AND e.external_id=sessions.external_id
          AND e.kind IN ('UserPromptSubmit','Stop','PreToolUse','PostToolUse',
                         'PostToolUseFailure','PermissionRequest')
    ) OR EXISTS (
        SELECT 1 FROM panel_events e
        WHERE e.machine_id=sessions.machine_id AND e.adapter=sessions.adapter
          AND e.external_id=sessions.external_id
          AND e.kind IN ('UserPromptSubmit','Stop','PreToolUse','PostToolUse',
                         'PostToolUseFailure','PermissionRequest')
    )
    )
));

-- +goose Down
ALTER TABLE sessions DROP COLUMN conversation_claimed;
