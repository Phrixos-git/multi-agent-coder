# multi-agent-coder

ローカルLLMを coder / reviewer / tester / fixer として利用するSTDIO MCP Serverです。既定ではすべてのロールがllama.cppを直接呼び出します。

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

llama.cpp Serverは別途起動してください。標準構成ではMain LLM（Qwen3.8-27B、`127.0.0.1:8080`）をreviewer/testerがDirectで使用し、Coding LLM（Coding Model、`127.0.0.1:18082`）をfixerがDirectで使用します。coderはPi経由でCoding Modelを使用します。ポートは環境変数から設定し、Server内には固定しません。

## CodexへMCP Serverを追加

次のコマンド内の `/absolute/path/to/multi-agent-coder` と `/absolute/path/to/target-repository` を、実際の絶対パスへ置き換えて実行します。`MAC_REPOSITORY_ROOT` には、このServer自身ではなくローカルAgentに読み取らせる対象Repositoryを指定します。

```bash
codex mcp add multi-agent-coder \
  --env MAC_LLM_BASE_URL=http://127.0.0.1:8080 \
  --env MAC_LLM_MODEL=qwen3.8-27b \
  --env MAC_LLM_TIMEOUT=180s \
  --env MAC_CODING_LLM_BASE_URL=http://127.0.0.1:18082 \
  --env MAC_CODING_LLM_MODEL=qwen-coder \
  --env MAC_CODING_LLM_TIMEOUT=180s \
  --env MAC_LLM_MAX_TOKENS=4096 \
  --env MAC_CODER_MAX_TOKENS=4096 \
  --env MAC_REVIEWER_MAX_TOKENS=4096 \
  --env MAC_TESTER_MAX_TOKENS=3072 \
  --env MAC_FIXER_MAX_TOKENS=4096 \
  --env MAC_LLM_REASONING_EFFORT=none \
  --env MAC_LLM_TEMPERATURE=0.2 \
  --env MAC_CODER_BACKEND=pi \
  --env MAC_PI_EXECUTABLE=pi \
  --env MAC_PI_PROVIDER=local-coder \
  --env MAC_PI_MODEL=qwen-coder \
  --env MAC_PI_RPC_ARGS= \
  --env MAC_PI_TIMEOUT=300s \
  --env MAC_REPOSITORY_ROOT=/absolute/path/to/target-repository \
  --env MAC_LOG_LEVEL=info \
  -- /absolute/path/to/multi-agent-coder/multi-agent-coder
```

登録結果は次のコマンドで確認できます。

```bash
codex mcp list
```

