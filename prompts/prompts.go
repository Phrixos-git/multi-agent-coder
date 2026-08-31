// Package prompts provides the embedded agent system prompts.
package prompts

import "embed"

// FS contains the role-specific system prompts.
//
//go:embed *.md
var FS embed.FS
