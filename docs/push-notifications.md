# プッシュ通知API仕様書

## 概要
このAPIは、agentapi-ui においてユーザーにリアルタイムでプッシュ通知を配信するためのエンドポイントを提供します。セッションベースの通知配信、WebPushサポート、およびイベント管理機能を含みます。

## エンドポイント一覧

### 通知購読管理

#### POST /notifications/subscribe
プッシュ通知の購読を開始します。

##### リクエストボディ
```json
{
  "endpoint": "https://fcm.googleapis.com/fcm/send/...",
  "keys": {
    "p256dh": "BG3OGHrl3YJ5PHpl0GSqtALSDRZFj4Bcq3PF6BdJlHs...",
    "auth": "I7Psnr6vvdoYUsL3G6JXRM=="
  },
  "session_ids": ["abc123", "def456"],
  "notification_types": ["message", "status_change", "session_update"]
}
```

##### フィールド説明
- `endpoint`: WebPushのエンドポイントURL
- `keys`: WebPush暗号化キー
- `session_ids`: 通知を受け取りたいセッションIDの配列（省略時は全セッション）
- `notification_types`: 受け取りたい通知タイプの配列

##### レスポンス
```json
{
  "subscription_id": "sub_abc123",
  "created_at": "2023-06-08T12:00:00Z",
  "expires_at": "2023-07-08T12:00:00Z"
}
```

#### GET /notifications/subscriptions
現在のユーザーの購読一覧を取得します。

##### レスポンス
```json
{
  "subscriptions": [
    {
      "subscription_id": "sub_abc123",
      "session_ids": ["abc123", "def456"],
      "notification_types": ["message", "status_change"],
      "created_at": "2023-06-08T12:00:00Z",
      "expires_at": "2023-07-08T12:00:00Z",
      "active": true
    }
  ]
}
```

#### PUT /notifications/subscriptions/{subscription_id}
購読設定を更新します。

##### リクエストボディ
```json
{
  "session_ids": ["abc123", "xyz789"],
  "notification_types": ["message", "session_update"]
}
```

#### DELETE /notifications/subscriptions/{subscription_id}
購読を削除します。

### 通知送信

#### POST /notifications/send
管理者権限でプッシュ通知を送信します。

##### リクエストボディ
```json
{
  "title": "新しいメッセージ",
  "body": "Claude からの返答が到着しました",
  "icon": "/icons/message.png",
  "badge": "/icons/badge.png",
  "data": {
    "session_id": "abc123",
    "message_id": "msg_456",
    "type": "message",
    "url": "/session/abc123"
  },
  "target": {
    "user_ids": ["user_123"],
    "session_ids": ["abc123"],
    "notification_type": "message"
  },
  "ttl": 86400,
  "urgency": "normal"
}
```

##### フィールド説明
- `title`: 通知のタイトル
- `body`: 通知の本文
- `icon`: 通知アイコンのURL
- `badge`: バッジアイコンのURL
- `data`: 通知に付加するカスタムデータ
- `target`: 送信対象の指定
- `ttl`: 通知の生存時間（秒）
- `urgency`: 通知の緊急度（low, normal, high）

### イベント連携

#### POST /notifications/webhook
agentapiからのWebhookを受信して自動通知を送信します。

##### リクエストボディ
```json
{
  "session_id": "abc123",
  "user_id": "user_123",
  "event_type": "message_received",
  "timestamp": "2023-06-08T12:00:00Z",
  "data": {
    "message_id": "msg_456",
    "content": "新しいメッセージの内容",
    "agent_status": "stable"
  }
}
```

### 通知履歴

#### GET /notifications/history
ユーザーの通知履歴を取得します。

##### クエリパラメータ
- `session_id`: セッションIDでフィルタ
- `type`: 通知タイプでフィルタ
- `limit`: 取得件数（デフォルト: 50）
- `offset`: 取得開始位置

##### レスポンス
```json
{
  "notifications": [
    {
      "notification_id": "notif_123",
      "title": "新しいメッセージ",
      "body": "Claude からの返答が到着しました",
      "type": "message",
      "session_id": "abc123",
      "sent_at": "2023-06-08T12:00:00Z",
      "delivered": true,
      "clicked": false
    }
  ],
  "total": 125,
  "has_more": true
}
```

## 通知タイプ

### message
エージェントからのメッセージ受信時に送信される通知

### status_change
エージェントのステータス変更時（running ↔ stable）に送信される通知

