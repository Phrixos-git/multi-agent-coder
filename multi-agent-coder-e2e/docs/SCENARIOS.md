# E2E scenarios

## Scope

The repository E2E checks separate four kinds of checks:

1. Live Codex-to-MCP acceptance calls verify that Codex invokes
   `multi-agent-coder` and does not silently replace a failed MCP call.
2. Deterministic fixture scripts verify source/test outcomes and the MCP server's
   response classification with fixed candidates and mock HTTP responses.
3. A live Go integration test verifies Main LLM and Coding LLM role routing
   against the locally configured services.
4. An optional live implementation challenge verifies Pi-generated interval
   normalization code and repairs against executable acceptance tests.

Direct agents return JSON proposals and have no shell or Git authority.
Pi coder/fixer agents may edit files and run tests inside temporary worktrees;
MAC manages Git and collects patches. Reviewer/tester always use the Main LLM
Direct route. Codex controls application to the main working tree.

## Live implementation challenge

From the repository root, with the usual `MAC_*` variables exported and both
`MAC_CODER_BACKEND=pi` and `MAC_FIXER_BACKEND=pi`, run:

```text
MAC_LIVE_IMPLEMENTATION_E2E=1 go test -v ./internal/mcp -run '^TestLiveImplementationE2E$' -count=1 -timeout=15m
```

This optional test uses the current Go source and in-memory MCP transport. It
does not register or start the installed STDIO binary. It replaces
`MAC_REPOSITORY_ROOT` with a disposable Git repository containing only a small
interval-normalization fixture. It never applies agent changes to this checkout.

The fixture contract and tests are supplied before implementation. Coder
implements `Normalize` from a stub. In a separate repository, fixer receives a
seeded implementation through a dirty `intervals.go` overlay and repairs its
missing adjacent-interval merge. The baseline tests must fail before each call;
after patch application every contract test must pass. Only `intervals.go` may
change; agents cannot pass by weakening the acceptance tests.

Acceptance covers empty input, singleton and unordered intervals, overlaps,
adjacency, nesting, duplicates, negative endpoints, invalid reversed intervals,
input immutability on success and failure, and minimum/maximum machine integers.
A finite exhaustive test compares all pairs of small intervals with a set-based
oracle, and checks canonical ordering and idempotence. The implementation uses
only the Go standard library and must not enumerate every integer in a range.

The test logs whether each returned patch has a final LF. Until the existing
patch-LF defect is fixed, it adds one LF before `git apply --check` and applying
the patch. Success proves implementation behavior after this normalization;
it does not prove that the raw returned patch is directly applicable.

## Commands

Run one scenario from this directory:

```text
scripts/e2e-01-normal
scripts/e2e-02-review
scripts/e2e-03-fixer
scripts/e2e-04-tester
scripts/e2e-05-token-limit
scripts/e2e-06-timeout
scripts/e2e-07-mcp-unavailable
scripts/e2e-08-invalid-json
```

Run all recorded/deterministic checks:

```text
scripts/run-all
```

Run E2E-07 against a new ephemeral Codex process instead of the checked-in
acceptance trace:

```text
MAC_E2E_LIVE_CODEX=1 scripts/e2e-07-mcp-unavailable
```

The live mode deliberately passes `--ignore-user-config`, making
`multi-agent-coder` unavailable. It is read-only and must emit an explicit
`mcp_error` without a tool, shell, or sub-agent event.

## Live role-routing integration test

The separate Go test `internal/mcp/TestLiveRoleRouting` exercises live Main
LLM and Coding LLM routing. It reads endpoint and model settings from the same
`MAC_*` environment variables as the MCP server.

```text
MAC_LIVE_ROLE_E2E=1 go test -v ./internal/mcp -run '^TestLiveRoleRouting$' -count=1 -timeout=9m
```

The expected routes are coder/fixer → Coding LLM and reviewer/tester → Main
LLM. The test disables the other endpoint for each role so success demonstrates
the intended route. Run the command from the repository root.

## Expected routing

| Scenario | Expected route | Terminal classification |
| --- | --- | --- |
| E2E-01 | coder → reviewer → tester | success; fixer skipped |
| E2E-02 | coder → reviewer | review_failure; fixer skipped |
| E2E-03 | coder → reviewer → fixer → tester | success |
| E2E-04 | tester proposal → Codex `go test` | test_failure |
| E2E-05 | coder | token_limit |
| E2E-06 | coder | timeout |
| E2E-07 | Codex → unavailable MCP → stop | mcp_error |
| E2E-08 | coder | invalid_json |

E2E-05 proves that an HTTP-success response with `finish_reason=length` is
classified before agent JSON decoding. E2E-08 covers both malformed HTTP JSON
and valid HTTP JSON whose assistant content is malformed. E2E-06 uses a finite
timeout and bounded retry; its trace requires every later role to be skipped.

