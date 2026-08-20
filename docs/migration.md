# Monorepo migration

## Source repositories

初回取り込み元は以下です。

- backend: `takutakahashi/agentapi-proxy@59d37797a96491a000b5d3b65e98744aea8fb024`
- frontend: `takutakahashi/agentapi-ui@caf2a1f0383c29cded727175548205d566988bbd`

どちらも`git subtree add`で履歴を保ったまま取り込んでいます。

## Source of truth

移行後は`ccplant/ccplant`を唯一の開発元とします。旧リポジトリへの直接変更は
原則行わず、修正は先にモノレポへ入れます。

## Backport

`Backport components` workflowを手動実行すると、対象ディレクトリを
`git subtree split`で切り出し、旧リポジトリへ同期PRを作成します。

- `backend/` → `takutakahashi/agentapi-proxy`
- `frontend/` → `takutakahashi/agentapi-ui`

workflowには旧リポジトリへpushおよびPR作成できる
`BACKPORT_TOKEN` secretが必要です。

## Release

単一の`vX.Y.Z`タグからbackend/frontendのコンテナ、Goバイナリ、および
旧リポジトリ互換のvalues/templatesを持つ`backend`/`frontend` Helm Chartを同じ
`vX.Y.Z`バージョンで個別に発行します。統合`ccplant` Helm Chartも
`X.Y.Z`バージョンで同時に発行します。
