# multi-agent-coder 設計書

## 1. 概要

`multi-agent-coder` は、Codexを上位Orchestratorとして利用し、RoleごとにローカルLLM Backendを選択するMCP Serverである。

タスクごとにSystem Promptと会話コンテキストを分離することで、以下の役割を持たせる。

- Coder
- Reviewer
- Tester
- Fixer
- 将来的にはRefactorer、Documenterなどを追加可能

Codexは要求分析、タスク分割、成果物の採否、実際のファイル編集、テスト実行を担当する。

ローカルLLMはコード生成・レビュー・テスト設計などの補助作業を担当する。

---

# 2. 目的

主目的は、Codexのトークン消費を抑えながら、ローカルLLMへコーディング作業を委譲できる環境を構築することである。

基本方針は以下とする。

```text
Codex
  = Architect / Tech Lead / Orchestrator

Local LLM
  = Programmer / Reviewer / Tester
```

Codexですべてのコードを生成するのではなく、

```text
設計
タスク分割
最終レビュー
統合判断
```

をCodexが担当し、

```text
コード実装
局所的なコードレビュー
テストケース提案
修正案作成
```

をローカルLLMへ委譲する。

---

# 3. 非目標

以下は実装対象外とする。

- 複数GPUへの分散
- ローカルLLMによる直接的なGit操作
- Direct Backendによる任意Shellコマンド実行
- Direct Backendによる直接的なファイル変更
- 自律的な無制限再試行
- 複数Agentによる同一ファイルの並列編集
- 独自ベクトルDB
- RAG
- Web検索

Pi Coder/Fixer Backendに限り、一時Git worktree内でのファイル編集とbash実行を許可する。OS SandboxではなくRepository編集の隔離であり、Piはサーバー実行ユーザーのOS権限を持つ。

Role別Backend routingはMain LLMとCoding LLMの2つの手動起動済みserverを利用する。自動起動・停止や複数Coding Modelからの自動選択は行わない。

---

# 4. 全体アーキテクチャ

```text
┌──────────────────────────┐
│          User            │
└─────────────┬────────────┘
              │
              ▼
┌──────────────────────────┐
│        Codex CLI         │
│                          │
│ Orchestrator             │
│ ・要求分析               │
│ ・タスク分割             │
│ ・MCP呼び出し            │
│ ・ファイル変更           │
│ ・テスト実行             │
│ ・最終レビュー           │
└─────────────┬────────────┘
              │ MCP / STDIO
              ▼
┌──────────────────────────┐
│    multi-agent-coder     │
│        MCP Server        │
│                          │
│ ・Role選択               │
│ ・Prompt生成             │
│ ・Context生成            │
│ ・Role別Backend routing  │
│ ・LLM / Pi RPC管理       │
│ ・Worktreeと差分回収     │
└─────────────┬────────────┘
              │ Role別Backend selection
              ▼
┌──────────────────────────┐
│ coder: Direct or Pi       │
│ fixer: Direct or Pi       │
│ reviewer/tester: Direct   │
│              -> Main LLM  │
└──────────────────────────┘
```

---

# 5. Agent構成

## 5.1 Coder

役割：

- 指定された機能の実装
- 既存コードに合わせた実装
- 最小限の変更
- 必要なテストコード作成

Coderは設計変更を勝手に行わない。

要求が不明確な場合は推測で大規模変更を行わず、問題点をレスポンスへ含める。

---

## 5.2 Reviewer

Coderとは独立したコンテキストで起動する。

役割：

- 実装内容レビュー
- バグ検出
- API契約違反検出
- エラーハンドリング確認
- セキュリティ上の問題確認
- 不必要な変更検出

重要：

ReviewerにはCoderとの会話履歴を渡さない。

渡すものは、

```text
Task Specification
+
Original Code
+
Candidate Code / Diff
```

のみとする。

これによりCoder自身の判断にReviewerが引きずられることを防ぐ。

---

# 5.3 Tester

役割：

- 必要なテストケース抽出
- 境界値検討
- Failure case検討
- Regression test検討
- テストコード生成

Tester自身にはテストコマンドを実行させない。

