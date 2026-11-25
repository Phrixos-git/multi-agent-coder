---
name: multi-agent-coder-delegation
description: Use when the user explicitly asks Claude Code to delegate coding, review, testing, or fixing work to the multi-agent-coder MCP server or local LLM. Break large coding requests into small sequential tasks suitable for a slower local LLM before calling multi-agent-coder.
---

# multi-agent-coder delegation

Before calling `multi-agent-coder`, inspect the repository and decompose
the user's request.

## Goal

Keep every local LLM request small, focused, and independently completable.

## Responsibilities

- Direct Backend artifacts are proposals. Claude Code validates and applies
  them, and Claude Code runs the tests.
- Pi Backend patches come from a temporary worktree. Claude Code reviews the
  patch content before applying it to the main working tree.

## Workflow

1. Understand the complete user request.

2. Inspect the relevant repository files yourself.

3. Create an internal ordered task list.

4. Each task should have:
   - one primary objective
   - only directly relevant files
   - minimal requirements
   - a bounded expected output

5. Send only the first task to `mcp__multi-agent-coder__local_agent_run`
   with role `coder`.

6. Validate the returned result yourself.

   If `status` is `needs_input` and `workspace_input` is present, ask the user
   which repository to use. Offer candidates only when the client has reliable
   workspace information. Retry the same task with the user-selected absolute
   directory in `repository_directory`. Use Git-root-relative paths in `files`.

   If `warnings` contains `OutputTruncated`, the patch or artifact content
   is incomplete. Do not apply that result. Split the task into smaller
   tasks and send them again.

7. Apply the accepted change.

8. Continue with the next task.

9. Do not send all remaining tasks together.

10. After implementation tasks are complete, request tests separately
    when substantial test generation is required.

11. Send only the resulting diff and directly relevant context to
    `reviewer`.

12. If review findings exist, group closely related findings and send
    only those findings to `fixer`.

## Task sizing

Prefer:

- one function or one closely related set of functions
- one component
- one interface change
- one logical behavior
- one small test group

Avoid:

- complete feature implementation across multiple layers
- implementation + tests + documentation in one request
- unrelated file changes
- repository-wide review
- speculative refactoring

If a task appears likely to generate a large response,
split it before calling the MCP server.

Do not solve local-LLM latency by increasing the task size,
timeout, or output limit unless splitting is impractical.
