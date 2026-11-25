# multi-agent-coder

ローカルLLMを coder / reviewer / tester / fixer として利用するSTDIO MCP Serverです。既定ではすべてのロールがllama.cppを直接呼び出します。CoderとFixerはそれぞれ独立してPi Backendを選べます。

## E2E regression suite

小規模な Calculator repository、8つの固定シナリオ、個別・一括実行
scripts は [`multi-agent-coder-e2e/`](multi-agent-coder-e2e/) にあります。
Codex→MCP の live acceptance と、mock LLM を使う決定的なエラー分類テストを
分離しています。詳細は `multi-agent-coder-e2e/docs/SCENARIOS.md` を参照してください。

## ビルド

Go 1.24以降と、OpenAI互換の `/v1/chat/completions` を提供するllama.cpp Serverが必要です。

```bash
cd /absolute/path/to/multi-agent-coder
go build -o ./multi-agent-coder ./cmd/multi-agent-coder
```

llama.cpp Serverは別途起動してください。標準構成ではCoder/FixerがCoding LLM（`127.0.0.1:8089`）をDirectで使用し、Reviewer/TesterがMain LLM（推奨Qwen3.8-27B、`127.0.0.1:8089`）をDirectで使用します。`MAC_CODER_BACKEND`と`MAC_FIXER_BACKEND`は個別に`direct`または`pi`を選べます。Reviewer/TesterはDirectのみです。Piが起動できない場合にDirectへfallbackしません。

## Agentの権限と成果物

Direct Backendは従来どおりread-onlyでJSON成果物を返します。Pi Coder/Fixer Backendは一時Git worktree内でread/bash/edit/writeとテストを行い、MACがGit差分をPatchとして回収します。Coder worktreeはHEADから作成します。FixerはMain Working Tree全体ではなく`files[]`指定対象だけ現在状態をoverlayし、overlay後BaselineからFinalまでの差分だけを返します。Piへ`files`の本文は渡さず、starting file pathとして伝えます。Piが変更していない既存dirtyファイルはPatch/ChangedFilesに含めません。WorktreeはRepository編集の隔離であり、OS Sandboxではありません。Piの処理は1 Taskごとに新規プロセス/Sessionです。

### Pi Coder/Fixerの設定

Piは任意機能です。Coder/Fixerが共通のPi設定を使います。llama.cppの`/v1/models`に表示されるモデルIDに合わせて、利用者側でPi Agent Directoryの`models.json`を設定してください。モデルIDはMAC側にハードコードしません。

```text
MAC_PI_AGENT_DIR=/home/user/.pi/mac-agent
```

MACはPi子プロセスの`PI_CODING_AGENT_DIR`へこの値を渡します。`APPEND_SYSTEM.md`はPiが標準で読み込む追加System Prompt用ファイルです。

`models.json`のOpenAI互換provider例（実際のmodel IDは`/v1/models`に合わせる）：

```json
{
  "providers": {
    "local-qwen": {
      "baseUrl": "http://127.0.0.1:8089/v1",
      "api": "openai-completions",
      "apiKey": "local",
      "models": [{"id": "<llama.cpp-model-id>", "name": "Qwen3.8-27B", "contextWindow": 65536, "maxTokens": 8192}]
    }
  }
}
```

MACはPi起動時に`PI_CODING_AGENT_DIR`へ`MAC_PI_AGENT_DIR`を設定します。Pi設定ファイルはMACが生成・変更しません。利用者が用意する`settings.json`の推奨値はProvider `local-qwen`、Model `qwen`、Thinking `medium`、budget `2048`、tools `read,bash,edit,write`、session persistence/extensions disabled、provider retry `0`、agent retry `1`、HTTP idle timeout `300000ms`、compaction reserve `16384`/keep recent `12000`です。Piのバージョンに合わせて設定schemaを確認してください。

```json
{
  "defaultProvider": "local-qwen", "defaultModel": "qwen", "defaultThinkingLevel": "medium",
  "thinkingBudgets": {"minimal": 256, "low": 1024, "medium": 2048, "high": 4096, "xhigh": 8192},
  "defaultTools": ["read", "bash", "edit", "write"],
  "retry": {"enabled": true, "maxRetries": 1, "provider": {"maxRetries": 0}},
  "httpIdleTimeoutMs": 300000,
  "compaction": {"enabled": true, "reserveTokens": 16384, "keepRecentTokens": 12000}
}
```