テスト実行はCodexが担当する。

---

# 5.4 Fixer

Reviewerまたはテスト結果で問題が検出された場合に利用する。

入力：

```text
Task
Current Code
Review Findings
Test Failure
```

出力：

```text
修正案
修正コード
変更理由
```

---

# 6. 1モデルを複数Agentとして利用する方法

Agentごとに独立したSystem Promptを使用する。

例：

```text
prompts/
├── coder.md
├── reviewer.md
├── tester.md
└── fixer.md
```

同じQwenを使用する場合でも、

```text
Coder request

System:
You are a software implementation agent.
```

と

```text
Reviewer request

System:
You are a strict software code reviewer.
```

を別リクエストとして送信する。

会話履歴も共有しない。

したがって、

```text
Qwen Process
    │
    ├── Coder Context
    ├── Reviewer Context
    ├── Tester Context
    └── Fixer Context
```

という論理構造になる。

---

# 7. 基本実行フロー

```text
User
 │
 ▼
Codex
 │
 ├─ 要求分析
 ├─ Repository調査
 └─ Task Specification生成
          │
          ▼
  local_agent_run
     role=coder
          │
          ▼
        Qwen
          │
          ▼
    Candidate Code
          │
          ▼
        Codex
          │
     ファイルへ適用
          │
          ▼
  local_agent_run
    role=reviewer
          │
          ▼
        Qwen
          │
          ▼
      Review Result
          │
     ┌────┴─────┐
     │          │
    OK        NG
     │          │
     │          ▼
     │       Fixer
     │          │
     └────┬─────┘
          ▼
       Codex
          │
      Test実行
          │
     ┌────┴─────┐
     │          │
   PASS        FAIL
     │          │
     │          ▼
     │       Fixer
     │
     ▼
最終Codex Review
     │
     ▼
   Complete
```

`MAC_CODER_BACKEND`と`MAC_FIXER_BACKEND`は独立して`direct`または`pi`を選べる。Piの設定はCoder/Fixerで共有する。Piを選んだ呼び出しは一時worktree上の新規RPC Sessionで実行し、Reviewer/Testerは常にDirectを使う。

---

# 8. MCP Server

MCP Server名：

```text
multi-agent-coder
```

通信方式：

```text
STDIO
```

Version 1ではHTTP MCP Serverは使用しない。

理由：

- localhostポートを増やす必要がない
- Codexがプロセス起動を管理できる
- 認証が不要
- ローカル専用ツールとして扱いやすい

---

# 9. MCP Tool

Version 1ではツールを増やしすぎず、基本的に2つだけ提供する。

## 9.1 local_agent_run

主ツール。

入力例：

```json
{
  "role": "coder",
  "task": "UserRepositoryにFindByIDを追加する",
  "requirements": [
    "既存インターフェースを壊さない",
    "エラーをwrapする"
  ],
  "files": [
    "internal/repository/user.go",
    "internal/model/user.go"
  ],
  "additional_context": ""
}
```

role：

```text
coder
reviewer
tester
fixer
```

---

## 9.2 local_agent_health

動作確認用。

確認対象はMain LLMとCoding LLMとする。各LLMへ有限timeoutで個別に接続し、接続拒否による停止とHTTP/応答の失敗を区別する。いずれかのLLMが停止していてもMCP Serverは動作を続ける。

レスポンス例：

```json
{
  "status": "ok",
  "llm": "available",
  "main_llm": {"status": "available", "base_url": "http://127.0.0.1:8089", "model": "qwen3.8-27b"},
  "coding_llm": {"status": "available", "base_url": "http://127.0.0.1:8089", "model": "qwen-coder"}
}
```

---

# 10. local_agent_run 出力

可能な限り構造化する。

```json
{
  "status": "success",
  "role": "coder",
  "summary": "FindByIDを追加しました",
  "artifacts": [
    {
      "path": "internal/repository/user.go",
      "content": "..."
    }
  ],
  "findings": [],
  "risks": [],
  "notes": []
}
```

Reviewerの場合：

