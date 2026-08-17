package llm

import (
	"embed"
	"strings"
)

// System prompts live as editable Markdown next to the code and are seeded
// into llm_prompts as version 1 on first boot. Lab Admins add new versions
// from the Admin panel; every call records the version used, so graded work
// stays reproducible for accreditation review.
//
//go:embed prompts/*.md
var promptFS embed.FS

var DefaultPrompts = map[string]string{}

func init() {
	for _, module := range Modules {
		body, err := promptFS.ReadFile("prompts/" + module + ".md")
		if err != nil {
			continue
		}
		DefaultPrompts[module] = strings.TrimSpace(string(body))
	}
}