CLIによるSTDIO MCP Serverの登録形式については、[Codex公式MCPドキュメント](https://developers.openai.com/ja-JP/docs/extend/mcp)も参照してください。

MCP Serverを再ビルドした場合、登録済みの実行ファイルと同じパスへ出力すればMCP設定の再登録は不要です。実行中のCodex CLIまたはIDE拡張機能には、再起動後に反映されます。

### MCP stderrログの保存

`scripts/run-mcp-server` をMCPの `command` に指定すると、stdoutはMCP Protocol専用のまま、stderrをCodexへ転送しつつ日次ログへ保存します。既定の保存先は `logs/multi-agent-coder-YYYY-MM-DD.log` です。`logs/` はGit管理対象外です。保存先は `MAC_LOG_DIR` で変更できます。

```toml
[mcp_servers.multi-agent-coder]
command = "/absolute/path/to/multi-agent-coder/scripts/run-mcp-server"
```

設定変更後はCodex CLIまたはIDE拡張機能を再起動してください。`local_agent_health` はMain LLM、Coding LLM、Piを個別に確認します。LLMは `/v1/models` への接続を確認し、Piは実行ファイルの存在を確認します。Coding LLM停止時は `coding_llm.status=stopped` と `status=degraded` を返し、MCP Serverは起動したままです。

## 環境変数一覧

Server本体が読み取る環境変数はすべて省略可能で、省略時はコード内の既定値を使用します。ただし、意図しないRepositoryを対象にしないよう、実運用では `MAC_REPOSITORY_ROOT` を明示してください。また、利用するLLMおよびPiの実際の構成に合わせて接続先、モデル名、backendを指定してください。

| 環境変数 | 用途 | 既定値 |
| --- | --- | --- |
| `MAC_REPOSITORY_ROOT` | Agentが参照する対象Repository | Server起動時のカレントディレクトリ |
| `MAC_CODER_BACKEND` | coderのbackend（`direct` または `pi`） | `direct` |
| `MAC_LLM_BASE_URL` | reviewer/testerおよびdirect coderのMain LLM | `http://127.0.0.1:8080` |
| `MAC_LLM_MODEL` | Main LLMのモデル名 | 空文字列（既存互換） |
| `MAC_LLM_TIMEOUT` | Main LLMのリクエストTimeout | `180s` |
| `MAC_CODING_LLM_BASE_URL` | fixerのCoding LLM | `http://127.0.0.1:18082` |
| `MAC_CODING_LLM_MODEL` | Coding LLMのモデル名 | `qwen-coder` |
| `MAC_CODING_LLM_TIMEOUT` | Coding LLMのリクエストTimeout | `180s` |
| `MAC_LLM_MAX_TOKENS` | Role固有値がない場合の共通token上限 | `4096` |
| `MAC_CODER_MAX_TOKENS` | direct coderのtoken上限 | `4096` |
| `MAC_REVIEWER_MAX_TOKENS` | reviewerのtoken上限 | `4096` |
| `MAC_TESTER_MAX_TOKENS` | testerのtoken上限 | `3072` |
| `MAC_FIXER_MAX_TOKENS` | fixerのtoken上限 | `4096` |
| `MAC_LLM_REASONING_EFFORT` | Direct LLMリクエストのreasoning effort | `none` |
| `MAC_LLM_TEMPERATURE` | Direct LLMリクエストのtemperature | `0.2` |
| `MAC_LOG_LEVEL` | Serverのログレベル | `info` |
| `MAC_PI_EXECUTABLE` | Pi実行ファイル名またはパス | `pi` |
| `MAC_PI_PROVIDER` | Piへ渡す `--provider` | 空文字列（指定しない） |
| `MAC_PI_MODEL` | Piへ渡す `--model` | 空文字列（指定しない） |
| `MAC_PI_RPC_ARGS` | Piへ渡す追加引数（空白区切り） | 空文字列 |
| `MAC_PI_TIMEOUT` | Pi実行Timeout | `300s` |

URLには絶対HTTP(S) URL、Timeoutには正のGo duration、token上限には1以上の整数を指定します。`MAC_LLM_TEMPERATURE` は0以上2以下、`MAC_LOG_LEVEL` は `debug`、`info`、`warn`、`error` のいずれかです。不正な値の場合、Serverは設定エラーとして起動を中止します。

`scripts/run-mcp-server` を使用する場合だけ、ログ保存先を `MAC_LOG_DIR` で変更できます。この変数はラッパーScript専用で、Server本体は読み取りません。`MAC_PI_E2E` と `MAC_LIVE_ROLE_E2E` はテストを明示的に有効化するための変数であり、通常のServer実行には不要です。

Coding LLMへのDirectリクエストはJSON schemaで応答形式を制約し、既存のJSON検証で内容を確認します。Main LLMへのDirectリクエストは従来の `json_object` を使用します。

実サーバーを使うRole routing E2Eは、通常の `MAC_*` 設定からMain LLM、Coding LLM、Piの接続先とモデルを読み取ります。`MAC_PI_PROVIDER` と `MAC_PI_MODEL` は、Piの `models.json` に登録した実際のprovider/model名に合わせてください。`MAC_LLM_MODEL` が空なら、テストはMain LLMの `/v1/models` からモデルIDを取得します。テストは一時Git Repositoryを作り、reviewer/tester→Main LLM、fixer→Coding LLM、coder→Piの各経路を実行します。

```bash
MAC_LIVE_ROLE_E2E=1 go test -v ./internal/mcp -run '^TestLiveRoleRouting$' -count=1 -timeout=9m
```

実行前に `MAC_LLM_BASE_URL`、`MAC_CODING_LLM_BASE_URL`、必要に応じて各モデル名、`MAC_PI_PROVIDER`、`MAC_PI_MODEL` を現在のローカル構成に合わせてexportしてください。Piのprovider/modelが未設定なら、テストは呼び出し前に不足項目を示して失敗します。

## max_tokens設定

Agent実行時の `max_tokens` はRole別の環境変数から取得します。

| 環境変数 | 対象 | 既定値 |
| --- | --- | ---: |
| `MAC_LLM_MAX_TOKENS` | Roleを指定しない低レベル呼び出し | 4096 |
| `MAC_CODER_MAX_TOKENS` | coder | 4096 |
| `MAC_REVIEWER_MAX_TOKENS` | reviewer | 4096 |
| `MAC_TESTER_MAX_TOKENS` | tester | 3072 |
| `MAC_FIXER_MAX_TOKENS` | fixer | 4096 |

各値には1以上の整数を指定します。Role固有の設定が未指定なら `MAC_LLM_MAX_TOKENS` を使用し、それも未指定なら表の既定値を使用します。不正な値の場合、Serverは設定エラーとして起動を中止します。`MAC_CODER_BACKEND=pi` の場合、coderの生成設定はPi側が管理するため、`MAC_CODER_MAX_TOKENS` はDirect backendにのみ適用されます。

## Pi coder backend（オプション）

`MAC_CODER_BACKEND=pi` を明示すると、coderだけがPi Coding AgentのRPCモードを使用します。fixerはDirect Coding LLM、reviewer/testerはDirect Main LLMを使用します。既定値 `direct` は後方互換のため維持し、その場合のcoderはMain LLMへ直接接続します。Piエラー時の暗黙fallbackはありません。

```text
MAC_CODER_BACKEND=pi
MAC_PI_EXECUTABLE=pi
MAC_PI_PROVIDER=local-coder
MAC_PI_MODEL=qwen-coder
MAC_PI_RPC_ARGS=
MAC_PI_TIMEOUT=300s
```

Codex側のTool Call timeoutより短い `MAC_PI_TIMEOUT` を設定し、Pi timeout後の差分収集、worktree cleanup、構造化エラー返却の時間を確保してください。

Piは別途インストールし、Pi自身の `models.json` で `local-coder` providerを `http://127.0.0.1:18082` に向け、`qwen-coder` modelの生成上限を4096 tokensに設定してください。本Serverは `models.json` を変更せず、llama.cpp Serverの起動・停止も管理しません。`MAC_PI_EXECUTABLE` が見つからなければ明示的なエラーになります。RPC追加引数は空白区切りです（引用符を解釈するshell parserではありません）。

Pi実行ごとにGit `HEAD` から安全な内部IDの一時branchとOS temp directory内のworktreeを作り、Piのworking directoryをそこに固定します。差分（untracked fileを含むbinary patch）を回収してからworktreeとbranchを削除します。本体working treeがdirtyでも実行できますが、その未コミット変更はPiには渡らず、応答に警告が入ります。

Piの最終文章はモデル報告として明示し、実変更はGit patchを正とします。timeoutや異常終了でも回収できたpatch、changed files、tool診断、warningsを `status=error` の応答に保持します。tool診断とstderrはサイズを制限して保持します。

Git worktreeは編集競合を避けるための隔離であり、OSレベルのsandboxではありません。PiはServer実行ユーザーの権限を持つため、信頼できるPi設定・モデル・プロジェクトでのみ利用してください。

Pi RPCの仕様は [Pi公式RPC documentation](https://pi.dev/docs/latest/rpc) を参照してください。
