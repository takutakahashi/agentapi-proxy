# カスタムWebhook対応の設計

## 概要

現在、agentapi-proxyはGitHub webhookのみをサポートしていますが、この設計ではSlack、Datadog、PagerDuty、カスタムサービスなど、任意のWebhookソースからのイベントを処理できるように拡張します。

## 現状分析

### 既存の実装

#### アーキテクチャ

```
┌─────────────────────────────────────────┐
│  Webhook Entity & Repository (Domain)  │
│  - webhook.go                           │
│  - webhook_repository.go                │
│  - Kubernetes Secret ベース             │
└─────────────────────────────────────────┘
                 ↓
┌─────────────────────────────────────────┐
│  Controller Layer                       │
│  - webhook_controller.go (CRUD API)     │
│  - webhook_github_controller.go (受信)  │
└─────────────────────────────────────────┘
                 ↓
┌─────────────────────────────────────────┐
│  HTTP Endpoints                         │
│  - POST /webhooks (作成)                │
│  - GET /webhooks (一覧)                 │
│  - POST /hooks/github/{id} (受信)       │
└─────────────────────────────────────────┘
```

#### 主要コンポーネント

1. **Webhookエンティティ** (`internal/domain/entities/webhook.go`)
   - `WebhookType`: `github` | `custom` (定義済み)
   - `WebhookTrigger`: トリガールール配列
   - `TriggerConditions`: GitHub条件 + JSONPath条件 (未実装)
   - `WebhookSessionConfig`: セッション設定

2. **GitHub Webhook Controller** (`webhook_github_controller.go`)
   - HMAC署名検証 (SHA256/SHA1)
   - GitHubペイロード構造体による解析
   - GitHub特化型のトリガーマッチング
     - イベント (push, pull_request, issues等)
     - アクション (opened, synchronize, closed等)
     - ブランチ、リポジトリ、ラベル、パス、送信者等
   - Goテンプレートによる初期メッセージ生成

3. **OpenAPI仕様** (`spec/openapi.json`)
   - `JSONPathCondition`: path, operator, value (定義済み、未実装)
   - Operators: `eq`, `ne`, `contains`, `matches`, `in`, `exists`

### 既存実装の長所

✅ クリーンアーキテクチャで分離されている
✅ Kubernetes Secretベースでスケーラブル
✅ User/Teamスコープ対応
✅ 優先度ベースのトリガー評価
✅ 配信記録 (Delivery Record) によるトレーサビリティ
✅ テンプレートエンジンによる柔軟なメッセージ生成

### 課題

❌ GitHub以外のWebhookソースに未対応
❌ JSONPath条件評価が未実装
❌ カスタムwebhook用のコントローラーが存在しない
❌ 汎用的なペイロード処理機構がない

## 設計方針

### 基本コンセプト

1. **既存のGitHub webhookとの共存**
   - GitHub webhookの機能を維持
   - カスタムwebhookは独立したエンドポイント

2. **JSONPathベースの柔軟な条件評価**
   - 任意のJSON構造に対応
   - 複数の条件演算子サポート

3. **署名検証の一般化**
   - HMAC署名検証を再利用可能に設計
   - 複数の署名アルゴリズム対応

4. **テンプレートエンジンの拡張**
   - 汎用的なペイロードアクセス
   - GitHub以外のペイロード構造に対応

## アーキテクチャ設計

### コンポーネント構成

```
┌────────────────────────────────────────────────────┐
│  既存: WebhookGitHubController                     │
│  - POST /hooks/github/{id}                         │
│  - GitHub専用のペイロード構造体                    │
│  - GitHub特化型トリガーマッチング                  │
└────────────────────────────────────────────────────┘
                       ↓ (継続利用)
┌────────────────────────────────────────────────────┐
│  新規: WebhookCustomController                     │
│  - POST /hooks/custom/{id}                         │
│  - 汎用的なJSONペイロード処理                      │
│  - JSONPath条件評価                                │
└────────────────────────────────────────────────────┘
                       ↓
┌────────────────────────────────────────────────────┐
│  共通: SignatureVerifier (ユーティリティ)          │
│  - HMAC署名検証の共通化                            │
└────────────────────────────────────────────────────┘
                       ↓
┌────────────────────────────────────────────────────┐
│  新規: JSONPathEvaluator (ユーティリティ)          │
│  - JSONPath条件の評価                              │
│  - 演算子 (eq, ne, contains, matches, in, exists) │
└────────────────────────────────────────────────────┘
```

### ディレクトリ構造

