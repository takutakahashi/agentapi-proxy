# Changelog

## [v1.126.1](https://github.com/takutakahashi/agentapi-proxy/compare/v1.126.0...v1.126.1) - 2025-12-28
- feat: Add session resume fallback to Kubernetes mode by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/383

## [v1.126.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.125.0...v1.126.0) - 2025-12-28
- feat: Hardcode MCP secret naming convention by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/381

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

## [v1.107.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.106.0...v1.107.0) - 2025-12-23
- fix: use correct env var CLAUDE_CODE_USE_BEDROCK=1 for Bedrock enablement by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/341

## [v1.106.0](https://github.com/takutakahashi/agentapi-proxy/compare/v1.105.0...v1.106.0) - 2025-12-23
- feat: sync settings to credentials secret for secure env injection by @takutakahashi in https://github.com/takutakahashi/agentapi-proxy/pull/339