```json
{
  "status": "success",
  "role": "reviewer",
  "summary": "1件の問題を検出",
  "artifacts": [],
  "findings": [
    {
      "severity": "high",
      "description": "sql.ErrNoRowsの処理が不足しています"
    }
  ],
  "risks": [],
  "notes": []
}
```

---

# 11. ファイルアクセス

Direct Backendに自由なファイルアクセスを与えない。Pi Coder/FixerではMACが検証したstarting file pathsを渡し、Piはtemporary worktree内のファイルを必要に応じて読む。Fixerは`files[]`対象だけMain Working Treeからoverlayし、overlay直後をBaselineとしてBaseline→FinalのPatchだけ返す。

Codexから、

```text
files:
- internal/foo.go
- internal/bar.go
```

のように必要ファイルを指定する。

MCP Server側では指定パスを検証する。

必須条件：

```text
filepath.Clean()
filepath.Abs()
filepath.Rel()
```

等を利用し、Repository Root外へのアクセスを拒否する。

禁止例：

```text
../../.ssh/id_ed25519
/etc/passwd
/home/user/.config/...
```

---

# 12. ファイル変更ポリシー

Direct BackendではローカルLLM自身はファイルを書き換えない。

```text
Qwen
 ↓
Candidate Code
 ↓
MCP Response
 ↓
Codex
 ↓
File Update
```

とする。

理由：

- 競合防止
- 誤編集防止
- Repository全体破壊防止
- 変更履歴をCodex側で追跡可能
- Sandboxを単純化できる

---

# 13. Shell実行

Direct BackendにはShell権限を与えない。Pi Coder/Fixer Backendのみ一時worktree内でbashを許可し、Git履歴操作・branch切替・worktree管理は禁止する。

例えば、

```text
go test ./...
npm test
cargo test
```

などはCodex自身が実行する。Pi Coder/Fixer Backendでは指定した一時worktree内でPiがtargeted testと広域テストを実行する。

ローカルLLMは、

```text
このテストを実行してください
```

と提案するだけにする。

---

# 14. llama.cpp Client

アクセス先は設定可能にする。

Default：

```text
http://127.0.0.1:8089
```

Chat Completion：

```text
POST /v1/chat/completions
```

構造：

```text
internal/llm/
├── client.go
├── request.go
├── response.go
└── errors.go
```

---

# 15. Timeout

LLMリクエストには必ずTimeoutを設定する。

Role別Default：

```text
Coder/Fixer/Coding LLM: 180 seconds
Reviewer/Tester/Main LLM: 180 seconds
```

Roleを指定しない低レベル呼び出しでは`MAC_LLM_TIMEOUT`を使用する。

---

# 16. Retry

無制限Retryは禁止する。

Version 1：

```text
Max Retry = 1
```

Retry対象：

```text
Connection reset
HTTP 502
HTTP 503
Timeout
```

Retry対象外：

```text
400
401
404
Invalid JSON
Context length exceeded
```

---

# 17. Agent Loop上限

Fixerの無限ループを防止する。

Default：

```text
Coder
 ↓
Reviewer
 ↓
Fixer
 ↓
Reviewer
```

まで。

最大Fix回数：

```text
2
```

それ以上失敗した場合はCodexへ制御を戻す。

---

# 18. Prompt構造

各Promptは以下で構成する。

```text
SYSTEM ROLE

RULES

TASK

REQUIREMENTS

FILES

ADDITIONAL CONTEXT

OUTPUT FORMAT
```

---

# 19. Coder Prompt方針

Coderには以下を要求する。

```text
Do not redesign unrelated components.

Do not modify files outside the requested scope.

Prefer minimal changes.

Preserve existing public APIs unless explicitly requested.

Do not invent missing requirements.

Return complete replacement code only for files that require changes.
```

---

# 20. Reviewer Prompt方針

Reviewer：

```text
Do not assume the implementation is correct.

Inspect independently.

Prioritize:
1. correctness
2. security
3. API compatibility
4. error handling
5. maintainability

Do not rewrite working code merely for style preference.
```

---

# 21. Tester Prompt方針

Tester：