```
internal/
├── domain/
│   └── entities/
│       ├── webhook.go (変更なし)
│       └── jsonpath_condition.go (新規: 条件評価ロジック)
├── interfaces/
│   └── controllers/
│       ├── webhook_controller.go (変更なし)
│       ├── webhook_github_controller.go (変更なし)
│       └── webhook_custom_controller.go (新規)
└── infrastructure/
    └── webhook/
        ├── signature_verifier.go (新規: 共通署名検証)
        └── jsonpath_evaluator.go (新規: JSONPath評価)
```

## 詳細設計

### 1. カスタムWebhookコントローラー

#### ファイル: `internal/interfaces/controllers/webhook_custom_controller.go`

**責務**:
- カスタムwebhookの受信エンドポイント (`POST /hooks/custom/{id}`)
- 署名検証 (HMAC)
- JSONPath条件によるトリガーマッチング
- セッション作成

**主要メソッド**:

```go
type WebhookCustomController struct {
    repo              repositories.WebhookRepository
    sessionManager    repositories.SessionManager
    signatureVerifier *webhook.SignatureVerifier
    jsonpathEvaluator *webhook.JSONPathEvaluator
}

// HandleCustomWebhook handles POST /hooks/custom/{id}
func (c *WebhookCustomController) HandleCustomWebhook(ctx echo.Context) error
```

**処理フロー**:

```
1. Webhook ID取得 (URLパスから)
2. リクエストボディ読み込み
3. Webhook設定取得 (Repository)
4. 署名検証 (X-Signature ヘッダー、設定可能)
5. JSONペイロード解析
6. トリガーマッチング (JSONPath条件評価)
7. セッション作成
8. Delivery Record記録
```

#### リクエスト例

```http
POST /hooks/custom/abc-123-def-456
X-Signature: sha256=abcdef1234567890...
Content-Type: application/json

{
  "event": "deployment.succeeded",
  "deployment": {
    "id": "deploy-123",
    "environment": "production",
    "status": "success"
  },
  "service": {
    "name": "api-server",
    "version": "v2.1.0"
  },
  "timestamp": "2026-01-11T10:30:00Z"
}
```

### 2. JSONPath条件評価

#### ファイル: `internal/infrastructure/webhook/jsonpath_evaluator.go`

**責務**:
- JSONPath式による値抽出
- 条件演算子による評価

**使用ライブラリ**: `github.com/oliveagle/jsonpath`

**条件演算子**:

| Operator   | 説明                          | 例                                   |
|------------|-------------------------------|--------------------------------------|
| `eq`       | 等価                          | `$.event == "deployment.succeeded"`  |
| `ne`       | 非等価                        | `$.status != "failed"`               |
| `contains` | 文字列/配列に含まれる         | `$.tags contains "production"`       |
| `matches`  | 正規表現マッチ                | `$.service.name matches "^api-.*"`   |
| `in`       | 配列に含まれる                | `$.environment in ["prod", "stage"]` |
| `exists`   | パスが存在する                | `$.deployment.id exists`             |

**実装イメージ**:

```go
type JSONPathEvaluator struct{}

func (e *JSONPathEvaluator) Evaluate(
    payload map[string]interface{},
    conditions []entities.JSONPathCondition,
) (bool, error) {
    for _, cond := range conditions {
        // JSONPath式で値を抽出
        value, err := jsonpath.JsonPathLookup(payload, cond.Path())
        if err != nil {
            if cond.Operator() == "exists" && cond.Value() == false {
                continue // パスが存在しない = exists: false は成功
            }
            return false, err
        }

        // 演算子で評価
        matched := e.evaluateOperator(value, cond.Operator(), cond.Value())
        if !matched {
            return false, nil
        }
    }
    return true, nil
}
```

### 3. 署名検証の共通化

#### ファイル: `internal/infrastructure/webhook/signature_verifier.go`

**責務**:
- HMAC署名の検証 (SHA256/SHA1/SHA512)
- GitHub形式 (`sha256=...`) とカスタム形式の両対応

**実装イメージ**:

```go
type SignatureVerifier struct{}

type SignatureConfig struct {
    HeaderName string // "X-Hub-Signature-256" or "X-Signature"
    Secret     string
    Algorithm  string // "sha256", "sha1", "sha512"
}

func (v *SignatureVerifier) Verify(
    payload []byte,
    signatureHeader string,
    config SignatureConfig,
) bool {
    // アルゴリズム選択
    var h hash.Hash
    switch config.Algorithm {
    case "sha256":
        h = hmac.New(sha256.New, []byte(config.Secret))
    case "sha1":
        h = hmac.New(sha1.New, []byte(config.Secret))
    case "sha512":
        h = hmac.New(sha512.New, []byte(config.Secret))
    default:
        return false
    }

    h.Write(payload)
    expected := hex.EncodeToString(h.Sum(nil))

    // GitHub形式 (sha256=...) を処理
    signature := strings.TrimPrefix(signatureHeader, config.Algorithm+"=")

    return hmac.Equal([]byte(expected), []byte(signature))
}
```

