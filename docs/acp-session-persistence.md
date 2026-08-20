# ACP セッション永続化設計（Claude / Codex）

## 目的

`claude-acp` と `codex-acp` の Pod / プロセスを作り直しても、ACP の
`session/load` で同じ会話を再開できるようにする。ここで扱うのは会話状態であり、
workdir、認証情報、ユーザー設定の永続化は既存機能の責務とする。

この設計は 2026-08-03 時点の以下の実装を調査した結果に基づく。

- `@agentclientprotocol/claude-agent-acp`:
  [commit 08e0e7f](https://github.com/agentclientprotocol/claude-agent-acp/tree/08e0e7f14ee1e221626a436b54b46ca32f2fe242)
  （Claude Agent SDK `0.3.220`）
- `@agentclientprotocol/codex-acp`:
  [commit efa3789](https://github.com/agentclientprotocol/codex-acp/tree/efa3789c3909838590f2f7cf24682ec4a0e987e4)
  （`@openai/codex ^0.145.0`）
- Codex app-server:
  [commit 8922a78](https://github.com/openai/codex/tree/8922a784fe6aa80683fe97c2dcdfdc361478aa7f)

現状は両 ACP パッケージを `@latest` で起動するため、保存形式を無条件に固定契約とは
みなせない。実装時にはパッケージを pin し、adapter version を snapshot manifest に
記録する。

## ACP とローカル状態の関係

ACP の `sessionId` 自体に会話内容は入っていない。再開には次の 2 つが両方必要になる。

1. `acp-server` が保存する `{cwd}/.acp-session-id`
2. その ID から各 agent が検索する durable state

現在の `acp-server` は起動時に `.acp-session-id` を読み、agent が
`loadSession` capability を返した場合に `session/load` を呼ぶ。失敗するとログを出して
新規セッションへフォールバックする。そのため ID だけを残しても、agent 側の durable
state が消えれば引き継ぎにはならない。また、このフォールバックはデータ消失を見逃し
やすいため、引き継ぎを明示指定した場合は fail closed に変更する。

## Claude ACP の調査結果

### `session/load` の動作

Claude ACP の `loadSession` は次を行う。

1. Claude Agent SDK の `query` を `resume: <ACP sessionId>` 付きで生成する
2. SDK の `getSessionMessages(sessionId)` で履歴を読み、ACP client に replay する
3. transcript がなければ `Resource not found` を返す

ACP session ID と Claude SDK session ID は同じ UUID である。CWD は保存先を決める
project key に関与するため、復元前後で同じ正規化済み CWD を使う必要がある。

### 再開に必要な保存対象

必須:

- `{CLAUDE_CONFIG_DIR:-~/.claude}/projects/<project-key>/<sessionId>.jsonl`
- `{cwd}/.acp-session-id`

条件付きで必要:

- `{CLAUDE_CONFIG_DIR}/projects/<project-key>/<sessionId>/subagents/**/*.jsonl`
- 同じ場所の subagent metadata sidecar（存在する場合）

subagent transcript がなくても main session は再開できるが、subagent 履歴の完全な replay
や関連機能が欠ける。したがってディレクトリごと保存する。

再開だけには不要で、別管理にするもの:

- `~/.claude/.credentials.json`（既存 managed files / credentials）
- `~/.claude/settings.json`, `~/.claude.json`, plugins, skills（起動時 compile）
- project の `memory/`、todos、command history、cache（会話再開とは別機能）

Claude Agent SDK `0.3.220` には `SessionStore` があり、main transcript と subagent
subkey を外部 store に mirror / materialize できる。オブジェクトストレージ方式では、
ホーム全体を archive するよりこの API を使う方が保存境界が明確である。

注意点:

- `sessionStore` の mirror はローカル transcript 書き込み後に行われる。
- `listSubkeys` を実装しない store では resume 時に main transcript しか復元されない。
- file checkpointing の backup blob は `SessionStore` で mirror されないため、現バージョン
  では `enableFileCheckpointing` と併用できない。

## Codex ACP の調査結果

### `session/load` の動作

Codex ACP の `loadSession` は Codex app-server に次を送る。

1. `thread/resume { threadId: <ACP sessionId> }`
2. `thread/read { threadId, includeTurns: true }`
3. 得られた thread history を ACP client に replay する

ACP session ID と Codex thread ID は同じ UUID である。app-server の local thread store は
thread ID から rollout を検索し、それを initial history として再開する。

### 再開に必要な保存対象

必須:

- `${CODEX_HOME:-~/.codex}/sessions/YYYY/MM/DD/rollout-*-<threadId>.jsonl`
- `${CODEX_HOME:-~/.codex}/state_*.sqlite*`
- `${CODEX_HOME:-~/.codex}/session_index.jsonl`
- `{cwd}/.acp-session-id`

rollout JSONL が会話の canonical data である。一方、現行 app-server の開発環境での
実機検証では rollout 単体だと `thread/resume` が thread を解決できなかったため、
SQLite index（WAL/SHM を含む）と session index も同じ checkpoint に含める。

条件付きで必要:

- 子 agent も個別 thread として後から再開・参照する要件がある場合、その rollout
- archive 済み thread を対象にする場合は `archived_sessions/` 内の該当 rollout

再開だけには不要で、別管理にするもの:

- `~/.codex/auth.json`（既存 managed files / credentials）
- `~/.codex/config.toml`, hooks, skills（起動時 compile）
- `history.jsonl`, logs, cache, `state_*.sqlite`（履歴一覧や検索の補助状態）
- `memories/`（会話再開とは別機能）

Codex には Claude SDK の `SessionStore` に相当する、ACP package から設定可能な安定した
オブジェクトストレージ adapter は現時点でない。upstream には experimental
`ThreadStore` abstraction があるが public config は local / in-memory のみである。
従って最初のオブジェクトストレージ実装は rollout file を checkpoint する sidecar 方式
とする。

## 共通データモデル

proxy DB に次の metadata を持ち、blob 本体とは分離する。

```text
ACPContinuation
  id                 引き継ぎ用の不変 ID
  owner scope        user または team
  agent type         claude-acp | codex-acp
  acp session id     Claude session UUID / Codex thread UUID
  cwd identity       repository + clone path の論理 ID
  backend            volume | object
  object/version     volume path または object generation
  adapter version    npm package version + state format version
  checkpoint status  pending | ready | corrupt
  checksum/size
  created/updated/last-restored timestamps
```

主経路は同一 proxy session ID の Pod 再作成である。provisioner は永続化が有効なら毎回
自分の proxy session ID を snapshot key として検索し、初回起動の 404 は新規 session、
snapshot があれば状態を配置してから `session/load` する。`resume_from` は別 proxy session
から明示的に移行する副次的な API とし、その場合の 404 / load failure は復元失敗として返す。

同一 continuation への同時 writer は禁止し lease を取る。分岐したい場合は各 ACP の
fork API を使って新しい continuation ID を発行する。

## パターン A: ボリューム永続化

### 配置

セッション Pod に user / team scope の RWX PVC を `/session-state` として mount する。

```text
/session-state/<scope>/<owner>/<continuation-id>/
  manifest.json
  acp-session-id
  claude/projects/<project-key>/<sessionId>.jsonl
  claude/projects/<project-key>/<sessionId>/...
  codex/sessions/YYYY/MM/DD/rollout-...-<threadId>.jsonl
```

agent の通常ホームを丸ごと PVC にしない。init container が manifest の allowlist だけを
runtime home に materialize し、agent 終了時または checkpoint 時に atomic rename で
volume 側へ反映する。これにより credentials / generated config との上書き競合を避ける。

Claude は transcript を直接 volume 配下へ置く専用 `CLAUDE_CONFIG_DIR` も選べるが、設定や
credentials まで同じ root に入るため、MVP では allowlist copy を共通方式とする。

### checkpoint

- turn 完了時（bridge が prompt 完了を観測した直後）
- 30 秒ごとの dirty check（生成中の Pod loss 対策）
- SIGTERM / preStop（best effort）

JSONL の途中行を公開しないよう、copy -> fsync -> manifest generation 更新の順に commit
する。復元は `ready` generation のみを読む。

### turn 後の自動 suspend / resume

checkpoint には ACP の会話状態に加えて Git workspace の HEAD commit、checkout 中の
branch（detached HEAD を含む）、staged / unstaged の差分、無視対象を除く untracked file を
保存する。復元時は同じ commit と branch を checkout してから差分を適用する。

永続化が有効な ACP session は Stop hook で checkpoint が完了した後、既定で 1 時間後の
suspend を予約する。期限は session の canonical Kubernetes Service の annotation に保存
されるため、proxy の再起動や replica の切り替えでも失われない。期限到来時には
Deployment / Pod だけを削除し、Service、settings Secret、PVC、snapshot は保持する。

停止中の session を開くと既存の lazy workload restore が同じ proxy session ID で workload
を再作成し、snapshot から ACP session を復元する。猶予時間は
`session_persistence.suspend_after`（環境変数
`AGENTAPI_SESSION_PERSISTENCE_SUSPEND_AFTER`）で変更でき、`0` で無効化できる。期限時点で
新しい turn が実行中なら suspend は延期され、その turn の Stop hook で期限が再設定される。

### 特性

- 長所: 実装が単純、追記が安価、復元が速い
- 短所: RWX storage が必要、cluster / region を越えにくい、PVC 障害ドメインに依存

## パターン B: オブジェクトストレージ永続化

### object layout

```text
acp-sessions/<scope>/<owner>/<continuation-id>/
  generations/<generation>/manifest.json
  generations/<generation>/claude/<project-key>/<sessionId>.jsonl.zst
  generations/<generation>/claude/<project-key>/<sessionId>/...jsonl.zst
  generations/<generation>/codex/<threadId>/rollout.jsonl.zst
  current.json
```

`current.json` は全 blob の upload と checksum 検証後に更新する commit marker である。
bucket versioning と server-side encryption を有効化する。

### Claude

Claude の Stop hook が `agentapi-proxy client backup-session-state` を呼ぶ。CLI は main / subagent
transcript と `.acp-session-id` だけを archive にし、provisioner token で backend の内部 API
へ送る。S3 credentials は session Pod に渡さず、backend が Garage へ upload する。

### Codex

Codex の Stop hook も同じ CLI / backend API を使う。CLI が対象 rollout、SQLite index、
session index、`.acp-session-id` を gzip archive にし、backend が Garage へ upload する。
復元時はすべて元の `CODEX_HOME` 配下へ戻す。

Codex rollout は実行中に追記されるため、turn-end checkpoint を主 commit point とする。
途中 checkpoint は改行で完結した JSONL record までを snapshot に含める。

### 特性

- 長所: cluster / region 非依存、高い耐久性、lifecycle / versioning を使える
- 短所: Claude と Codex で adapter が異なる、転送遅延と request cost、整合性制御が必要

## 推奨する実装順

1. ACP package を pin し、`.acp-session-id` の保存場所を continuation state 配下へ変更する
2. volume backend と共通 manifest / lease / strict restore を実装する
3. 同一 proxy session ID の Pod を強制削除・再作成する E2E を Claude / Codex 各 1 本追加する
4. object backend を追加する（Claude `SessionStore`、Codex rollout checkpointer）
5. forced Pod deletion、途中 JSONL、checksum mismatch、異なる CWD / agent type を試験する

受け入れ条件は「履歴が UI に replay される」だけでなく、再起動後の追加 prompt が再起動前
の固有 token を回答でき、同じ ACP session ID の transcript / rollout に追記されることとする。
