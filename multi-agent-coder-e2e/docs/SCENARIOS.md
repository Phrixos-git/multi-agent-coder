# E2E scenarios

## Scope

The repository E2E checks separate three kinds of checks:

1. Live Codex-to-MCP acceptance calls verify that Codex invokes
   `multi-agent-coder` and does not silently replace a failed MCP call.
2. Deterministic fixture scripts verify source/test outcomes and the MCP server's
   response classification with fixed candidates and mock HTTP responses.
3. A live Go integration test verifies Main LLM, Coding LLM, and Pi role routing
   against the locally configured services.

The local agents never receive shell or Git authority in direct mode. Codex
applies accepted artifacts and executes tests. Pi coder mode remains isolated in
its temporary worktree and returns a patch for Codex to inspect.

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
LLM, Coding LLM, and Pi routing. It reads endpoint, model, and Pi provider
settings from the same `MAC_*` environment variables as the MCP server. If
`MAC_LLM_MODEL` is empty, it uses the first model ID returned by Main LLM's
`/v1/models`. `MAC_PI_PROVIDER` and `MAC_PI_MODEL` must match an entry in
Pi's `models.json`; the test does not assume provider or model names.

```text
MAC_LIVE_ROLE_E2E=1 go test -v ./internal/mcp -run '^TestLiveRoleRouting$' -count=1 -timeout=9m
```

The expected routes are reviewer/tester → Main LLM, fixer → Coding LLM, and
coder → Pi. The test disables the other direct endpoint for each role so that
success demonstrates the intended backend route. Run the command from the
repository root.

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