### 4. エンティティの拡張 (オプション)

#### ファイル: `internal/domain/entities/webhook.go`

**カスタムwebhook設定の追加** (将来的に):

```go
type WebhookCustomConfig struct {
    signatureHeader string // デフォルト: "X-Signature"
    signatureAlgo   string // デフォルト: "sha256"
    payloadPath     string // JSONペイロードのルートパス (オプション)
}
```

現時点ではOpenAPI仕様に含まれていないため、まずは最小限の実装を行い、必要に応じて拡張します。

## OpenAPI仕様の更新

### 新規エンドポイント

```json
"/hooks/custom/{id}": {
  "post": {
    "summary": "Receive custom webhook",
    "description": "Receives webhook payloads from custom services. Uses the webhook ID from the URL to identify the webhook and verifies the signature using the webhook's secret.",
    "operationId": "handleCustomWebhook",
    "tags": ["Webhooks"],
    "security": [],
    "parameters": [
      {
        "name": "id",
        "in": "path",
        "required": true,
        "description": "Webhook ID",
        "schema": {"type": "string"}
      }
    ],
    "requestBody": {
      "required": true,
      "content": {
        "application/json": {
          "schema": {
            "type": "object",
            "description": "Custom webhook payload (arbitrary JSON structure)"
          }
        }
      }
    },
    "responses": {
      "200": {
        "description": "Webhook processed",
        "content": {
          "application/json": {
            "schema": {
              "type": "object",
              "properties": {
                "message": {"type": "string"},
                "session_id": {"type": "string"},
                "webhook_id": {"type": "string"},
                "trigger_id": {"type": "string"}
              }
            }
          }
        }
      },
      "400": {"description": "Invalid payload"},
      "401": {"description": "Signature verification failed"},
      "404": {"description": "Webhook not found"}
    }
  }
}
```

## 使用例

### Slack Webhookの例

#### 1. Webhookの作成

```http
POST /webhooks
Authorization: Bearer <token>
Content-Type: application/json

{
  "name": "Slack Incident Alerts",
  "type": "custom",
  "triggers": [
    {
      "name": "Critical incident in production",
      "priority": 10,
      "enabled": true,
      "conditions": {
        "jsonpath": [
          {
            "path": "$.event.type",
            "operator": "eq",
            "value": "incident"
          },
          {
            "path": "$.event.severity",
            "operator": "eq",
            "value": "critical"
          },
          {
            "path": "$.event.environment",
            "operator": "eq",
            "value": "production"
          }
        ]
      },
      "session_config": {
        "initial_message_template": "Critical incident detected: {{.event.title}}\nSeverity: {{.event.severity}}\nEnvironment: {{.event.environment}}\n\nPlease investigate and take action.",
        "tags": {
          "source": "slack",
          "type": "incident"
        }
      }
    }
  ]
}
```

**レスポンス**:
```json
{
  "id": "webhook-abc-123",
  "webhook_url": "https://agentapi.example.com/hooks/custom/webhook-abc-123",
  "secret": "64文字のHEX文字列"
}
```

#### 2. Slackからのペイロード送信

```http
POST /hooks/custom/webhook-abc-123
X-Signature: sha256=computed-signature
Content-Type: application/json

{
  "event": {
    "type": "incident",
    "title": "Database connection pool exhausted",
    "severity": "critical",
    "environment": "production",
    "timestamp": "2026-01-11T10:30:00Z"
  },
  "user": {
    "id": "U12345",
    "name": "john.doe"
  }
}
```

#### 3. 処理フロー

1. **署名検証**: `X-Signature` ヘッダーをwebhookのsecretで検証
2. **JSONPath評価**:
   - `$.event.type == "incident"` ✅
   - `$.event.severity == "critical"` ✅
   - `$.event.environment == "production"` ✅
3. **セッション作成**: 初期メッセージテンプレートをレンダリング
4. **配信記録**: Delivery Recordを保存

### Datadogモニタリングアラートの例

