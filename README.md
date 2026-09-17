# multi-agent-coder

ローカルLLMを coder / reviewer / tester / fixer として利用するSTDIO MCP Serverです。既定ではすべてのロールがllama.cppを直接呼び出します。

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

Piは別途インストールし、Pi自身の `models.json` でprovider/modelとllama.cpp接続を設定してください。llama.cpp Serverの起動・停止は本Serverでは管理しません。`MAC_PI_EXECUTABLE` が見つからなければ明示的なエラーになります。RPC追加引数は空白区切りです（引用符を解釈するshell parserではありません）。

Pi実行ごとにGit `HEAD` から安全な内部IDの一時branchとOS temp directory内のworktreeを作り、Piのworking directoryをそこに固定します。差分（untracked fileを含むbinary patch）を回収してからworktreeとbranchを削除します。本体working treeがdirtyでも実行できますが、その未コミット変更はPiには渡らず、応答に警告が入ります。

Piの最終文章はモデル報告として明示し、実変更はGit patchを正とします。timeoutや異常終了でも回収できたpatch、changed files、tool診断、warningsを `status=error` の応答に保持します。tool診断とstderrはサイズを制限して保持します。

Git worktreeは編集競合を避けるための隔離であり、OSレベルのsandboxではありません。PiはServer実行ユーザーの権限を持つため、信頼できるPi設定・モデル・プロジェクトでのみ利用してください。

Pi RPCの仕様は [Pi公式RPC documentation](https://pi.dev/docs/latest/rpc) を参照してください。
