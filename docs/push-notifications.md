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

### データベース設計
```sql
-- 購読情報テーブル
CREATE TABLE notification_subscriptions (
  id UUID PRIMARY KEY,
  user_id VARCHAR(255) NOT NULL,
  endpoint TEXT NOT NULL,
  p256dh_key TEXT NOT NULL,
  auth_key TEXT NOT NULL,
  session_ids JSONB,
  notification_types JSONB,
  created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
  expires_at TIMESTAMP WITH TIME ZONE,
  active BOOLEAN DEFAULT true
);

-- 通知履歴テーブル
CREATE TABLE notification_history (
  id UUID PRIMARY KEY,
  user_id VARCHAR(255) NOT NULL,
  subscription_id UUID REFERENCES notification_subscriptions(id),
  title VARCHAR(255) NOT NULL,
  body TEXT,
  type VARCHAR(50) NOT NULL,
  session_id VARCHAR(255),
  data JSONB,
  sent_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
  delivered BOOLEAN DEFAULT false,
  clicked BOOLEAN DEFAULT false,
  error_message TEXT
);
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