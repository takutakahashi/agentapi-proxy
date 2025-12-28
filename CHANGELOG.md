# Changelog

## [v2.0.2](https://github.com/takutakahashi/agentapi-proxy/compare/v2.0.1...v2.0.2) - 2025-12-28
- refactor: separate proxy routing and handlers for better modularity by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/295
- Remove session persistence functionality by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/297
- feat: add Helm chart development build workflow by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/300
- refactor: replace init containers with fsGroup for permission management by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/299
- feat: restore session management APIs without persistence by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/301
- fix: improve GitHub App installation ID selection logic by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/302
- refactor: runAgentAPIServerを疎結合に変更 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/298
- fix: テスト実行時の環境変数干渉を修正 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/303
- refactor: ServerRunner インターフェースを整理 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/304
- refactor: SessionManager インターフェースを導入し AgentSession を廃止 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/305
- feat: KubernetesSessionManager を実装 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/306
- Update Kubernetes session health check configuration by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/307
- feat: KubernetesSession に Claude Credential 転送機能を追加 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/308
- feat: KubernetesSessionManager に claude.json 設置機能を追加 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/309
- feat: credentials.json をファイルとして Secret からマウントするように変更 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/310
- feat: Kubernetes セッションでリポジトリクローンを行う initContainer を追加 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/311
- feat: Kubernetes セッションで notification subscription を Secret にマウント by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/315
- feat: Session Pod に credentials 同期用サイドカーコンテナを追加 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/316
- feat: KubernetesSessionManager のセッションリストを Service ベースに変更 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/318
- refactor: セッションごとの credentials Secret を廃止 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/320
- feat: チーム別・ユーザー別の認証情報 Secret を Session Pod に自動マウント by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/317
- feat: Kubernetes セッションモード時に myclaudesPersistence を自動無効化 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/322
- feat: Kubernetes Session Pod に nodeSelector と tolerations を設定可能に by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/323
- feat: Kubernetes Session Pod の PVC 作成を選択可能に by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/324
- fix: Kubernetes Session Pod に CLAUDE.md をコピーする処理を追加 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/326
- feat: GitHub App PEM ファイルを emptyDir で共有し GITHUB_APP_PEM_PATH を設定 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/325
- feat: Kubernetes セッションで初期メッセージを送信する機能を追加 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/327
- feat: Kubernetes Session で MCP サーバー設定を Secret から読み込む機能を追加 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/328
- fix: Kubernetes Session で notifications ディレクトリを書き込み可能に修正 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/329
- feat: Settings API for Bedrock configuration (Kubernetes mode) by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/330
- chore: bump agentapi version from v0.11.2 to v0.11.6 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/333
- feat: use 'claude -c || claude' pattern for session resume fallback by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/334
- feat: add update verb to secrets resource in Role by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/336
- feat: add /user/info API endpoint by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/337
- feat: use Deployment instead of StatefulSet for kubernetesSession mode by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/338
- fix: remove AWS credentials from GET settings API response by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/340
- feat: sync settings to credentials secret for secure env injection by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/339
- fix: use correct env var CLAUDE_CODE_USE_BEDROCK=1 for Bedrock enablement by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/341
- feat: remove region setting from Bedrock settings API by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/342
- fix: handle empty notification-subscriptions-source directory gracefully by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/343
- fix: preserve existing credentials when Settings API receives empty values by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/344
- feat: add wildcard pattern support for team authorization by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/346
- feat: implement sidecar-based initial message sender for kubernetes sessions by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/348
- refactor: simplify initial message to use only params.message by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/350
- feat: support params.github_token for direct GitHub token authentication by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/351
- feat(helm): add repo scope to default OAuth scope by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/352
- fix: delete github-token secret when session is deleted by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/353
- fix: skip gh auth login when GITHUB_TOKEN is already set by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/355
- fix: do not mount github-session secret when params.github_token is provided by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/354
- fix: GHES auth setup-git requires explicit hostname and gh auth login by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/356
- feat: add schedule worker for delayed and recurring session start by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/357
- fix: Add nil checks for scheduleWorker and kubernetesSession in templates by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/358
- fix: Register schedule handlers in router to fix 404 error by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/359
- feat: Add pods/log permission to Helm role by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/360
- feat: separate session ServiceAccount and Role for security by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/361
- feat: Add schedule worker environment variables to Helm deployment by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/362
- fix: Use ServiceAccount from config for session pods by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/363
- feat: Add description field to session list API response by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/364
- feat: Delete previous session when schedule starts by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/365
- fix: update credentials-sync sidecar to work without secrets:get permission by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/367
- fix: Hardcode session ServiceAccount name to agentapi-proxy-session by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/369
- feat: Add pods/log permission to session ServiceAccount by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/370
- refactor: Use create-first then replace approach for credentials-sync by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/371
- feat: Add MCP servers configuration to Settings API by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/372
- docs: Unify OpenAPI specs and add Settings API documentation by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/373
- fix: Add -L flag to find command for notification file copy by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/375
- fix: Use correct MCPSecretPrefix to match Helm values.yaml by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/374
- feat: Add tagpr for automated release management by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/376
- chore: Remove template and validate steps from helm-dev-build by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/379
- fix: Extract repository info from tags when creating sessions via schedule by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/378
- feat: Embed GITHUB_TOKEN in Claude Code settings.json by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/377
- feat: Hardcode MCP secret naming convention by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/381