Pi起動例：

```bash
codex mcp add multi-agent-coder \
  --env MAC_CODER_BACKEND=pi \
  --env MAC_FIXER_BACKEND=pi \
  --env MAC_PI_EXECUTABLE=/absolute/path/to/pi \
  --env MAC_PI_PROVIDER=local-qwen \
  --env MAC_PI_MODEL=qwen \
  --env MAC_PI_AGENT_DIR=/home/user/.pi/mac-agent \
  --env MAC_PI_RPC_ARGS= \
  --env MAC_PI_TIMEOUT=0 \
  --env MAC_LLM_BASE_URL=http://127.0.0.1:8089 \
  --env MAC_LLM_MODEL=qwen3.8-27b \
  --env MAC_LLM_TIMEOUT=180s \
  --env MAC_CODING_LLM_BASE_URL=http://127.0.0.1:8089 \
  --env MAC_CODING_LLM_MODEL=qwen-coder \
  --env MAC_CODING_LLM_TIMEOUT=180s \
  -- /absolute/path/to/multi-agent-coder/multi-agent-coder --workspace-from-cwd
```

`MAC_PI_TIMEOUT=0`はPi Agent全体のHard Timeoutなしを意味します。MCP Context cancellation、Pi startup 15秒、RPC idle 300秒、shutdown grace 3秒は維持します。Piは`agent_settled`まで待ちます。PiはGit操作を行わず、PatchはworktreeのGit状態からMACが作ります。

## CodexへMCP Serverを追加

次のコマンド内の `/absolute/path/to/multi-agent-coder` を、MACを設置した絶対パスへ置き換えて実行します。MCP登録は一度で済みます。作業するGitリポジトリでCodexを開くと、配下のサブディレクトリからでもMACがGitルートを特定します。

```bash
codex mcp add multi-agent-coder \
  --env MAC_LLM_BASE_URL=http://127.0.0.1:8089 \
  --env MAC_LLM_MODEL=qwen3.8-27b \
  --env MAC_LLM_TIMEOUT=180s \
  --env MAC_CODING_LLM_BASE_URL=http://127.0.0.1:8089 \
  --env MAC_CODING_LLM_MODEL=qwen-coder \
  --env MAC_CODING_LLM_TIMEOUT=180s \
  --env MAC_LLM_MAX_TOKENS=4096 \
  --env MAC_CODER_MAX_TOKENS=4096 \
  --env MAC_REVIEWER_MAX_TOKENS=4096 \
  --env MAC_TESTER_MAX_TOKENS=3072 \
  --env MAC_FIXER_MAX_TOKENS=4096 \
  --env MAC_LLM_REASONING_EFFORT=none \
  --env MAC_LLM_TEMPERATURE=0.2 \
  --env MAC_LOG_LEVEL=info \
  -- /absolute/path/to/multi-agent-coder/multi-agent-coder --workspace-from-cwd
```

登録結果は次のコマンドで確認できます。

```bash
codex mcp list
```