### session_update
セッション状態の更新時に送信される通知

### error
エラー発生時に送信される通知

## WebPush仕様

### サポートするプッシュサービス
- Google FCM (Firebase Cloud Messaging)
- Mozilla WebPush
- Microsoft WNS

### VAPID認証
- 公開鍵: 環境変数 `VAPID_PUBLIC_KEY` で設定
- 秘密鍵: 環境変数 `VAPID_PRIVATE_KEY` で設定
- 連絡先メール: 環境変数 `VAPID_CONTACT_EMAIL` で設定

## セキュリティ

### 認証・認可
- 全エンドポイントで認証が必要
- ユーザーは自分の購読のみ管理可能
- 管理者は全ユーザーの通知送信・管理が可能

### プライバシー
- 購読情報は暗号化して保存
- プッシュ通知の内容は必要最小限に制限
- 敏感な情報は通知に含めない

### レート制限
- `/notifications/send`: 1ユーザーあたり100件/分
- `/notifications/subscribe`: 1ユーザーあたり10件/分

## 必要な権限

### 通知管理権限
- `notification:subscribe` - 通知購読権限
- `notification:manage` - 通知管理権限
- `notification:send` - 通知送信権限（管理者のみ）
- `notification:history` - 通知履歴取得権限

## 実装上の考慮事項

### JSONLベースのデータストア設計

各ユーザーのホームディレクトリにJSONL（JSON Lines）ファイルを配置してデータを管理します。

#### ディレクトリ構造
```
/home/agentapi/.agentapi-proxy/myclaudes/{user_id}/notifications/
├── subscriptions.jsonl        # 購読情報
├── history.jsonl             # 通知履歴
└── .lock                     # ファイルロック用
```

#### データ形式

**subscriptions.jsonl**
```jsonl
{"id":"sub_abc123","user_id":"user_123","endpoint":"https://fcm.googleapis.com/...","keys":{"p256dh":"BG3OGHrl...","auth":"I7Psnr6v..."},"session_ids":["abc123","def456"],"notification_types":["message","status_change"],"created_at":"2023-06-08T12:00:00Z","expires_at":"2023-07-08T12:00:00Z","active":true}
{"id":"sub_def456","user_id":"user_123","endpoint":"https://fcm.googleapis.com/...","keys":{"p256dh":"CX4PGLsl...","auth":"J8Qtor7w..."},"session_ids":["xyz789"],"notification_types":["message"],"created_at":"2023-06-08T13:00:00Z","expires_at":"2023-07-08T13:00:00Z","active":true}
```

**history.jsonl**
```jsonl
{"id":"notif_123","user_id":"user_123","subscription_id":"sub_abc123","title":"新しいメッセージ","body":"Claude からの返答が到着しました","type":"message","session_id":"abc123","data":{"message_id":"msg_456","url":"/session/abc123"},"sent_at":"2023-06-08T12:00:00Z","delivered":true,"clicked":false,"error_message":null}
{"id":"notif_124","user_id":"user_123","subscription_id":"sub_abc123","title":"ステータス変更","body":"エージェントが応答中です","type":"status_change","session_id":"abc123","data":{"status":"running"},"sent_at":"2023-06-08T12:01:00Z","delivered":true,"clicked":true,"error_message":null}
```

#### ファイル操作の実装