## [v2.0.1](https://github.com/takutakahashi/agentapi-proxy/compare/v2.0.0...v2.0.1) - 2025-12-06
- Fix legacy API authentication requirements by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/292

## [v2.0.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.75.0...v2.0.0) - 2025-12-06
- feat: Add MockAgentService for testing with environment variable configuration by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/272
- feat: Refactor codebase to clean architecture pattern by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/271
- feat: プロビジョンモード (Provision Mode) - Kubernetes StatefulSet による エージェント管理 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/275
- Kubernetes StatefulSet with Clean Architecture by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/273
- feat: K8sモード用envtestテストの実装 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/276
- remove: 未使用のLoadConfigLegacy関数を削除 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/277
- feat: Add user-specific ConfigMap and Secret support to k8s mode by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/279
- Update Go version to 1.25 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/281
- Add OpenAPI specification for agentapi-proxy by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/283
- Reorganize controller routes to match OpenAPI specification by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/284
- feat: k8s mode での通知設定と Secret 管理の改善 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/280
- refactor: use internal health controller implementation by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/287
- Remove session persistence functionality by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/288
- Refactor session controller to use Echo framework by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/289
- Add API v1 endpoints for notification and session proxy by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/291

## [v1.20250724.1](https://github.com/takutakahashi/agentapi-proxy/compare/v1.65.0...v1.20250724.1) - 2025-07-24
- プッシュ通知のサブスクリプション管理を改善 by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/256

## [v1.125.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.124.0...v1.125.0) - 2025-12-28
- feat: Add tagpr for automated release management by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/376
- chore: Remove template and validate steps from helm-dev-build by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/379
- fix: Extract repository info from tags when creating sessions via schedule by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/378
- feat: Embed GITHUB_TOKEN in Claude Code settings.json by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/377

## [v1.124.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.123.0...v1.124.0) - 2025-12-28
- feat: Add MCP servers configuration to Settings API by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/372
- docs: Unify OpenAPI specs and add Settings API documentation by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/373
- fix: Add -L flag to find command for notification file copy by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/375
- fix: Use correct MCPSecretPrefix to match Helm values.yaml by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/374

## [v1.123.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.122.0...v1.123.0) - 2025-12-27
- feat: Add pods/log permission to session ServiceAccount by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/370
- refactor: Use create-first then replace approach for credentials-sync by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/371

## [v1.122.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.121.0...v1.122.0) - 2025-12-27
- fix: update credentials-sync sidecar to work without secrets:get permission by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/367
- fix: Hardcode session ServiceAccount name to agentapi-proxy-session by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/369

## [v1.121.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.120.0...v1.121.0) - 2025-12-27
- feat: Delete previous session when schedule starts by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/365

## [v1.120.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.119.0...v1.120.0) - 2025-12-27
- fix: Use ServiceAccount from config for session pods by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/363
- feat: Add description field to session list API response by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/364

## [v1.119.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.118.0...v1.119.0) - 2025-12-26
- feat: separate session ServiceAccount and Role for security by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/361
- feat: Add schedule worker environment variables to Helm deployment by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/362

## [v1.118.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.117.0...v1.118.0) - 2025-12-26
- feat: Add pods/log permission to Helm role by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/360

## [v1.117.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.116.0...v1.117.0) - 2025-12-26
- fix: Register schedule handlers in router to fix 404 error by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/359

## [v1.116.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.115.0...v1.116.0) - 2025-12-26
- fix: Add nil checks for scheduleWorker and kubernetesSession in templates by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/358

## [v1.115.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.114.0...v1.115.0) - 2025-12-25
- feat: add schedule worker for delayed and recurring session start by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/357

## [v1.114.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.113.0...v1.114.0) - 2025-12-24
- fix: delete github-token secret when session is deleted by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/353
- fix: skip gh auth login when GITHUB_TOKEN is already set by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/355
- fix: do not mount github-session secret when params.github_token is provided by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/354
- fix: GHES auth setup-git requires explicit hostname and gh auth login by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/356

## [v1.113.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.112.0...v1.113.0) - 2025-12-24
- feat(helm): add repo scope to default OAuth scope by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/352

## [v1.112.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.111.0...v1.112.0) - 2025-12-24
- feat: support params.github_token for direct GitHub token authentication by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/351

## [v1.111.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.110.0...v1.111.0) - 2025-12-23
- feat: implement sidecar-based initial message sender for kubernetes sessions by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/348
- refactor: simplify initial message to use only params.message by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/350

## [v1.110.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.109.0...v1.110.0) - 2025-12-23
- feat: add wildcard pattern support for team authorization by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/346

## [v1.109.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.108.0...v1.109.0) - 2025-12-23
- fix: preserve existing credentials when Settings API receives empty values by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/344

## [v1.108.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.107.0...v1.108.0) - 2025-12-23
- feat: remove region setting from Bedrock settings API by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/342
- fix: handle empty notification-subscriptions-source directory gracefully by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/343