## Logs and privacy

Scenario traces expose `request_id`, `task_id`, `agent`, `backend`, `model`,
timestamps/duration, `max_tokens`, `finish_reason`, `result`, and `error_type`
where applicable. Production MCP logs stay on stderr. Prompts, secrets, and full
source contents are not written to logs by default.

The checked-in traces contain only fixed synthetic values or sanitized Codex
events. A scenario failure prints its expected result, actual output, failed
stage, and error type, then exits non-zero.

## Claude Code acceptance

These manual steps check the same acceptance points with Claude Code as the
orchestrator. They use `claude -p` (non-interactive print mode) and do not
change the Codex scenarios or scripts above. Run them from this directory.

The launcher argument selects the orchestrator; the shared `.env.<profile>`
file is reused unchanged:

```text
/absolute/path/to/multi-agent-coder/scripts/run-mcp-server qwen claude-code
```

For a reproducible run, put the server in a dedicated MCP config file (for
example `/tmp/mac-claude-mcp.json`) and pass it with `--strict-mcp-config` so
that user and project MCP settings are ignored:

```json
{
  "mcpServers": {
    "multi-agent-coder": {
      "command": "/absolute/path/to/multi-agent-coder/scripts/run-mcp-server",
      "args": ["qwen", "claude-code"]
    }
  }
}
```

### CC-HEALTH: orchestrator identification

```text
claude -p --strict-mcp-config --mcp-config /tmp/mac-claude-mcp.json \
  --allowedTools mcp__multi-agent-coder__local_agent_health \
  --output-format stream-json --verbose \
  'Call mcp__multi-agent-coder__local_agent_health once and print its JSON result verbatim.'
```

Pass: the tool result contains `"orchestrator":"claude-code"`. Repeating the
call through `scripts/run-mcp-server qwen` (no second argument) must report
`"orchestrator":"codex"`, including when `.env.qwen` contains
`MAC_ORCHESTRATOR=codex` and the launcher argument is `claude-code`.

### CC-01: normal delegation

Run the E2E-01 task (coder → reviewer → tester) with the
`multi-agent-coder-delegation` skill from
`.claude/skills/multi-agent-coder-delegation/SKILL.md`, allowing the
`mcp__multi-agent-coder__local_agent_run` tool. Claude Code applies the
accepted Direct artifacts itself and runs `go test ./...` itself.

Pass: the stream contains `local_agent_run` calls with roles `coder`,
`reviewer`, and `tester` in that order, `go test ./...` succeeds after the
changes, and no result whose `warnings` contain `OutputTruncated` is applied.

### CC-07: stop when MCP is unavailable

Equivalent of E2E-07. `--strict-mcp-config` without `--mcp-config` starts
Claude Code with no MCP servers, so `multi-agent-coder` is unavailable.
`--tools ""` removes all built-in tools so the run is read-only and cannot
fall back to shell, file edits, or sub-agents.

```text
claude -p --strict-mcp-config --tools "" --no-session-persistence \
  --output-format stream-json --verbose \
  'This is E2E-07. You must use the multi-agent-coder MCP for any work. Ask it only to report coder availability. If that MCP is unavailable, stop immediately, state multi-agent-coder unavailable and mcp_error, and do not use built-in sub-agents, other MCPs, shell, or edit files.' \
  > /tmp/mac-claude-e2e-07.jsonl
```

Pass criteria:

1. The final `result` event text contains `multi-agent-coder unavailable`
   and `mcp_error`:

   ```text
   grep '"type":"result"' /tmp/mac-claude-e2e-07.jsonl | grep -q 'multi-agent-coder unavailable.*mcp_error'
   ```

2. No tool was invoked. The stream must not contain a `tool_use` block:

   ```text
   ! grep -q '"type":"tool_use"' /tmp/mac-claude-e2e-07.jsonl
   ```

3. The `system` `init` event lists no `multi-agent-coder` entry in
   `mcp_servers`.

On failure, record the expected result, the actual stream, the failed stage
(`MCP connect` or `fallback guard`), and the error type `mcp_error`, as the
Codex script does.

### CC-ISERROR and CC-TRUNCATE: Claude Code response handling

With the llama-server stopped, a `local_agent_run` call must return a result
with `isError: true` while its structured content still has `status: error`
and the error `kind`. With `MAC_CLAUDE_TOOL_ERROR_FLAG=false`, the same call
returns `isError` unset. Truncation is covered deterministically by the Go
tests in `internal/mcp/orchestrator_test.go`; for a manual check, set a small
`MAC_CLAUDE_MAX_OUTPUT_CHARS` (for example `2000`) and confirm that a coder
result contains `OutputTruncated` warnings and that Claude Code, following the
skill, splits the task instead of applying the result.
