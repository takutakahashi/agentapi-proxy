# pkg/proxy クリーンアーキテクチャ責務分類

## クリーンアーキテクチャの層

```
┌─────────────────────────────────────────────────────────────┐
│                    Frameworks & Drivers                      │
│  (pkg/proxy: HTTP Server, Router, Kubernetes Client)        │
├─────────────────────────────────────────────────────────────┤
│                   Interface Adapters                         │
│  (Controllers/Handlers, Presenters, Gateways)               │
├─────────────────────────────────────────────────────────────┤
│                      Use Cases                               │
│  (Application Business Rules)                                │
├─────────────────────────────────────────────────────────────┤
│                       Entities                               │
│  (Enterprise Business Rules)                                 │
└─────────────────────────────────────────────────────────────┘
```

---

## pkg/proxy ファイル分類

### 1. Entities（ドメイン層）→ `internal/domain/entities/`

**責務**: ビジネスルールを表現する純粋なデータ構造

| ファイル | 内容 | 移行先 | 状態 |
|---------|------|--------|------|
| `types.go` | StartRequest, SessionParams, RunServerRequest | `internal/domain/entities/session.go` | ✅ 完了 |
| `session.go` | Session interface, SessionFilter, ResourceScope | `internal/domain/entities/session.go` | ✅ 完了 |
| `share.go` | SessionShare | `internal/domain/entities/share.go` | ✅ 完了 |

---

### 2. Use Cases（ユースケース層）→ `internal/usecases/`

**責務**: アプリケーション固有のビジネスルール

| ユースケース | 責務 | 移行先 | 状態 |
|-------------|------|--------|------|
| CreateSession | セッション作成 | `internal/usecases/session/` | ✅ 完了 |
| ListSessions | セッション一覧取得 | `internal/usecases/session/` | ✅ 完了 |
| DeleteSession | セッション削除 | `internal/usecases/session/` | ✅ 完了 |
| CreateShare | 共有リンク作成 | `internal/usecases/share/` | ✅ 完了 |
| GetShare | 共有リンク取得 | `internal/usecases/share/` | ✅ 完了 |
| DeleteShare | 共有リンク削除 | `internal/usecases/share/` | ✅ 完了 |

---

### 3. Interface Adapters（インターフェースアダプタ層）

#### 3.1 Controllers/Handlers → `internal/interfaces/controllers/`

**責務**: HTTPリクエストをユースケースに変換

| ファイル | 責務 | 移行先 | 状態 |
|---------|------|--------|------|
| `session_handlers.go` | セッションCRUD API | `internal/interfaces/controllers/session_controller.go` | 📋 未着手 |
| `share_handlers.go` | 共有リンクAPI | `internal/interfaces/controllers/share_controller.go` | 📋 未着手 |
| `settings_handlers.go` | 設定管理API | `internal/interfaces/controllers/settings_controller.go` | 📋 未着手 |
| `oauth_handlers.go` | OAuth認証フロー | `internal/interfaces/controllers/oauth_controller.go` | 📋 未着手 |
| `notification_handlers_new.go` | 通知API | `internal/interfaces/controllers/notification_controller.go` | ✅ 完了 |
| `auth_info_handlers.go` | 認証情報API | `internal/interfaces/controllers/auth_info_controller.go` | 📋 未着手 |
| `user_handlers.go` | ユーザー情報API | `internal/interfaces/controllers/user_controller.go` | 📋 未着手 |
| `health_handlers.go` | ヘルスチェック | `internal/interfaces/controllers/health_controller.go` | 📋 未着手 |

#### 3.2 Gateways/Repositories → `internal/infrastructure/repositories/`

**責務**: 外部データソースへのアクセス

| ファイル | 責務 | 移行先 | 状態 |
|---------|------|--------|------|
| `share_repository.go` | 共有リンク永続化 | `internal/infrastructure/repositories/kubernetes_share_repository.go` | ✅ 完了 |

---

### 4. Infrastructure（インフラストラクチャ層）→ `internal/infrastructure/`

#### 4.1 Services → `internal/infrastructure/services/`

**責務**: 外部サービスとの連携、技術的な実装詳細

| ファイル | 責務 | 移行先 | 状態 |
|---------|------|--------|------|
| `kubernetes_session_manager.go` | K8sセッション管理 | `internal/infrastructure/services/kubernetes_session_manager.go` | 📋 未着手 |
| `kubernetes_session.go` | K8sセッション実装 | `internal/infrastructure/services/kubernetes_session.go` | ✅ 完了 |
| `credential_provider*.go` | 認証情報プロバイダー | `internal/infrastructure/services/credential_provider*.go` | ✅ 完了 |
| `env_merge.go` | 環境変数マージ | `internal/infrastructure/services/env_merge.go` | ✅ 完了 |
| `subscription_secret_syncer_k8s.go` | Secret同期 | `internal/infrastructure/services/` | 📋 未着手 |

---

### 5. Frameworks & Drivers（フレームワーク層）→ `pkg/proxy/` に残す

**責務**: HTTPサーバー、ルーティング、起動処理

| ファイル | 責務 | 備考 |
|---------|------|------|
| `proxy.go` | HTTPサーバー初期化、ミドルウェア設定 | そのまま維持 |
| `router.go` | ルート登録、ルーティング設定 | そのまま維持 |
| `startup.go` | 起動処理、依存関係初期化 | そのまま維持 |

---

## ポート（インターフェース）定義 → `internal/usecases/ports/`

**責務**: 依存関係逆転のためのインターフェース定義

| ファイル | 責務 | 状態 |
|---------|------|------|
| `repositories/session_repository.go` | SessionManager interface | ✅ 完了 |
| `repositories/share_repository.go` | ShareRepository interface | ✅ 完了 |
| `repositories/settings_repository.go` | SettingsRepository interface | ✅ 完了 |
| `services/credentials_secret_syncer.go` | CredentialsSecretSyncer interface | ✅ 完了 |

---

## 依存関係の方向

```
pkg/proxy (Frameworks)
    ↓
internal/interfaces/controllers (Interface Adapters)
    ↓
internal/usecases/* (Use Cases)
    ↓
internal/domain/entities (Entities)
    ↑
internal/usecases/ports/* (Ports - interfaces)
    ↑
internal/infrastructure/* (Infrastructure - implements ports)
```

---

## 後方互換性の維持

`pkg/proxy/` のファイルは型エイリアスとして残し、外部パッケージからの参照を維持：

```go
// pkg/proxy/session.go
package proxy

import "github.com/takutakahashi/agentapi-proxy/internal/domain/entities"

type Session = entities.Session
type SessionFilter = entities.SessionFilter
```

---

## 完了済みリファクタリング

1. ✅ **Entities層**: Session, Share, Types を `internal/domain/entities/` に移動
2. ✅ **Ports層**: SessionManager, ShareRepository インターフェースを定義
3. ✅ **Use Cases層**: session/, share/ ユースケースを作成
4. ✅ **Infrastructure層**: credential_provider, env_merge, kubernetes_session を移動
5. ✅ **pkg/proxy 型エイリアス**: 後方互換性のための型エイリアスを設定

## 未着手

1. 📋 **Handlers → Controllers**: session_handlers.go 等をコントローラー層に移動
2. 📋 **kubernetes_session_manager.go**: 巨大ファイルの整理（責務は Infrastructure 層）
3. 📋 **DI Container**: 新しい依存関係の統合
