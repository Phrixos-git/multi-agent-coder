package mcp

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/arietis/multi-agent-coder/internal/config"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const outputTruncatedSplitHint = "OutputTruncated: truncated patch or artifact content is incomplete and must not be applied; split the task into smaller requests to obtain the full result"

// orchestratorName reports the MCP client, treating an unset value as Codex.
func orchestratorName(cfg config.Config) string {
	if cfg.Orchestrator == "" {
		return config.OrchestratorCodex
	}
	return cfg.Orchestrator
}

// claudeCodeResult applies the Claude Code-only response post-processing.
// It is never called for Codex, whose responses stay unchanged.
//
// A non-nil CallToolResult with nil Content makes the SDK fill both
// StructuredContent and the text Content from the returned RunOutput, so the
// payload matches the nil-result path and only isError differs.
func claudeCodeResult(cfg config.Config, output RunOutput) (*sdkmcp.CallToolResult, RunOutput) {
	if cfg.ClaudeMaxOutputChars > 0 {
		output = limitOutputChars(output, cfg.ClaudeMaxOutputChars)
	}
	if cfg.ClaudeToolErrorFlag && output.Status == "error" {
		return &sdkmcp.CallToolResult{IsError: true}, output
	}
	return nil, output
}

// truncatableField is one string that may be shortened to fit the limit.
type truncatableField struct {
	name     string
	artifact int // index into artifacts, or -1 for patch
	original string
	runes    int // rune count of original
	kept     int // runes kept from the start of original
}

// limitOutputChars shortens patch and artifacts[].content from their ends so
// that the JSON-encoded output has at most limit characters. Other fields are
// never shortened. Each shortened field is reported in warnings.
//
// Every truncatable field is capped at a common number of runes; the largest
// cap whose encoded output (including the added warnings) fits is chosen by
// binary search, so small fields stay intact and large ones shrink evenly.
func limitOutputChars(output RunOutput, limit int) RunOutput {
	size, err := encodedChars(output)
	if err != nil || size <= limit {
		return output
	}

	fields := make([]*truncatableField, 0, len(output.Artifacts)+1)
	longest := 0
	if output.Patch != "" {
		fields = append(fields, newTruncatableField("patch", -1, output.Patch))
	}
	for i, artifact := range output.Artifacts {
		if artifact.Content != "" {
			fields = append(fields, newTruncatableField(fmt.Sprintf("artifacts[%d].content", i), i, artifact.Content))
		}
	}
	for _, field := range fields {
		longest = max(longest, field.runes)
	}

	withCap := func(capRunes int) (RunOutput, int, error) {
		for _, field := range fields {
			field.kept = min(field.runes, capRunes)
		}
		candidate := applyTruncation(output, fields)
		size, err := encodedChars(candidate)
		return candidate, size, err
	}

	candidate, size, err := withCap(0)
	if err != nil {
		return output
	}
	if size > limit {
		candidate.Warnings = append(candidate.Warnings, fmt.Sprintf(
			"OutputTruncated: response is %d chars and still exceeds MAC_CLAUDE_MAX_OUTPUT_CHARS=%d because non-truncatable fields are too large", size, limit))
		return candidate
	}
	// Invariant: cap low fits, cap high (the untruncated output) does not.
	low, high := 0, longest
	for high-low > 1 {
		middle := low + (high-low)/2
		if _, size, err := withCap(middle); err == nil && size <= limit {
			low = middle
		} else {
			high = middle
		}
	}
	candidate, _, _ = withCap(low)
	return candidate
}

func newTruncatableField(name string, artifact int, value string) *truncatableField {
	n := utf8.RuneCountInString(value)
	return &truncatableField{name: name, artifact: artifact, original: value, runes: n, kept: n}
}

// applyTruncation returns a copy of output with fields shortened to their
// kept lengths and one warning per shortened field.
func applyTruncation(output RunOutput, fields []*truncatableField) RunOutput {
	result := output
	// Copy without turning an empty, non-nil slice into nil: artifacts has no
	// omitempty and must stay [] rather than null.
	if output.Artifacts != nil {
		result.Artifacts = append([]Artifact{}, output.Artifacts...)
	}
	if output.Warnings != nil {
		result.Warnings = append([]string{}, output.Warnings...)
	}
	truncated := false
	for _, field := range fields {
		if field.kept >= field.runes {
			continue
		}
		truncated = true
		value := truncateRunes(field.original, field.kept)
		if field.artifact < 0 {
			result.Patch = value
		} else {
			result.Artifacts[field.artifact].Content = value
		}
		result.Warnings = append(result.Warnings, fmt.Sprintf("OutputTruncated: %s truncated from %d to %d chars", field.name, field.runes, field.kept))
	}
	if truncated {
		result.Warnings = append(result.Warnings, outputTruncatedSplitHint)
	}
	return result
}

func truncateRunes(value string, keep int) string {
	if keep <= 0 {
		return ""
	}
	count := 0
	for index := range value {
		if count == keep {
			return value[:index]
		}
		count++
	}
	return value
}

// encodedChars measures output as the SDK serializes it into text Content.
func encodedChars(output RunOutput) (int, error) {
	encoded, err := json.Marshal(output)
	if err != nil {
		return 0, err
	}
	return utf8.RuneCount(encoded), nil
}
