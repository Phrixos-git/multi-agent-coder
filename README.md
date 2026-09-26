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

llama.cpp Serverは別途起動してください。標準構成ではCoder/FixerがCoding LLM（Qwen2.5-Coder-7B、`127.0.0.1:18082`）をDirectで使用し、Reviewer/TesterがMain LLM（Qwen3.8-27B、`127.0.0.1:8080`）をDirectで使用します。ポートとモデル名は環境変数から設定します。

## Agentの権限と成果物

Codexは必要なTask、関連ファイル、RequirementsだけをAgentへ渡します。Local LLMは受け取った内容から実装案・レビュー・テスト案・修正案をJSON成果物として返し、Repositoryを直接読み書きしません。Agentにread/edit/write tool、Shell、Git操作は与えません。成果物の選択とRepositoryへの適用、Shellやテストの実行、Git操作、最終判断はCodexが行います。Coderへの1回の依頼は1つの明確な目的に絞り、大きな作業は小さなTaskに分割してください。

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

設定変更後はCodex CLIまたはIDE拡張機能を再起動してください。`local_agent_health` はMain LLMとCoding LLMを個別に確認します。各LLMの `/v1/models` へ有限Timeoutで接続し、Coding LLM停止時は `coding_llm.status=stopped` と `status=degraded` を返します。MCP Serverは起動したままです。

## 環境変数一覧

Server本体が読み取る環境変数はすべて省略可能で、省略時はコード内の既定値を使用します。ただし、意図しないRepositoryを対象にしないよう、実運用では `MAC_REPOSITORY_ROOT` を明示してください。また、利用するLLMの実際の構成に合わせて接続先とモデル名を指定してください。

| 環境変数 | 用途 | 既定値 |
| --- | --- | --- |
| `MAC_REPOSITORY_ROOT` | Agentが参照する対象Repository | Server起動時のカレントディレクトリ |
| `MAC_LLM_BASE_URL` | reviewer/testerのMain LLM | `http://127.0.0.1:8080` |
| `MAC_LLM_MODEL` | Main LLMのモデル名 | `qwen3.8-27b` |
| `MAC_LLM_TIMEOUT` | Main LLMのリクエストTimeout | `180s` |
| `MAC_CODING_LLM_BASE_URL` | coder/fixerのCoding LLM | `http://127.0.0.1:18082` |
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

URLには絶対HTTP(S) URL、Timeoutには正のGo duration、token上限には1以上の整数を指定します。`MAC_LLM_TEMPERATURE` は0以上2以下、`MAC_LOG_LEVEL` は `debug`、`info`、`warn`、`error` のいずれかです。不正な値の場合、Serverは設定エラーとして起動を中止します。

`scripts/run-mcp-server` を使用する場合だけ、ログ保存先を `MAC_LOG_DIR` で変更できます。この変数はラッパーScript専用で、Server本体は読み取りません。`MAC_LIVE_ROLE_E2E` は実LLMを使う任意のrouting testを有効化するための変数であり、通常のServer実行には不要です。

Coding LLMへのDirectリクエストはJSON schemaで応答形式を制約し、既存のJSON検証で内容を確認します。Main LLMへのDirectリクエストは従来の `json_object` を使用します。

実サーバーを使うRole routing E2Eは、通常の `MAC_*` 設定からMain LLMとCoding LLMの接続先・モデル名を読み取ります。テストは一時Git Repositoryを作り、coder/fixer→Coding LLM、reviewer/tester→Main LLMの各経路で、直接生成したJSON成果物を確認します。

```bash
MAC_LIVE_ROLE_E2E=1 go test -v ./internal/mcp -run '^TestLiveRoleRouting$' -count=1 -timeout=9m
```

実行前に `MAC_LLM_BASE_URL`、`MAC_CODING_LLM_BASE_URL` と必要に応じて各モデル名を現在のローカル構成に合わせてexportしてください。

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