**購読情報の管理**
```go
type NotificationSubscription struct {
    ID                string                 `json:"id"`
    UserID           string                 `json:"user_id"`
    Endpoint         string                 `json:"endpoint"`
    Keys             map[string]string      `json:"keys"`
    SessionIDs       []string              `json:"session_ids"`
    NotificationTypes []string              `json:"notification_types"`
    CreatedAt        time.Time             `json:"created_at"`
    ExpiresAt        *time.Time            `json:"expires_at"`
    Active           bool                  `json:"active"`
}

func getNotificationsDir(userID string) string {
    return filepath.Join("/home/agentapi/.agentapi-proxy/myclaudes", userID, "notifications")
}

func ensureNotificationsDir(userID string) error {
    dir := getNotificationsDir(userID)
    return os.MkdirAll(dir, 0755)
}

func addSubscription(userID string, sub NotificationSubscription) error {
    if err := ensureNotificationsDir(userID); err != nil {
        return err
    }
    
    filePath := filepath.Join(getNotificationsDir(userID), "subscriptions.jsonl")
    lockPath := filepath.Join(getNotificationsDir(userID), ".lock")
    
    // ファイルロック
    lock, err := flock.New(lockPath)
    if err != nil {
        return err
    }
    defer lock.Close()
    
    if err := lock.Lock(); err != nil {
        return err
    }
    defer lock.Unlock()
    
    file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        return err
    }
    defer file.Close()
    
    encoder := json.NewEncoder(file)
    return encoder.Encode(sub)
}

func getSubscriptions(userID string) ([]NotificationSubscription, error) {
    filePath := filepath.Join(getNotificationsDir(userID), "subscriptions.jsonl")
    
    file, err := os.Open(filePath)
    if err != nil {
        if os.IsNotExist(err) {
            return []NotificationSubscription{}, nil
        }
        return nil, err
    }
    defer file.Close()
    
    var subscriptions []NotificationSubscription
    scanner := bufio.NewScanner(file)
    
    for scanner.Scan() {
        var sub NotificationSubscription
        if err := json.Unmarshal(scanner.Bytes(), &sub); err != nil {
            continue // 破損したエントリをスキップ
        }
        // アクティブで有効期限内の購読のみを返す
        if sub.Active && (sub.ExpiresAt == nil || sub.ExpiresAt.After(time.Now())) {
            subscriptions = append(subscriptions, sub)
        }
    }
    
    return subscriptions, scanner.Err()
}

func updateSubscription(userID string, subscriptionID string, updates NotificationSubscription) error {
    // 全購読を読み込み、該当するものを更新して書き戻し
    subscriptions, err := getAllSubscriptions(userID) // 有効期限切れも含む全て
    if err != nil {
        return err
    }
    
    filePath := filepath.Join(getNotificationsDir(userID), "subscriptions.jsonl")
    lockPath := filepath.Join(getNotificationsDir(userID), ".lock")
    
    lock, err := flock.New(lockPath)
    if err != nil {
        return err
    }
    defer lock.Close()
    
    if err := lock.Lock(); err != nil {
        return err
    }
    defer lock.Unlock()
    
    // ファイルを新しく作成
    tempFile := filePath + ".tmp"
    file, err := os.Create(tempFile)
    if err != nil {
        return err
    }
    defer file.Close()
    
    encoder := json.NewEncoder(file)
    updated := false
    
    for _, sub := range subscriptions {
        if sub.ID == subscriptionID {
            updated = true
            updates.ID = subscriptionID
            updates.UserID = userID
            if err := encoder.Encode(updates); err != nil {
                return err
            }
        } else {
            if err := encoder.Encode(sub); err != nil {
                return err
            }
        }
    }
    
    if !updated {
        os.Remove(tempFile)
        return errors.New("subscription not found")
    }
    
    return os.Rename(tempFile, filePath)
}
```

**通知履歴の管理**
```go
type NotificationHistory struct {
    ID             string                 `json:"id"`
    UserID         string                 `json:"user_id"`
    SubscriptionID string                 `json:"subscription_id"`
    Title          string                 `json:"title"`
    Body           string                 `json:"body"`
    Type           string                 `json:"type"`
    SessionID      string                 `json:"session_id"`
    Data           map[string]interface{} `json:"data"`
    SentAt         time.Time             `json:"sent_at"`
    Delivered      bool                  `json:"delivered"`
    Clicked        bool                  `json:"clicked"`
    ErrorMessage   *string               `json:"error_message"`
}

func addNotificationHistory(userID string, notification NotificationHistory) error {
    if err := ensureNotificationsDir(userID); err != nil {
        return err
    }
    
    filePath := filepath.Join(getNotificationsDir(userID), "history.jsonl")
    lockPath := filepath.Join(getNotificationsDir(userID), ".lock")
    
    lock, err := flock.New(lockPath)
    if err != nil {
        return err
    }
    defer lock.Close()
    
    if err := lock.Lock(); err != nil {
        return err
    }
    defer lock.Unlock()
    
    file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        return err
    }
    defer file.Close()
    
    encoder := json.NewEncoder(file)
    return encoder.Encode(notification)
}

func getNotificationHistory(userID string, limit, offset int, filters map[string]string) ([]NotificationHistory, int, error) {
    filePath := filepath.Join(getNotificationsDir(userID), "history.jsonl")
    
    file, err := os.Open(filePath)
    if err != nil {
        if os.IsNotExist(err) {
            return []NotificationHistory{}, 0, nil
        }
        return nil, 0, err
    }
    defer file.Close()
    
    var allNotifications []NotificationHistory
    scanner := bufio.NewScanner(file)
    
    for scanner.Scan() {
        var notification NotificationHistory
        if err := json.Unmarshal(scanner.Bytes(), &notification); err != nil {
            continue // 破損したエントリをスキップ
        }
        
        // フィルタリング
        if sessionID := filters["session_id"]; sessionID != "" && notification.SessionID != sessionID {
            continue
        }
        if notificationType := filters["type"]; notificationType != "" && notification.Type != notificationType {
            continue
        }
        
        allNotifications = append(allNotifications, notification)
    }
    
    if err := scanner.Err(); err != nil {
        return nil, 0, err
    }
    
    // 新しい順にソート
    sort.Slice(allNotifications, func(i, j int) bool {
        return allNotifications[i].SentAt.After(allNotifications[j].SentAt)
    })
    
    total := len(allNotifications)
    
    // ページネーション
    if offset >= total {
        return []NotificationHistory{}, total, nil
    }
    
    end := offset + limit
    if end > total {
        end = total
    }
    
    return allNotifications[offset:end], total, nil
}
```

