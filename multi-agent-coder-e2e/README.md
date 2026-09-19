# multi-agent-coder E2E fixture

This small Go module is the repository fixture used to exercise
`multi-agent-coder`. The Calculator API is intentionally small so agent routing,
response validation, error classification, and logs remain the focus.

Run all deterministic/recorded checks:

```text
scripts/run-all
```

Every scenario is also individually executable from `scripts/`. See
`docs/SCENARIOS.md` for live Codex-to-MCP acceptance steps, expected routing,
error classification, and logging/privacy details. A scenario exits non-zero
and prints `expected`, `actual`, `failed stage`, and `error type` on failure.