```json
{
  "name": "Datadog High CPU Alert",
  "type": "custom",
  "triggers": [
    {
      "name": "CPU usage above 90%",
      "conditions": {
        "jsonpath": [
          {
            "path": "$.alert_type",
            "operator": "eq",
            "value": "metric_alert"
          },
          {
            "path": "$.current_value",
            "operator": "gt",
            "value": 90
          },
          {
            "path": "$.tags",
            "operator": "contains",
            "value": "env:production"
          }
        ]
      },
      "session_config": {
        "initial_message_template": "⚠️ High CPU Alert\n\nHost: {{.host}}\nCPU: {{.current_value}}%\nThreshold: {{.threshold}}%\n\nInvestigate the issue and scale if necessary.",
        "tags": {
          "source": "datadog",
          "alert_type": "cpu"
        }
      }
    }
  ]
}
```

## 実装計画

### Phase 1: 基盤実装

1. ✅ **要件定義と設計** (このドキュメント)
2. ⬜ **署名検証の共通化**
   - `signature_verifier.go` の実装
   - 単体テスト
3. ⬜ **JSONPath評価エンジン**
   - `jsonpath_evaluator.go` の実装
   - 全演算子のテスト
4. ⬜ **カスタムWebhookコントローラー**
   - `webhook_custom_controller.go` の実装
   - エンドポイント登録

### Phase 2: API統合

5. ⬜ **ルーティング追加**
   - `cmd/server.go` でルート登録
   - `POST /hooks/custom/{id}` の有効化
6. ⬜ **OpenAPI仕様更新**
   - `spec/openapi.json` に新規エンドポイント追加
7. ⬜ **統合テスト**
   - E2Eテストの作成
   - Slack/Datadog等の実ペイロードでのテスト

### Phase 3: ドキュメント整備

8. ⬜ **ユーザードキュメント**
   - カスタムwebhookの使い方
   - JSONPath条件の書き方
   - 各種サービスとの統合例
9. ⬜ **サンプル集**
   - Slack webhook設定例
   - Datadog webhook設定例
   - PagerDuty webhook設定例

## セキュリティ考慮事項

1. **署名検証の必須化**
   - カスタムwebhookでも署名検証を必須とする
   - 検証失敗時は401エラーを返す

2. **レート制限**
   - 同一webhookからの連続リクエストを制限
   - DDoS攻撃への対策

3. **ペイロードサイズ制限**
   - 最大1MBまでのペイロードを受け付ける
   - 大きすぎるペイロードは400エラー

4. **JSONPathインジェクション対策**
   - JSONPath式のサニタイズ
   - 危険な式のブロックリスト

## パフォーマンス考慮事項

1. **JSONPath評価の最適化**
   - 条件数が多い場合の早期終了 (AND条件)
   - キャッシング (同一パスの繰り返し評価を回避)

2. **署名検証のオーバーヘッド削減**
   - 一度だけ検証 (再検証しない)
   - アルゴリズム選択の最適化

3. **並行処理**
   - 複数webhookの同時処理に対応
   - Goroutineによる非同期処理

## 互換性と移行

### 既存のGitHub Webhookへの影響

- **影響なし**: GitHub webhookは既存のエンドポイント (`/hooks/github/{id}`) を継続使用
- **コントローラー分離**: `WebhookGitHubController` と `WebhookCustomController` は独立
- **共通ロジックの活用**: 署名検証などの共通部分は再利用

### データ構造の互換性

- **Webhookエンティティ**: 既存のフィールドを維持
- **JSONPath条件**: 新規追加のため既存データに影響なし

## テスト戦略

### 単体テスト

- `signature_verifier_test.go`: 全署名アルゴリズムのテスト
- `jsonpath_evaluator_test.go`: 全演算子のテスト
- `webhook_custom_controller_test.go`: コントローラーロジックのテスト

### 統合テスト

- エンドポイント全体のE2Eテスト
- 実ペイロードでのシナリオテスト

### テストカバレッジ目標

- 単体テスト: 80%以上
- 統合テスト: 主要シナリオ100%カバー

## まとめ

このカスタムwebhook対応により、agentapi-proxyは以下の利点を得ます：

✅ **汎用性**: GitHub以外のあらゆるwebhookソースに対応
✅ **柔軟性**: JSONPath条件により複雑なフィルタリングが可能
✅ **拡張性**: 新しいサービスの追加が容易
✅ **セキュリティ**: HMAC署名検証による安全性確保
✅ **保守性**: クリーンアーキテクチャによる明確な責任分離

既存のGitHub webhook機能を損なうことなく、agentapi-proxyの適用範囲を大幅に拡大できます。