```text
Identify tests required to validate the requested behavior.

Prioritize:
- happy path
- error path
- boundary conditions
- regression
- externally visible behavior

Avoid meaningless coverage-only tests.
```

---

# 22. Repository構成

```text
multi-agent-coder/
├── AGENTS.md
├── README.md
├── go.mod
├── go.sum
│
├── cmd/
│   └── multi-agent-coder/
│       └── main.go
│
├── internal/
│   ├── agent/
│   │   ├── role.go
│   │   ├── runner.go
│   │   └── prompt.go
│   │
│   ├── config/
│   │   └── config.go
│   │
│   ├── llm/
│   │   ├── client.go
│   │   ├── request.go
│   │   ├── response.go
│   │   └── errors.go
│   │
│   ├── mcp/
│   │   ├── server.go
│   │   ├── health.go
│   │   └── local_agent.go
│   │
│   └── workspace/
│       ├── reader.go
│       └── validator.go
│
├── prompts/
│   ├── coder.md
│   ├── reviewer.md
│   ├── tester.md
│   └── fixer.md
│
├── docs/
│   └── DESIGN.md
│
└── tests/
```

---

# 23. Configuration

環境変数を基本とする。

```text
MAC_LLM_BASE_URL
MAC_LLM_MODEL
MAC_LLM_TIMEOUT
MAC_CODING_LLM_BASE_URL
MAC_CODING_LLM_MODEL
MAC_CODING_LLM_TIMEOUT
MAC_LLM_MAX_TOKENS
MAC_CODER_MAX_TOKENS
MAC_REVIEWER_MAX_TOKENS
MAC_TESTER_MAX_TOKENS
MAC_FIXER_MAX_TOKENS
MAC_LLM_REASONING_EFFORT
MAC_LLM_TEMPERATURE
MAC_REPOSITORY_ROOT
MAC_LOG_LEVEL
```

Default：

```text
MAC_LLM_BASE_URL=http://127.0.0.1:8089
MAC_LLM_MODEL=qwen3.8-27b
MAC_LLM_TIMEOUT=180s
MAC_CODING_LLM_BASE_URL=http://127.0.0.1:8089
MAC_CODING_LLM_MODEL=qwen-coder
MAC_CODING_LLM_TIMEOUT=180s
MAC_LLM_MAX_TOKENS=4096
MAC_CODER_MAX_TOKENS=4096
MAC_REVIEWER_MAX_TOKENS=4096
MAC_TESTER_MAX_TOKENS=3072
MAC_FIXER_MAX_TOKENS=4096
MAC_LLM_REASONING_EFFORT=none
MAC_LLM_TEMPERATURE=0.2
MAC_LOG_LEVEL=info
```

Agent実行時の`max_tokens`はRole別の環境変数から取得する。

```text
Coder:    MAC_CODER_MAX_TOKENS
Reviewer: MAC_REVIEWER_MAX_TOKENS
Tester:   MAC_TESTER_MAX_TOKENS
Fixer:    MAC_FIXER_MAX_TOKENS
```

Role固有の値が未指定なら`MAC_LLM_MAX_TOKENS`、それも未指定ならコード内Defaultを使用する。Roleを指定しない低レベル呼び出しでも`MAC_LLM_MAX_TOKENS`を使用する。

上記のModel名を標準Defaultとし、環境変数でllama-server側のモデルIDに合わせて変更できる。

---

# 24. Logging

STDIO MCPではstdoutをProtocol通信に使用するため、通常ログをstdoutへ出してはならない。

ログ出力先：

```text
stderr
```

必須ログ：

```text
timestamp
level
role
request_id
duration
status
```

Prompt全文およびソースコード全文はDefaultではログへ記録しない。

CodexからSTDIO Serverを起動する際は、`scripts/run-mcp-server` に `qwen` または `strata` を渡す。ラッパーはRepository Rootにある信頼済みの `.env.qwen` または `.env.strata` をBashで読み込み、設定を子プロセスへ渡す。Repository Rootの実行ファイル `multi-agent-coder` を優先し、実行できない場合は `bin/multi-agent-coder` を探す。Repository Rootの `run-mcp-server` からも同じラッパーを起動できる。