CLIによるSTDIO MCP Serverの登録形式については、[Codex公式MCPドキュメント](https://developers.openai.com/ja-JP/docs/extend/mcp)も参照してください。

MCP Serverを再ビルドした場合、登録済みの実行ファイルと同じパスへ出力すればMCP設定の再登録は不要です。実行中のCodex CLIまたはIDE拡張機能には、再起動後に反映されます。

### 作業リポジトリの確認

`local_agent_health` の `workspace_status: "ready"` と `repository_root` で対象を確認できます。ローカルCodexの例にある `--workspace-from-cwd` は、クライアントがセッションの作業ディレクトリでMCPプロセスを起動する場合に使います。CodexのMCP設定で `cwd` を固定せず、作業したいリポジトリでセッションを開いてください。Claude Codeでは `CLAUDE_PROJECT_DIR` が起点になります。

セッションの作業場所を引き継げない起動経路では `--workspace-from-cwd` を省略します。場所を取得できない場合やGit管理外の場合、`local_agent_run` は `status: "needs_input"` と `workspace_input` を返します。呼び出し元はユーザーに作業場所を聞き、指定または選択された絶対ディレクトリを `repository_directory` に入れて元の依頼を再送します。確定後、同じ接続ではパス指定が不要です。別の対象に切り替える場合は新しいMCPセッションを使います。`files` は表示されたGitルートからの相対パスです。

以前の登録から切り替える場合は、既存の `MAC_REPOSITORY_ROOT` をプロファイルやMCP登録から削除できます。ローカルCodexで自動検出を使う場合は、起動引数を上記の例に合わせてからセッションを開き直してください。

### MCP stderrログの保存

`scripts/run-mcp-server` をMCPの `command` に指定し、`args` で `qwen` または `strata` を選びます。`.env.example` をMACの設置先の `.env.qwen` または `.env.strata` にコピーし、モデルの設定を記入してください。ラッパーは設定ファイルをBashで読み込むため、信頼できるファイルを使ってください。設置先の `run-mcp-server` からも同じコマンドを実行できます。

stdoutはMCP Protocol専用です。stderrはCodexへ転送し、既定では `logs/multi-agent-coder-<profile>-YYYY-MM-DD.log` に保存します。`logs/` はGit管理対象外です。保存先は `MAC_LOG_DIR` で変更できます。

```toml
[mcp_servers.multi-agent-coder]
command = "/absolute/path/to/multi-agent-coder/scripts/run-mcp-server"
args = ["qwen", "codex", "--workspace-from-cwd"]
```

設定変更後はCodex CLIまたはIDE拡張機能を再起動してください。`local_agent_health` はMain LLMとCoding LLMを個別に確認します。各LLMの `/v1/models` へ有限Timeoutで接続し、Coding LLM停止時は `coding_llm.status=stopped` と `status=degraded` を返します。MCP Serverは起動したままです。

## Claude CodeへMCP Serverを追加

Claude CodeからもCodexと同じ `scripts/run-mcp-server` を使います。`.env.qwen` / `.env.strata` はCodexと共有し、第2引数 `claude-code` で呼び出し元を指定します。

```bash
claude mcp add multi-agent-coder -- /absolute/path/to/multi-agent-coder/scripts/run-mcp-server qwen claude-code
```

登録結果は次のコマンドで確認できます。Claude Codeのセッション内では `/mcp` でも接続状態を確認できます。

```bash
claude mcp list
```

取り付けと取り外しは、次のスクリプトでも行えます。scopeの既定は `local` で、カレントディレクトリのプロジェクトだけに登録します。Claude Codeを使うプロジェクトのディレクトリで実行してください。

```bash
# 取り付け（同じscopeの既存登録は置き換えるため、profileの切り替えにも使える）
/absolute/path/to/multi-agent-coder/scripts/claude-mcp-attach qwen          # scope: local
/absolute/path/to/multi-agent-coder/scripts/claude-mcp-attach strata user   # 全プロジェクトで使う場合

# 取り外し（scope省略時は登録されているscopeから削除。未登録でもエラーにしない）
/absolute/path/to/multi-agent-coder/scripts/claude-mcp-detach
```

反映には、Claude Codeの再起動か `/mcp` での再接続が必要です。Codexの登録（`~/.codex/config.toml`）には影響しません。

`scripts/run-mcp-server` の引数は `{qwen|strata} [codex|claude-code] [--workspace-from-cwd]` です。

- 第1引数は従来どおりモデルprofile（読み込む `.env.<profile>`）を選びます。
- 第2引数は呼び出し元のMCP Clientです。省略時は `codex` です。第3引数の `--workspace-from-cwd` を使うときは、第2引数も指定します。不正な引数はUsageをstderrに出して終了コード2で終了します。
- ラッパーは `.env.<profile>` を読み込んだ後で `MAC_ORCHESTRATOR` を第2引数の値に設定します。`.env` に `MAC_ORCHESTRATOR` の行があっても起動引数が優先されるため、`.env` には書かないでください。

`claude-code` で起動した場合だけ、次の処理が加わります。`local_agent_health` の `orchestrator` フィールドで、どちらで起動したかを確認できます。

- Role Promptとして `prompts/claude-code/<role>.md` を使います。内容は `prompts/<role>.md` の「Codex」を「Claude Code」に置き換えたものです。
- `local_agent_run` の応答が `MAC_CLAUDE_MAX_OUTPUT_CHARS` を超える場合、`patch` と各 `artifacts[].content` を後ろから切り詰め、`warnings` に `OutputTruncated: ...` を追加します。切り詰めた成果物は適用できないため、タスクを分割して再依頼してください。`summary`、`findings`、`risks`、`notes`、`error`、`changed_files` は切り詰めません。
- `status` が `error` の応答では、MCPの `isError` を立てます。構造化出力とテキストの内容はCodexの場合と同じです。

| 環境変数 | 用途 | 既定値 |
| --- | --- | --- |
| `MAC_CLAUDE_MAX_OUTPUT_CHARS` | Claude Code向け応答（JSON）の最大文字数。`0` は無制限 | `80000` |
| `MAC_CLAUDE_TOOL_ERROR_FLAG` | 失敗時にMCPの `isError` を立てるか（`true` / `false`） | `true` |

`MAC_CLAUDE_*` は共有 `.env` に書けます。Codexから起動した場合は挙動に反映しませんが、値の検証は常に行います。不正な値があるとCodexからの起動も設定エラーで中止されます。

Claude Code側の制限との関係：

- `MAX_MCP_OUTPUT_TOKENS` はClaude CodeがMCP Toolの出力として受け取るtoken数の上限です。`MAC_CLAUDE_MAX_OUTPUT_CHARS` は文字数による上限なので、token数との比率は内容（コード、日本語など）によって変わります。Claude Code側で出力が切られる場合は、`MAC_CLAUDE_MAX_OUTPUT_CHARS` を下げるか `MAX_MCP_OUTPUT_TOKENS` を上げてください。MAC側の上限内に収めると、どのフィールドが切られたかが `warnings` でわかります。
- `MCP_TOOL_TIMEOUT` はClaude CodeがMCP Toolの完了を待つ時間（ミリ秒）です。Pi Backendは既定でAgent全体のTimeoutがなく（`MAC_PI_TIMEOUT=0`）、実行が長くなることがあります。Pi Coder/Fixerを使う場合は、Pi実行時間より長い値を指定してClaude Codeを起動してください（例：`MCP_TOOL_TIMEOUT=1800000 claude`）。

Claude Code用のSkillは `.claude/skills/multi-agent-coder-delegation/SKILL.md`、指示ファイルは `CLAUDE.md` です。Claude Codeから作業する場合は、AGENTS.mdとdocs/DESIGN.mdでCodexが担う責務（成果物の検証・適用、テスト実行、Pi Patchの確認など）をClaude Codeが担います。

## 環境変数一覧

Server本体が読み取る以下の環境変数は省略可能で、省略時はコード内の既定値を使用します。利用するLLMの実際の構成に合わせて接続先とモデル名を指定してください。

| 環境変数 | 用途 | 既定値 |
| --- | --- | --- |
| `MAC_LLM_BASE_URL` | reviewer/testerのMain LLM | `http://127.0.0.1:8089` |
| `MAC_LLM_MODEL` | Main LLMのモデル名 | `qwen3.8-27b` |
| `MAC_LLM_TIMEOUT` | Main LLMのリクエストTimeout | `180s` |
| `MAC_CODING_LLM_BASE_URL` | coder/fixerのCoding LLM | `http://127.0.0.1:8089` |
| `MAC_CODING_LLM_MODEL` | Coding LLMのモデル名 | `qwen-coder` |
| `MAC_CODING_LLM_TIMEOUT` | Coding LLMのリクエストTimeout | `180s` |
| `MAC_LLM_MAX_TOKENS` | Role固有値がない場合の共通token上限 | `4096` |
| `MAC_CODER_MAX_TOKENS` | coderのtoken上限 | `4096` |
| `MAC_REVIEWER_MAX_TOKENS` | reviewerのtoken上限 | `4096` |
| `MAC_TESTER_MAX_TOKENS` | testerのtoken上限 | `3072` |
| `MAC_FIXER_MAX_TOKENS` | fixerのtoken上限 | `4096` |
| `MAC_LLM_REASONING_EFFORT` | Direct LLMリクエストのreasoning effort | `none` |
| `MAC_LLM_TEMPERATURE` | Direct LLMリクエストのtemperature | `0.2` |
| `MAC_LOG_LEVEL` | Serverのログレベル | `info` |
| `MAC_CODER_BACKEND` | Coder backend (`direct` / `pi`) | `direct` |
| `MAC_FIXER_BACKEND` | Fixer backend (`direct` / `pi`) | `direct` |
| `MAC_PI_EXECUTABLE` | Pi実行ファイルまたはコマンド名 | `pi` |
| `MAC_PI_PROVIDER` | Pi provider | `local-qwen` |
| `MAC_PI_MODEL` | Pi model selector | `qwen` |
| `MAC_PI_AGENT_DIR` | Pi専用Agent Directory（空ならPi標準値） | 空 |
| `MAC_PI_RPC_ARGS` | 追加RPC引数（空白区切り） | 空 |
| `MAC_PI_TIMEOUT` | Pi Agent全体Timeout。`0`は無制限 | `0` |
| `MAC_ORCHESTRATOR` | 呼び出し元のMCP Client（`codex` / `claude-code`）。`scripts/run-mcp-server` が第2引数から設定するため、`.env` には書かない | `codex` |
| `MAC_CLAUDE_MAX_OUTPUT_CHARS` | Claude Code向け応答の最大文字数。`0`は無制限。`claude-code`時のみ適用 | `80000` |
| `MAC_CLAUDE_TOOL_ERROR_FLAG` | 失敗時にMCPの `isError` を立てるか（`true` / `false`）。`claude-code`時のみ適用 | `true` |

URLには絶対HTTP(S) URL、Timeoutには正のGo duration、token上限には1以上の整数を指定します。`MAC_LLM_TEMPERATURE` は0以上2以下、`MAC_LOG_LEVEL` は `debug`、`info`、`warn`、`error` のいずれかです。不正な値の場合、Serverは設定エラーとして起動を中止します。

`scripts/run-mcp-server` を使用する場合だけ、ログ保存先を `MAC_LOG_DIR` で変更できます。この変数はラッパーScript専用で、Server本体は読み取りません。`MAC_LIVE_ROLE_E2E` は実LLMを使う任意のrouting testを有効化するための変数であり、通常のServer実行には不要です。

Coding LLMへのDirectリクエストはJSON schemaで応答形式を制約し、既存のJSON検証で内容を確認します。Main LLMへのDirectリクエストは従来の `json_object` を使用します。

実サーバーを使うRole routing E2Eは、通常の `MAC_*` 設定からMain LLMとCoding LLMの接続先・モデル名を読み取ります。テストは一時Git Repositoryを作り、coder/fixer→Coding LLM、reviewer/tester→Main LLMの各経路で、直接生成したJSON成果物を確認します。

```bash
MAC_LIVE_ROLE_E2E=1 go test -v ./internal/mcp -run '^TestLiveRoleRouting$' -count=1 -timeout=9m
```

実行前に `MAC_LLM_BASE_URL`、`MAC_CODING_LLM_BASE_URL` と必要に応じて各モデル名を現在のローカル構成に合わせてexportしてください。

Pi Coder/Fixerの実装能力を確認する追加E2Eもあります。整数区間の正規化を課題とし、先行テスト、入力非破壊、整数境界、784通りの組み合わせを検証します。`MAC_CODER_BACKEND=pi`、`MAC_FIXER_BACKEND=pi`とPi設定をexportして実行してください。テスト専用の一時RepositoryだけにPatchを適用します。既知の末尾LF欠落は記録したうえで補完するため、無加工Patchの適用可否とは別の検証です。

```bash
MAC_LIVE_IMPLEMENTATION_E2E=1 go test -v ./internal/mcp -run '^TestLiveImplementationE2E$' -count=1 -timeout=15m
```

## max_tokens設定

Agent実行時の `max_tokens` はRole別の環境変数から取得します。

| 環境変数 | 対象 | 既定値 |
| --- | --- | ---: |
| `MAC_LLM_MAX_TOKENS` | Roleを指定しない低レベル呼び出し | 4096 |
| `MAC_CODER_MAX_TOKENS` | coder | 4096 |
| `MAC_REVIEWER_MAX_TOKENS` | reviewer | 4096 |
| `MAC_TESTER_MAX_TOKENS` | tester | 3072 |
| `MAC_FIXER_MAX_TOKENS` | fixer | 4096 |

各値には1以上の整数を指定します。Role固有の設定が未指定なら `MAC_LLM_MAX_TOKENS` を使用し、それも未指定なら表の既定値を使用します。不正な値の場合、Serverは設定エラーとして起動を中止します。
