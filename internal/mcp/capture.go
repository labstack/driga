package mcp

import "github.com/labstack/driga/internal/scrub"

// captureEvidence is a final boundary for Chat's MCP responses. Reapply policy
// even to old rows retained before a capture-policy downgrade. Never change IDs,
// measurements or paths; opaque prose and command lines require snippets.
func captureEvidence(value any, policy string) any {
	switch v := value.(type) {
	case map[string]any:
		if policy != "snippets" && v["title_from_prompt"] == true {
			v["title"] = ""
		}
		for key, item := range v {
			switch key {
			case "last_prompt", "summary", "text", "detail", "cmdline", "latest_handoff", "prompt", "last_message":
				if text, ok := item.(string); ok {
					if policy == "snippets" {
						v[key] = scrub.Snippet(text)
					} else {
						v[key] = ""
					}
					continue
				}
			case "argv":
				if policy != "snippets" {
					v[key] = []any{}
					continue
				}
				if args, ok := item.([]any); ok {
					for i, arg := range args {
						if text, ok := arg.(string); ok {
							args[i] = scrub.Snippet(text)
						}
					}
					continue
				}
			case "handoffs":
				if policy != "snippets" {
					v[key] = []any{}
					continue
				}
			}
			v[key] = captureEvidence(item, policy)
		}
		return v
	case []any:
		for i := range v {
			v[i] = captureEvidence(v[i], policy)
		}
		return v
	case string:
		return scrub.Scrub(v)
	default:
		return value
	}
}