#### ファイルロック

複数のリクエストが同時にファイルにアクセスすることを防ぐため、各ユーザーディレクトリに `.lock` ファイルを作成してファイルロックを実装します。

#### ファイルローテーション

通知履歴が大きくなりすぎないよう、定期的に古いエントリを削除するローテーション機能を実装します。

```go
func rotateNotificationHistory(userID string, maxEntries int) error {
    // 最新のN件のみを保持し、古いものを削除
    notifications, _, err := getNotificationHistory(userID, maxEntries*2, 0, nil)
    if err != nil {
        return err
    }
    
    if len(notifications) <= maxEntries {
        return nil // ローテーション不要
    }
    
    // 最新のmaxEntries件のみを保持
    keepNotifications := notifications[:maxEntries]
    
    filePath := filepath.Join(getNotificationsDir(userID), "history.jsonl")
    lockPath := filepath.Join(getNotificationsDir(userID), ".lock")
    
    lock, err := flock.New(lockPath)
    if err != nil {
        return err
    }
    defer lock.Close()
    
    if err := lock.Lock(); err != nil {
        return err
    }
    defer lock.Unlock()
    
    tempFile := filePath + ".tmp"
    file, err := os.Create(tempFile)
    if err != nil {
        return err
    }
    defer file.Close()
    
    encoder := json.NewEncoder(file)
    for _, notification := range keepNotifications {
        if err := encoder.Encode(notification); err != nil {
            return err
        }
    }
    
    return os.Rename(tempFile, filePath)
}
```

### 設定例
```yaml
# 環境変数
VAPID_PUBLIC_KEY: "BG3OGHrl3YJ5PHpl0GSqtALSDRZFj4Bcq..."
VAPID_PRIVATE_KEY: "your-private-key"
VAPID_CONTACT_EMAIL: "admin@example.com"
PUSH_NOTIFICATION_TTL: 86400
NOTIFICATION_RATE_LIMIT: 100
```

## 統合例

### フロントエンドでの購読登録
```javascript
// Service Worker登録
navigator.serviceWorker.register('/sw.js');

// プッシュ通知購読
const registration = await navigator.serviceWorker.ready;
const subscription = await registration.pushManager.subscribe({
  userVisibleOnly: true,
  applicationServerKey: urlBase64ToUint8Array(VAPID_PUBLIC_KEY)
});

// サーバーに購読情報を送信
await fetch('/notifications/subscribe', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${accessToken}`
  },
  body: JSON.stringify({
    endpoint: subscription.endpoint,
    keys: {
      p256dh: arrayBufferToBase64(subscription.getKey('p256dh')),
      auth: arrayBufferToBase64(subscription.getKey('auth'))
    },
    session_ids: [currentSessionId],
    notification_types: ['message', 'status_change']
  })
});
```

### agentapiとの連携
agentapi の `/events` エンドポイントからのSSEイベントを監視し、以下のイベント時に自動通知を送信：

1. `message_update`: 新しいメッセージ受信時
2. `status_change`: エージェントステータス変更時

```javascript
// SSEイベント監視例
const eventSource = new EventSource(`/session_id/${sessionId}/events`);
eventSource.addEventListener('message_update', async (event) => {
  const data = JSON.parse(event.data);
  await sendPushNotification({
    session_id: sessionId,
    event_type: 'message_received',
    data: data
  });
});
```