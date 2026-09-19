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

llama.cpp Serverは別途起動してください。本Serverは既定で `http://127.0.0.1:8080` へ接続します。

## CodexへMCP Serverを追加

次のコマンド内の `/absolute/path/to/multi-agent-coder` と `/absolute/path/to/target-repository` を、実際の絶対パスへ置き換えて実行します。`MAC_REPOSITORY_ROOT` には、このServer自身ではなくローカルAgentに読み取らせる対象Repositoryを指定します。

```bash
codex mcp add multi-agent-coder \
  --env MAC_LLM_BASE_URL=http://127.0.0.1:8080 \
  --env MAC_LLM_TIMEOUT=180s \
  --env MAC_LLM_MAX_TOKENS=4096 \
  --env MAC_CODER_MAX_TOKENS=4096 \
  --env MAC_REVIEWER_MAX_TOKENS=1536 \
  --env MAC_TESTER_MAX_TOKENS=1536 \
  --env MAC_FIXER_MAX_TOKENS=4096 \
  --env MAC_REPOSITORY_ROOT=/absolute/path/to/target-repository \
  --env MAC_LOG_LEVEL=info \
  -- /absolute/path/to/multi-agent-coder/multi-agent-coder
```

llama.cpp Server側でモデルが固定されていない場合は、モデル名に合わせて `--env MAC_LLM_MODEL=qwen-model-name` も追加してください。登録結果は次のコマンドで確認できます。

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

設定変更後はCodex CLIまたはIDE拡張機能を再起動してください。

## max_tokens設定

Agent実行時の `max_tokens` はRole別の環境変数から取得します。

| 環境変数 | 対象 | 既定値 |
| --- | --- | ---: |
| `MAC_LLM_MAX_TOKENS` | Roleを指定しない低レベル呼び出し | 4096 |
| `MAC_CODER_MAX_TOKENS` | coder | 4096 |
| `MAC_REVIEWER_MAX_TOKENS` | reviewer | 1536 |
| `MAC_TESTER_MAX_TOKENS` | tester | 1536 |
| `MAC_FIXER_MAX_TOKENS` | fixer | 4096 |

各値には1以上の整数を指定します。不正な値の場合、Serverは設定エラーとして起動を中止します。`MAC_CODER_BACKEND=pi` の場合、coderの生成設定はPi側が管理するため、`MAC_CODER_MAX_TOKENS` はDirect backendにのみ適用されます。

## Pi coder backend（オプション）

`MAC_CODER_BACKEND=pi` を明示すると、coderだけがPi Coding AgentのRPCモードを使用します。reviewer、tester、fixerは常に従来のDirect backendです。既定値は `direct` で、Piエラー時の暗黙fallbackはありません。

```text
MAC_CODER_BACKEND=pi
MAC_PI_EXECUTABLE=pi
MAC_PI_PROVIDER=local-qwen
MAC_PI_MODEL=qwen3.8-27b
MAC_PI_RPC_ARGS=
MAC_PI_TIMEOUT=300s
```

Codex側のTool Call timeoutより短い `MAC_PI_TIMEOUT` を設定し、Pi timeout後の差分収集、worktree cleanup、構造化エラー返却の時間を確保してください。

Piは別途インストールし、Pi自身の `models.json` でprovider/modelとllama.cpp接続を設定してください。llama.cpp Serverの起動・停止は本Serverでは管理しません。`MAC_PI_EXECUTABLE` が見つからなければ明示的なエラーになります。RPC追加引数は空白区切りです（引用符を解釈するshell parserではありません）。

Pi実行ごとにGit `HEAD` から安全な内部IDの一時branchとOS temp directory内のworktreeを作り、Piのworking directoryをそこに固定します。差分（untracked fileを含むbinary patch）を回収してからworktreeとbranchを削除します。本体working treeがdirtyでも実行できますが、その未コミット変更はPiには渡らず、応答に警告が入ります。

Piの最終文章はモデル報告として明示し、実変更はGit patchを正とします。timeoutや異常終了でも回収できたpatch、changed files、tool診断、warningsを `status=error` の応答に保持します。tool診断とstderrはサイズを制限して保持します。

Git worktreeは編集競合を避けるための隔離であり、OSレベルのsandboxではありません。PiはServer実行ユーザーの権限を持つため、信頼できるPi設定・モデル・プロジェクトでのみ利用してください。

Pi RPCの仕様は [Pi公式RPC documentation](https://pi.dev/docs/latest/rpc) を参照してください。