`scripts/run-mcp-server` は省略可能な第2引数で呼び出し元（`codex` または `claude-code`、既定 `codex`）を受け取る。ラッパーは `.env.<profile>` を読み込んだ後で `MAC_ORCHESTRATOR` に第2引数の値をexportする。不正な第2引数、または3個以上の引数ではUsageをstderrに出して終了コード2で終了する。詳細は「Claude Code対応」の節を参照する。

ラッパーはstderrを親プロセスへ転送し、`logs/multi-agent-coder-<profile>-YYYY-MM-DD.log` に保存する。ラッパー自体はstdoutに出力しない。ログのディレクトリは `MAC_LOG_DIR` で変更できる。

---

# 25. Request ID

各リクエストにUUIDを付与する。

```text
request_id
```

これを、

```text
Codex
MCP
LLM
Log
```

の追跡に利用する。

---

# 26. セキュリティ

以下を必須とする。

## Repository Root制限

指定されたRepository Root外を読み込まない。

## シンボリックリンク

symlink経由でRepository外へ出られないように実体パスを検証する。

## Secret

以下のようなファイルはDefaultで拒否する。

```text
.env
.env.*
*.pem
*.key
id_rsa
id_ed25519
credentials*
secrets*
```

ただし、`.env.*`の判定では、ファイル名に`.env.example`を含むもの
（例：`.env.example`、`.env.example.local`）をDefaultで許可する。
Secret判定は大文字・小文字を区別せず、各パス要素とsymlinkの実体パスにも適用する。
この例外は他の拒否パターンには適用しない。

必要な場合のみ明示許可方式にする。

## Local API

llama-serverは原則：

```text
127.0.0.1
```

へbindする。

外部LANへ公開しない。

---

# 27. Error設計

エラー分類：

```text
ConfigError
WorkspaceError
LLMConnectionError
LLMTimeoutError
LLMResponseError
LLMInvalidJSONError
LLMTruncatedError
InvalidRoleError
PromptError
```

MCP Toolではpanicせず、構造化したエラーを返す。

---

# 28. Test

## Unit Test

対象：

```text
Config
Role
Prompt generation
Path validation
Secret file filtering
LLM response parsing
```

## Integration Test

Mock HTTP Serverを利用し、

```text
MCP
 ↓
LLM Client
 ↓
Mock llama-server
```

を検証する。

## Manual E2E

実装能力の追加検証には`TestLiveImplementationE2E`を使用する。
`MAC_LIVE_IMPLEMENTATION_E2E=1`で明示的に有効化し、Coder/FixerはPiを使う。
テスト専用の一時Git Repositoryで整数区間の正規化を課題とする。
Coderには契約書・先行テスト・未実装の関数を渡し、Fixerには隣接区間を
結合しない実装を`files[]`経由でoverlayする。各呼び出しは独立した1課題とする。
返却Patchをテスト用Repositoryだけへ適用し、境界値・入力非破壊・有限の
組み合わせテストを実行する。通常の`go test ./...`では実モデルを呼ばない。
既知のPatch末尾LF欠落は明示的に記録し、この実装能力テストでは末尾LFを
補って検証する。無加工Patchの適用成功を示すテストとは区別する。

```text
Codex
 ↓
MCP
 ↓
multi-agent-coder
 ↓
coder/fixer → Direct → Coding LLM または Pi → temporary worktree
reviewer/tester → Direct → Main LLM
```

まで確認する。

---

# 29. 実装Phase

## Phase 1

Project skeleton。

実装：

```text
Go module
Config
MCP Server
health tool
```

---

## Phase 2

llama.cpp client。

実装：

```text
/v1/chat/completions
timeout
error handling
```

---

## Phase 3

Agent。

実装：

```text
coder
reviewer
tester
fixer
```

---

## Phase 4

Workspace。

実装：

```text
file reader
path validation
secret filtering
```

---

## Phase 5

local_agent_run。

```text
Codex
 ↓
MCP
 ↓
Agent
 ↓
LLM
```

を接続する。

---

## Phase 6

Test。

```text
unit
integration
E2E
```

