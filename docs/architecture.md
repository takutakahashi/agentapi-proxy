# agentapi-proxy アーキテクチャドキュメント

このドキュメントでは、agentapi-proxy のパッケージ構造、インターフェース、依存関係について図示します。

## 目次

- [1. レイヤーアーキテクチャ概要](#1-レイヤーアーキテクチャ概要)
- [2. パッケージ構造](#2-パッケージ構造)
- [3. ドメインエンティティ関係図](#3-ドメインエンティティ関係図)
- [4. インターフェース/ポート依存関係](#4-インターフェースポート依存関係)
- [5. DIコンテナの依存関係](#5-diコンテナの依存関係)
- [6. 主要なフロー図](#6-主要なフロー図)

---

## 1. レイヤーアーキテクチャ概要

agentapi-proxy は **Clean Architecture (クリーンアーキテクチャ)** と **Hexagonal Architecture (ヘキサゴナルアーキテクチャ)** のパターンに基づいて設計されています。

```mermaid
graph TB
    subgraph "External"
        HTTP[HTTP Requests]
        K8S[Kubernetes]
        GitHub[GitHub API]
        AWS[AWS Services]
    end

    subgraph "Application Layer"
        Controllers[Controllers<br/>interfaces/controllers/]
        Presenters[Presenters<br/>interfaces/presenters/]
    end

    subgraph "Use Case Layer"
        UseCases[Use Cases<br/>usecases/]
        Ports[Ports<br/>usecases/ports/]
    end

    subgraph "Domain Layer"
        Entities[Entities<br/>domain/entities/]
        DomainServices[Domain Services<br/>domain/services/]
    end

    subgraph "Infrastructure Layer"
        Repositories[Repository Implementations<br/>infrastructure/repositories/]
        Services[Service Implementations<br/>infrastructure/services/]
    end

    subgraph "DI"
        Container[DI Container<br/>di/]
    end

    HTTP --> Controllers
    Controllers --> UseCases
    UseCases --> Ports
    UseCases --> DomainServices
    Ports --> Repositories
    Ports --> Services
    DomainServices --> Entities
    Repositories --> K8S
    Services --> K8S
    Services --> GitHub
    Services --> AWS
    Controllers --> Presenters
    Container -.->|Inject| Controllers
    Container -.->|Inject| UseCases
    Container -.->|Inject| Repositories
    Container -.->|Inject| Services

    style Domain Layer fill:#e1f5ff
    style Use Case Layer fill:#fff4e1
    style Application Layer fill:#ffe1f5
    style Infrastructure Layer fill:#e1ffe1
    style DI fill:#f5e1ff
```

### アーキテクチャの特徴

1. **関心の分離**: 各レイヤーは明確な責務を持つ
2. **依存性の反転**: 上位レイヤーは下位レイヤーに依存しない（インターフェースを通じて依存）
3. **テスタビリティ**: インターフェースによりモックが容易
4. **拡張性**: 新しい実装を追加してもコアロジックは変更不要

---

## 2. パッケージ構造

```mermaid
graph LR
    subgraph "cmd/"
        CLI[CLI Commands<br/>Cobra Framework]
    end

    subgraph "internal/"
        subgraph "app/"
            Server[Server<br/>Echo HTTP Server]
            Router[Router<br/>Route Registration]
            Auth[Auth Middleware]
        end

        subgraph "di/"
            DIContainer[DI Container]
        end

        subgraph "domain/"
            DomainEntities[entities/]
            DomainServices2[services/]
        end

        subgraph "usecases/"
            UseCasesPorts[ports/]
            AuthUC[auth/]
            NotificationUC[notification/]
            SessionUC[session/]
            ShareUC[share/]
        end

        subgraph "infrastructure/"
            InfraRepos[repositories/]
            InfraServices[services/]
            Webhook[webhook/]
        end

        subgraph "interfaces/"
            InterfacesControllers[controllers/]
            InterfacesPresenters[presenters/]
        end
    end

    subgraph "pkg/"
        PkgAuth[auth/]
        PkgConfig[config/]
        PkgClient[client/]
        PkgGitHub[github/]
        PkgMCP[mcp/]
        PkgNotification[notification/]
        PkgUtils[utils/]
    end

    CLI --> Server
    Server --> Router
    Router --> DIContainer
    DIContainer --> InterfacesControllers
    InterfacesControllers --> UseCasesPorts
    UseCasesPorts --> DomainEntities
    UseCasesPorts --> DomainServices2
    InfraRepos --> UseCasesPorts
    InfraServices --> UseCasesPorts
    InterfacesControllers --> InterfacesPresenters

    style domain/ fill:#e1f5ff
    style usecases/ fill:#fff4e1
    style interfaces/ fill:#ffe1f5
    style infrastructure/ fill:#e1ffe1
    style di/ fill:#f5e1ff
```

### パッケージの役割

| パッケージ | 役割 |
|-----------|------|
| `cmd/` | CLI エントリーポイント（Cobra コマンド） |
| `internal/app/` | HTTP サーバーとルーティング |
| `internal/di/` | 依存性注入コンテナ |
| `internal/domain/` | ビジネスロジックとエンティティ |
| `internal/usecases/` | ユースケース層とポート定義 |
| `internal/infrastructure/` | インフラ実装（リポジトリ、サービス） |
| `internal/interfaces/` | API コントローラーとプレゼンター |
| `pkg/` | 公開ユーティリティ |

---

## 3. ドメインエンティティ関係図

```mermaid
classDiagram
    class User {
        +UserID id
        +string username
        +string email
        +UserType userType
        +[]Role roles
        +[]Permission permissions
        +string teamID
        +HasPermission(permission) bool
        +IsAdmin() bool
        +CanAccessResource(ownerID, scope, teamID) bool
    }

    class Session {
        +SessionID id
        +string addr
        +UserID userID
        +string scope
        +string teamID
        +SessionStatus status
        +time.Time createdAt
        +ID() SessionID
        +Status() SessionStatus
    }

    class Settings {
        +string name
        +Bedrock bedrock
        +[]MCPServer mcpServers
        +[]Marketplace marketplaces
        +string claudeCodeOAuthToken
        +string scope
        +string teamID
        +Bedrock() *Bedrock
        +MCPServers() []MCPServer
    }

    class Notification {
        +NotificationID id
        +UserID userID
        +string title
        +string message
        +time.Time createdAt
        +ID() NotificationID
        +UserID() UserID
    }

    class Webhook {
        +WebhookID id
        +UserID userID
        +string repositoryURL
        +string scope
        +string teamID
        +time.Time createdAt
        +RepositoryURL() string
    }

    class SessionShare {
        +SessionID sessionID
        +string token
        +time.Time expiresAt
        +string scope
        +string teamID
        +SessionID() SessionID
        +Token() string
    }

    class MCPServer {
        +string name
        +string type
        +string url
        +string command
        +[]string args
        +map env
        +Name() string
        +Type() string
    }

    class Marketplace {
        +string name
        +string url
        +string authToken
        +Name() string
        +URL() string
    }

    class Repository {
        +string owner
        +string name
        +string url
        +Owner() string
        +Name() string
    }

    User "1" -- "0..*" Session : owns
    User "1" -- "0..*" Notification : receives
    User "1" -- "0..*" Webhook : configures
    User "1" -- "0..1" Settings : has
    Session "1" -- "0..*" SessionShare : shared via
    Settings "1" -- "0..*" MCPServer : contains
    Settings "1" -- "0..*" Marketplace : references
    Webhook "1" -- "1" Repository : monitors

    note for User "UserType: api_key, github, aws, regular, admin\nRoles: Admin, User, Member, Developer, ReadOnly\nPermissions: session:create, session:read, etc."
    note for Session "SessionStatus: pending, running, stopped, failed\nScope: user, team"
```

### エンティティの責務

| エンティティ | 責務 |
|-------------|------|
| **User** | ユーザー認証情報、権限管理、リソースアクセス制御 |
| **Session** | agentapi セッションのライフサイクル管理 |
| **Settings** | ユーザー/チーム設定（Bedrock、MCP、Marketplace） |
| **Notification** | プッシュ通知データ |
| **Webhook** | GitHub Webhook 設定 |
| **SessionShare** | セッション共有トークン管理 |
| **MCPServer** | Model Context Protocol サーバー設定 |
| **Marketplace** | プラグインマーケットプレイス設定 |
| **Repository** | Git リポジトリ情報 |

---

## 4. インターフェース/ポート依存関係

### 4.1 リポジトリポート

```mermaid
graph TB
    subgraph "Repository Ports (usecases/ports/repositories/)"
        UserRepo[UserRepository]
        SettingsRepo[SettingsRepository]
        SessionMgr[SessionManager]
        NotificationRepo[NotificationRepository]
        WebhookRepo[WebhookRepository]
        ShareRepo[ShareRepository]
    end

    subgraph "Repository Implementations (infrastructure/repositories/)"
        K8sSettings[KubernetesSettingsRepository]
        K8sWebhook[KubernetesWebhookRepository]
        K8sShare[KubernetesShareRepository]
        MemUser[MemoryUserRepository]
        MemNotification[MemoryNotificationRepository]
    end

    subgraph "Session Manager Implementations (infrastructure/services/)"
        K8sSession[KubernetesSessionManager]
        LocalSession[LocalSessionManager]
    end

    UserRepo -.->|implements| MemUser
    SettingsRepo -.->|implements| K8sSettings
    SessionMgr -.->|implements| K8sSession
    SessionMgr -.->|implements| LocalSession
    NotificationRepo -.->|implements| MemNotification
    WebhookRepo -.->|implements| K8sWebhook
    ShareRepo -.->|implements| K8sShare

    K8sSettings --> K8S[Kubernetes API<br/>ConfigMaps/Secrets]
    K8sWebhook --> K8S
    K8sShare --> K8S
    K8sSession --> K8S
    MemUser --> Memory[(In-Memory)]
    MemNotification --> Memory

    style UserRepo fill:#fff4e1
    style SettingsRepo fill:#fff4e1
    style SessionMgr fill:#fff4e1
    style NotificationRepo fill:#fff4e1
    style WebhookRepo fill:#fff4e1
    style ShareRepo fill:#fff4e1
```

### 4.2 サービスポート

```mermaid
graph TB
    subgraph "Service Ports (usecases/ports/services/)"
        AuthSvc[AuthService]
        GitHubAuthSvc[GitHubAuthService]
        NotificationSvc[NotificationService]
        ProxySvc[ProxyService]
        EncryptionSvc[EncryptionService]
        CredSyncer[CredentialsSecretSyncer]
        MCPSyncer[MCPSecretSyncer]
        MarketplaceSyncer[MarketplaceSecretSyncer]
    end

    subgraph "Service Implementations (infrastructure/services/)"
        direction LR
        subgraph "Encryption Strategies"
            NoopEnc[NoopEncryptionService]
            LocalEnc[LocalEncryptionService<br/>AES-256-GCM]
            KMSEnc[KMSEncryptionService<br/>AWS KMS]
        end

        K8sCredSyncer[KubernetesCredentialsSecretSyncer]
        K8sMCPSyncer[KubernetesMCPSecretSyncer]
        K8sMarketplaceSyncer[KubernetesMarketplaceSecretSyncer]
    end

    EncryptionSvc -.->|implements| NoopEnc
    EncryptionSvc -.->|implements| LocalEnc
    EncryptionSvc -.->|implements| KMSEnc
    CredSyncer -.->|implements| K8sCredSyncer
    MCPSyncer -.->|implements| K8sMCPSyncer
    MarketplaceSyncer -.->|implements| K8sMarketplaceSyncer

    KMSEnc --> AWSKMS[AWS KMS]
    LocalEnc --> LocalKey[(Local Key)]
    K8sCredSyncer --> K8S2[Kubernetes<br/>Secrets]
    K8sMCPSyncer --> K8S2
    K8sMarketplaceSyncer --> K8S2

    style AuthSvc fill:#fff4e1
    style GitHubAuthSvc fill:#fff4e1
    style NotificationSvc fill:#fff4e1
    style ProxySvc fill:#fff4e1
    style EncryptionSvc fill:#fff4e1
```

### 4.3 ユースケース層

```mermaid
graph TB
    subgraph "Use Cases (usecases/)"
        AuthenticateUserUC[AuthenticateUserUseCase]
        ValidateAPIKeyUC[ValidateAPIKeyUseCase]
        GitHubAuthUC[GitHubAuthenticateUseCase]
        ValidatePermissionUC[ValidatePermissionUseCase]
        SendNotificationUC[SendNotificationUseCase]
        ManageSubscriptionUC[ManageSubscriptionUseCase]
        SessionUC[SessionUseCase]
        ShareUC[ShareUseCase]
    end

    subgraph "Ports"
        AuthSvc2[AuthService]
        GitHubAuthSvc2[GitHubAuthService]
        NotificationSvc2[NotificationService]
        UserRepo2[UserRepository]
        SessionMgr2[SessionManager]
        NotificationRepo2[NotificationRepository]
        ShareRepo2[ShareRepository]
    end

    AuthenticateUserUC --> AuthSvc2
    AuthenticateUserUC --> UserRepo2
    ValidateAPIKeyUC --> AuthSvc2
    GitHubAuthUC --> GitHubAuthSvc2
    GitHubAuthUC --> UserRepo2
    ValidatePermissionUC --> UserRepo2
    SendNotificationUC --> NotificationSvc2
    SendNotificationUC --> NotificationRepo2
    ManageSubscriptionUC --> NotificationRepo2
    SessionUC --> SessionMgr2
    ShareUC --> ShareRepo2
    ShareUC --> SessionMgr2

    style AuthenticateUserUC fill:#e1f5ff
    style ValidateAPIKeyUC fill:#e1f5ff
    style GitHubAuthUC fill:#e1f5ff
    style ValidatePermissionUC fill:#e1f5ff
    style SendNotificationUC fill:#e1f5ff
    style ManageSubscriptionUC fill:#e1f5ff
    style SessionUC fill:#e1f5ff
    style ShareUC fill:#e1f5ff
```

---

## 5. DIコンテナの依存関係

DI コンテナ (`internal/di/container.go`) はすべての依存関係を管理し、アプリケーション起動時に初期化します。

```mermaid
graph TB
    Container[DI Container]

    subgraph "Initialization Order"
        direction TB
        Step1[1. initRepositories]
        Step2[2. initServices]
        Step3[3. initUseCases]
        Step4[4. initPresenters]
        Step5[5. initControllers]
        Step6[6. seedData]

        Step1 --> Step2
        Step2 --> Step3
        Step3 --> Step4
        Step4 --> Step5
        Step5 --> Step6
    end

    Container --> Step1

    subgraph "Container Components"
        direction LR

        subgraph "Repositories"
            CUserRepo[UserRepo]
            CNotificationRepo[NotificationRepo]
            CSettingsRepo[SettingsRepo]
            CWebhookRepo[WebhookRepo]
            CShareRepo[ShareRepo]
        end

        subgraph "Services"
            CAuthService[AuthService]
            CNotificationService[NotificationService]
            CProxyService[ProxyService]
            CGitHubAuthService[GitHubAuthService]
            CEncryptionService[EncryptionService]
        end

        subgraph "Use Cases"
            CAuthenticateUserUC[AuthenticateUserUC]
            CValidateAPIKeyUC[ValidateAPIKeyUC]
            CGitHubAuthenticateUC[GitHubAuthenticateUC]
            CValidatePermissionUC[ValidatePermissionUC]
            CSendNotificationUC[SendNotificationUC]
            CManageSubscriptionUC[ManageSubscriptionUC]
        end

        subgraph "Controllers"
            CAuthController[AuthController]
            CNotificationController[NotificationController]
            CSessionController[SessionController]
            CSettingsController[SettingsController]
            CWebhookController[WebhookController]
            CShareController[ShareController]
        end

        subgraph "Middleware"
            CAuthMiddleware[AuthMiddleware]
        end
    end

    Step6 --> CUserRepo
    Step6 --> CNotificationRepo
    Step6 --> CAuthService
    Step6 --> CNotificationService
    Step6 --> CAuthenticateUserUC
    Step6 --> CAuthController
    Step6 --> CAuthMiddleware

    CAuthController --> CAuthenticateUserUC
    CAuthController --> CGitHubAuthenticateUC
    CAuthController --> CValidateAPIKeyUC
    CAuthMiddleware --> CValidatePermissionUC
    CNotificationController --> CSendNotificationUC
    CNotificationController --> CManageSubscriptionUC

    CAuthenticateUserUC --> CAuthService
    CAuthenticateUserUC --> CUserRepo
    CGitHubAuthenticateUC --> CGitHubAuthService
    CSendNotificationUC --> CNotificationService
    CSendNotificationUC --> CNotificationRepo

    style Container fill:#f5e1ff
    style Step1 fill:#e1ffe1
    style Step2 fill:#e1ffe1
    style Step3 fill:#e1ffe1
    style Step4 fill:#e1ffe1
    style Step5 fill:#e1ffe1
    style Step6 fill:#e1ffe1
```

### DI コンテナの責務

1. **依存関係の解決**: すべてのコンポーネントを正しい順序で初期化
2. **ライフサイクル管理**: シングルトンインスタンスの管理
3. **構成の集約**: アプリケーション全体の構成を一箇所で管理
4. **テスト容易性**: テスト時はモックに置き換え可能

---

## 6. 主要なフロー図

### 6.1 セッション作成フロー

```mermaid
sequenceDiagram
    participant Client
    participant Controller as SessionController
    participant UseCase as SessionUseCase
    participant SessionMgr as SessionManager
    participant K8s as Kubernetes API
    participant Syncer as CredentialsSecretSyncer

    Client->>Controller: POST /sessions/start
    activate Controller

    Controller->>Controller: Validate request
    Controller->>UseCase: CreateSession(ctx, req)
    activate UseCase

    UseCase->>Syncer: SyncCredentials(ctx, userID, scope, teamID)
    activate Syncer
    Syncer->>K8s: Create/Update Secret
    K8s-->>Syncer: Secret created
    deactivate Syncer

    UseCase->>SessionMgr: CreateSession(ctx, config)
    activate SessionMgr

    SessionMgr->>K8s: Create Pod
    K8s-->>SessionMgr: Pod created

    SessionMgr->>K8s: Watch Pod status
    K8s-->>SessionMgr: Pod running

    SessionMgr-->>UseCase: Session
    deactivate SessionMgr

    UseCase-->>Controller: Session
    deactivate UseCase

    Controller->>Controller: Format response
    Controller-->>Client: 200 OK (session data)
    deactivate Controller
```

### 6.2 設定更新フロー（暗号化付き）

```mermaid
sequenceDiagram
    participant Client
    participant Controller as SettingsController
    participant Repo as SettingsRepository<br/>(Kubernetes)
    participant EncRegistry as EncryptionServiceRegistry
    participant EncSvc as EncryptionService
    participant K8s as Kubernetes API

    Client->>Controller: PUT /settings/:name
    activate Controller

    Controller->>Controller: Parse request body
    Controller->>Repo: Save(ctx, settings)
    activate Repo

    Repo->>EncRegistry: SelectService(algorithm, keyID)
    activate EncRegistry
    EncRegistry-->>Repo: EncryptionService
    deactivate EncRegistry

    loop For each sensitive field
        Repo->>EncSvc: Encrypt(ctx, plaintext)
        activate EncSvc
        EncSvc-->>Repo: EncryptedData
        deactivate EncSvc
    end

    Repo->>K8s: Create/Update Secret
    activate K8s
    K8s-->>Repo: Secret saved
    deactivate K8s

    Repo-->>Controller: nil (success)
    deactivate Repo

    Controller-->>Client: 200 OK
    deactivate Controller
```

### 6.3 GitHub OAuth 認証フロー

```mermaid
sequenceDiagram
    participant User as User (Browser)
    participant Proxy as agentapi-proxy
    participant GitHub as GitHub OAuth
    participant Controller as AuthController
    participant UseCase as GitHubAuthenticateUseCase
    participant GitHubAuthSvc as GitHubAuthService
    participant UserRepo as UserRepository

    User->>Proxy: GET /auth/github
    activate Proxy
    Proxy->>GitHubAuthSvc: GenerateOAuthURL()
    GitHubAuthSvc-->>Proxy: OAuth URL
    Proxy-->>User: 302 Redirect to GitHub
    deactivate Proxy

    User->>GitHub: Authorize app
    GitHub-->>User: 302 Redirect to callback

    User->>Proxy: GET /auth/github/callback?code=xxx
    activate Proxy
    Proxy->>Controller: HandleCallback(code)
    activate Controller

    Controller->>UseCase: Authenticate(ctx, code)
    activate UseCase

    UseCase->>GitHubAuthSvc: ExchangeCodeForToken(ctx, code)
    activate GitHubAuthSvc
    GitHubAuthSvc->>GitHub: POST /login/oauth/access_token
    GitHub-->>GitHubAuthSvc: access_token
    deactivate GitHubAuthSvc

    UseCase->>GitHubAuthSvc: GetUserInfo(ctx, token)
    activate GitHubAuthSvc
    GitHubAuthSvc->>GitHub: GET /user
    GitHub-->>GitHubAuthSvc: User data
    deactivate GitHubAuthSvc

    UseCase->>UserRepo: FindByGitHubID(ctx, githubID)
    activate UserRepo
    UserRepo-->>UseCase: User (or create new)
    deactivate UserRepo

    UseCase-->>Controller: User, Token
    deactivate UseCase

    Controller->>Controller: Set session cookie
    Controller-->>User: 302 Redirect to dashboard
    deactivate Controller
    deactivate Proxy
```

### 6.4 通知送信フロー

```mermaid
sequenceDiagram
    participant Client
    participant Controller as NotificationController
    participant UseCase as SendNotificationUseCase
    participant NotificationSvc as NotificationService
    participant NotificationRepo as NotificationRepository
    participant PushService as Web Push Service

    Client->>Controller: POST /notifications/send
    activate Controller

    Controller->>UseCase: SendNotification(ctx, req)
    activate UseCase

    UseCase->>NotificationRepo: FindSubscriptionsByUserID(ctx, userID)
    activate NotificationRepo
    NotificationRepo-->>UseCase: []Subscription
    deactivate NotificationRepo

    UseCase->>NotificationSvc: SendNotification(ctx, subscription, notification)
    activate NotificationSvc

    loop For each subscription
        NotificationSvc->>PushService: Send push notification
        PushService-->>NotificationSvc: Success/Failure
    end

    NotificationSvc-->>UseCase: Result
    deactivate NotificationSvc

    UseCase->>NotificationRepo: SaveNotification(ctx, notification)
    activate NotificationRepo
    NotificationRepo-->>UseCase: nil
    deactivate NotificationRepo

    UseCase-->>Controller: nil (success)
    deactivate UseCase

    Controller-->>Client: 200 OK
    deactivate Controller
```

---

## 7. デザインパターン

agentapi-proxy では以下のデザインパターンが採用されています。

### 7.1 採用されているパターン

| パターン | 適用箇所 | 目的 |
|---------|---------|------|
| **Clean Architecture** | 全体構造 | 関心の分離、テスタビリティ |
| **Hexagonal Architecture** | Port & Adapter 層 | ビジネスロジックの独立性 |
| **Dependency Injection** | `internal/di/` | 疎結合、テスト容易性 |
| **Repository Pattern** | `usecases/ports/repositories/` | データアクセスの抽象化 |
| **Service Locator** | `Container` | 依存関係の集中管理 |
| **Strategy Pattern** | Encryption, Credentials | アルゴリズムの切り替え |
| **Factory Pattern** | `EncryptionServiceFactory` | オブジェクト生成の抽象化 |
| **Chain of Responsibility** | `CredentialProviderChain` | 複数プロバイダーの連鎖 |
| **Adapter Pattern** | Infrastructure 層 | 外部サービスの抽象化 |

### 7.2 パターン適用例

#### Strategy Pattern（暗号化サービス）

```mermaid
classDiagram
    class EncryptionService {
        <<interface>>
        +Encrypt(plaintext) EncryptedData
        +Decrypt(encrypted) string
        +Algorithm() string
        +KeyID() string
    }

    class NoopEncryptionService {
        +Encrypt(plaintext) EncryptedData
        +Decrypt(encrypted) string
        +Algorithm() "noop"
    }

    class LocalEncryptionService {
        -key []byte
        +Encrypt(plaintext) EncryptedData
        +Decrypt(encrypted) string
        +Algorithm() "local"
    }

    class KMSEncryptionService {
        -kmsClient *kms.Client
        +Encrypt(plaintext) EncryptedData
        +Decrypt(encrypted) string
        +Algorithm() "aws-kms"
    }

    class EncryptionServiceRegistry {
        -services map[string]EncryptionService
        +Register(service)
        +SelectService(algorithm, keyID) EncryptionService
    }

    EncryptionService <|.. NoopEncryptionService
    EncryptionService <|.. LocalEncryptionService
    EncryptionService <|.. KMSEncryptionService
    EncryptionServiceRegistry o-- EncryptionService
```

#### Chain of Responsibility（認証情報プロバイダー）

```mermaid
graph LR
    Request[Credential Request] --> Chain[CredentialProviderChain]

    Chain --> Env[CredentialProviderEnv]
    Env -->|Found| Return[Return Credentials]
    Env -->|Not Found| File[CredentialProviderFile]
    File -->|Found| Return
    File -->|Not Found| K8s[CredentialProviderK8s]
    K8s -->|Found| Return
    K8s -->|Not Found| Error[Error: No credentials]

    style Return fill:#e1ffe1
    style Error fill:#ffe1e1
```

---

## 8. リソーススコープとマルチテナンシー

agentapi-proxy は **ユーザースコープ** と **チームスコープ** の2つのリソーススコープをサポートしています。

```mermaid
graph TB
    subgraph "Resource Scopes"
        UserScope[User Scope<br/>scope: user]
        TeamScope[Team Scope<br/>scope: team]
    end

    subgraph "Resources"
        Sessions[Sessions]
        Webhooks[Webhooks]
        Shares[Session Shares]
        Settings[Settings]
    end

    subgraph "Access Control"
        AdminUser[Admin User<br/>Can access all]
        TeamMember[Team Member<br/>Can access team resources]
        ResourceOwner[Resource Owner<br/>Can access own resources]
    end

    UserScope --> Sessions
    TeamScope --> Sessions
    UserScope --> Webhooks
    TeamScope --> Webhooks
    UserScope --> Shares
    TeamScope --> Shares
    UserScope --> Settings
    TeamScope --> Settings

    AdminUser -.->|Full Access| Sessions
    AdminUser -.->|Full Access| Webhooks
    AdminUser -.->|Full Access| Shares

    TeamMember -.->|Team Access| TeamScope
    ResourceOwner -.->|Own Access| UserScope

    style UserScope fill:#e1f5ff
    style TeamScope fill:#fff4e1
    style AdminUser fill:#ffe1e1
```

### アクセス制御ロジック

```go
func (u *User) CanAccessResource(ownerUserID UserID, scope string, teamID string) bool {
    // Admin can access all resources
    if u.IsAdmin() {
        return true
    }

    // Team-scoped resources
    if scope == ScopeTeam {
        return u.teamID == teamID
    }

    // User-scoped resources
    return u.id == ownerUserID
}
```

---

## 9. 外部依存関係

```mermaid
graph TB
    subgraph "agentapi-proxy"
        Core[Core Application]
    end

    subgraph "External Services"
        K8sAPI[Kubernetes API<br/>client-go]
        GitHubAPI[GitHub API<br/>go-github]
        AWSKMS[AWS KMS<br/>aws-sdk-go-v2]
        MCPServers[MCP Servers<br/>mcp-go]
    end

    subgraph "Storage"
        K8sConfigMaps[ConfigMaps]
        K8sSecrets[Secrets]
        K8sPods[Pods/Jobs]
    end

    subgraph "HTTP Framework"
        Echo[Echo v4]
        Cobra[Cobra CLI]
        Viper[Viper Config]
    end

    Core --> K8sAPI
    Core --> GitHubAPI
    Core --> AWSKMS
    Core --> MCPServers
    Core --> Echo
    Core --> Cobra
    Core --> Viper

    K8sAPI --> K8sConfigMaps
    K8sAPI --> K8sSecrets
    K8sAPI --> K8sPods

    style Core fill:#e1f5ff
    style K8sAPI fill:#fff4e1
    style GitHubAPI fill:#fff4e1
    style AWSKMS fill:#fff4e1
    style MCPServers fill:#fff4e1
```

### 主要な外部ライブラリ

| ライブラリ | 用途 | パッケージ |
|-----------|------|-----------|
| Echo v4 | HTTP Web フレームワーク | `github.com/labstack/echo/v4` |
| Cobra | CLI フレームワーク | `github.com/spf13/cobra` |
| Viper | 設定管理 | `github.com/spf13/viper` |
| go-github | GitHub API クライアント | `github.com/google/go-github/v57` |
| client-go | Kubernetes クライアント | `k8s.io/client-go` |
| aws-sdk-go-v2 | AWS SDK | `github.com/aws/aws-sdk-go-v2` |
| mcp-go | Model Context Protocol | `github.com/mark3labs/mcp-go` |

---

## 10. まとめ

### アーキテクチャの強み

1. **明確な責務分離**: 各レイヤーが独立した責務を持つ
2. **高いテスタビリティ**: インターフェースによりモックが容易
3. **拡張性**: 新しい実装を追加してもコアロジックは変更不要
4. **保守性**: 変更の影響範囲が限定される
5. **マルチテナンシー対応**: ユーザーとチームのスコープ管理

### 開発時の注意点

1. **依存の方向**: 常に外側から内側へ（Infrastructure → Use Cases → Domain）
2. **インターフェース優先**: 実装よりもインターフェース定義を先に行う
3. **DI コンテナの活用**: 依存関係は DI コンテナで解決
4. **テストの記述**: 各レイヤーで適切なユニットテストを記述
5. **ドキュメント更新**: アーキテクチャ変更時は本ドキュメントも更新

---

このドキュメントは、agentapi-proxy の全体像を理解するためのガイドです。詳細な実装については、各パッケージのコードとテストを参照してください。
