// Package prompts provides the embedded agent system prompts.
package prompts

import "embed"

// FS contains the role-specific system prompts. The top-level files are used
// when Codex orchestrates; claude-code/ holds the Claude Code variants.
//
//go:embed *.md claude-code/*.md
var FS embed.FS