---

# 30. Version 1 完了条件

以下をすべて満たした場合Version 1完成とする。

```text
[ ] CodexからMCP Serverを認識できる

[ ] local_agent_healthを実行できる

[ ] coderへタスクを渡せる

[ ] reviewerへ独立コンテキストでレビューさせられる

[ ] testerへテスト設計を依頼できる

[ ] fixerへ修正案を依頼できる

[ ] Direct BackendのQwenはRepositoryを書き換えない

[ ] Repository Root外を読み込めない

[ ] Codexが成果物をRepositoryへ適用できる

[ ] Codexがテストを実行できる

[ ] llama-server停止時に正常にエラー処理できる

[ ] go test ./... が成功する
```

---

# 31. Role別Backend routing

既定は全Role Directである。`MAC_CODER_BACKEND`と`MAC_FIXER_BACKEND`は独立して`direct`/`pi`を指定できる。ReviewerとTesterはMain LLMのDirect Backendを使う。Pi設定はCoder/Fixerで共有し、どちらかがPiならPi readinessを一度確認する。Piを起動できない場合はエラーとし、Directへfallbackしない。

```text
local_agent_run
  ├─ coder            -> DirectBackend / Coding LLM or PiBackend / temporary worktree
  ├─ fixer            -> DirectBackend / Coding LLM or PiBackend / overlay worktree
  └─ reviewer, tester -> DirectBackend / Main LLM
```

Direct Coder/FixerはJSON成果物を返し、実ファイルの適用とテスト実行はCodexが担当する。Pi Coder/Fixerは1タスクにつき新規RPCプロセスを起動し、一時worktree上でread/bash/edit/writeとテストを行う。MACはGit状態からPatchとChangedFilesを回収し、Pi最終回答を成果物JSONとして解釈しない。Agent全体のPi timeoutは既定無制限だが、起動・health・通信idle・キャンセル処理には有限の制限を設ける。CoderはHEADから開始し、Fixerは`files[]`指定対象のMain Working Tree状態だけをoverlayする。Fixer Patchはoverlay後Baselineからの差分とする。

---

# 31. 将来候補

Version 1完成後に検討する。

```text
並列Agent
Agentごとのtemperature設定
Task Graph
Persistent Task State
Web UI
RAG
Agent performance metrics
Token metrics
Prompt evaluation
```

---

# 32. 最終方針

現在は、

```text
Codex
    ↓
multi-agent-coder MCP
    ↓
Role別に選択したMain/Coding llama-server
    ↓
Local LLM
```

RoleごとにBackendを解決し、System PromptとContextを分離する。Main/Codingの接続先は環境変数で指定し、LLM ClientはRoleによる接続先選択を行わない。

Direct Coder/FixerのCoding LLMへの応答にはAgent JSON schemaを指定する。Reviewer/TesterのMain LLMには従来のJSON object形式を使う。Pi Coder/FixerはJSON artifactを生成せず、MACがworktree状態からRunOutputを生成する。

Codexは設計判断、Main Working Treeへの変更適用、最終レビュー、統合判断を担当する。Pi Coder/Fixerはtemporary worktree内の実装・修正とテストのみを担当する。

```text
Piのworktree外の変更適用
Git履歴操作
最終テスト判定
最終承認
```

はCodexが担当する。

これによりローカルLLMの誤操作によるRepository破壊を抑えつつ、Codexの高度な判断能力とローカルLLMの計算資源を組み合わせる。

---

# 33. Claude Code対応

Codexに加えて、Claude CodeからもMCP Serverとして呼び出せるようにする。Codexからの登録手順と挙動は変えない。

## Orchestratorの選択

呼び出し元は `MAC_ORCHESTRATOR`（`codex` | `claude-code`）で表す。未設定時は `codex` とし、従来とまったく同じ挙動にする。

`.env.qwen` / `.env.strata` はCodexとClaude Codeで共有する。`.env` はBashの `source` で読み込まれ既存の環境変数を上書きするため、呼び出し元は `.env` ではなく `scripts/run-mcp-server` の第2引数で渡す。ラッパーは `.env` の読み込み後に `MAC_ORCHESTRATOR` を設定し、`.env` の同名の行より起動引数を優先する。

```text
Codex:       scripts/run-mcp-server qwen
Claude Code: scripts/run-mcp-server qwen claude-code
```

`local_agent_health` は `orchestrator` フィールドで現在の値を返す。リクエストログにも `orchestrator` を記録する。

## Configuration

```text
MAC_ORCHESTRATOR=codex
MAC_CLAUDE_MAX_OUTPUT_CHARS=80000
MAC_CLAUDE_TOOL_ERROR_FLAG=true
```

`MAC_CLAUDE_MAX_OUTPUT_CHARS` は0以上の整数（0は無制限）、`MAC_CLAUDE_TOOL_ERROR_FLAG` は `true` / `false` とする。`MAC_CLAUDE_*` はClaude Code専用の設定で、Codexから起動した場合は挙動に反映しない。ただし共有 `.env` の誤記をCodexからの起動時にも検出するため、`MAC_ORCHESTRATOR` の値に関係なく常に検証し、不正な値は `ConfigError` として起動を中止する。

## Promptの切り替え

`claude-code` の場合、Direct BackendのSystem Promptとして `prompts/claude-code/<role>.md` を使う。内容は `prompts/<role>.md` の「Codex」を「Claude Code」に置き換えたものであり、それ以外のルール、順序、文言は変えない。Codexとの比較評価のため差分を最小に保つ。

User Messageの構造と `OUTPUT FORMAT` は両者で共通とする。Pi Backendの `buildPiPrompt` は呼び出し元に依存する文言を含まないため共通とする。

## 出力制限

Claude CodeはMCP Toolの出力量に上限を持つ（`MAX_MCP_OUTPUT_TOKENS`）。`claude-code` かつ `MAC_CLAUDE_MAX_OUTPUT_CHARS > 0` の場合、`local_agent_run` の `RunOutput` をJSONエンコードした文字数が上限を超えたら、`patch` と各 `artifacts[].content` を後ろから切り詰めて上限内に収める。

- 各フィールドに共通の上限文字数を設け、全体が収まる最大の値を選ぶ。小さいフィールドはそのまま残り、大きいフィールドは均等に切り詰められる。
- 切り詰めたフィールドごとに `warnings` へ `OutputTruncated: <field> truncated from N to M chars` を追加する。
- 切り詰め後のPatchや成果物は適用できないため、タスクの分割を求める `OutputTruncated:` で始まるメッセージも `warnings` へ追加する。
- `summary`、`findings`、`risks`、`notes`、`error`、`changed_files` は切り詰めない。これらだけで上限を超える場合は、その旨を `OutputTruncated:` の警告として追加し、上限を超えたまま返す。

Orchestrator（Claude Code）は `OutputTruncated` を含む成果物を適用せず、タスクを分割して再依頼する。

## isError

`claude-code` かつ `MAC_CLAUDE_TOOL_ERROR_FLAG=true` の場合、`status` が `error` の `local_agent_run` 応答では、MCPの `CallToolResult.isError` を立てる。構造化出力（`structuredContent`）とテキストの `content` は、Codexの場合と同じ内容を返す。

## 責務

Claude Codeから作業する場合、本書でCodexが担当すると定めた要求分析、設計判断、タスク分割、ファイル変更、Shell実行、テスト実行、統合判断、最終承認は、Claude Codeが担当する。Direct Backendの成果物はClaude Codeが検証・適用し、テストもClaude Codeが実行する。Pi BackendのPatchはClaude Codeが内容を確認してから適用する。

Claude Code用の指示は `CLAUDE.md`（`AGENTS.md` を取り込む）と `.claude/skills/multi-agent-coder-delegation/SKILL.md` に置く。

## Codex挙動を変えない方針

`codex`（未設定を含む）の場合、Prompt、`local_agent_run` の応答、`isError` の扱いを従来から変えない。追加はログと `local_agent_health` の `orchestrator` フィールドのみとする。既存の公開API、関数シグネチャ、環境変数、既定値、Codexからの登録手順は変えない。
