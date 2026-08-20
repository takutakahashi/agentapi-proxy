# Push Notification Send API 仕様書

## 概要

登録されたユーザーのデバイスにプッシュ通知を送信するためのAPIエンドポイントです。特定のユーザーまたはユーザータイプを指定して通知を送信できます。

## エンドポイント

### `POST /api/send-notification`

プッシュ通知を送信します。

#### 認証

推奨: ログイン認証（送信者情報の自動取得のため）

#### リクエスト

##### Headers
```
Content-Type: application/json
Cookie: agentapi_token=<認証トークン>（オプション）
```

##### Body
```json
{
  "title": "通知タイトル",
  "body": "通知本文",
  "targetUserId": "12345",
  "url": "/chats/123",
  "icon": "/custom-icon.png",
  "badge": "/custom-badge.png",
  "tag": "chat-notification"
}
```

##### パラメータ説明

| パラメータ | 型 | 必須 | 説明 |
|----------|-----|------|------|
| title | string | ✓ | 通知のタイトル |
| body | string | ✓ | 通知の本文 |
| targetUserId | string | ※ | 送信対象の特定ユーザーID |
| targetUserType | 'github' \| 'api_key' | ※ | 送信対象のユーザータイプ |
| url | string | - | クリック時の遷移先URL（デフォルト: '/'） |
| icon | string | - | 通知アイコンのURL（デフォルト: '/icon-192x192.png'） |
| badge | string | - | 通知バッジのURL（デフォルト: '/icon-192x192.png'） |
| tag | string | - | 通知タグ（同一タグの通知は置き換えられる） |
| senderInfo | object | - | 送信者情報（未指定時は認証情報から自動取得） |

※ `targetUserId` または `targetUserType` のいずれか一方は必須

##### senderInfo オブジェクト（オプション）
```json
{
  "userId": "sender123",
  "userName": "送信者名",
  "userType": "github"
}
```

#### レスポンス

##### 成功時 (200 OK)
```json
{
  "success": true,
  "message": "2件の通知を送信しました",
  "sent": 2,
  "failed": 0,
  "total": 2,
  "target": {
    "userId": "12345",
    "userType": null
  },
  "sender": {
    "userId": "sender123",
    "userName": "example_user",
    "userType": "github"
  }
}
```

##### エラーレスポンス

###### VAPID未設定 (500 Internal Server Error)
```json
{
  "error": "VAPID keys are not configured. Please set environment variables."
}
```

###### 必須パラメータ不足 (400 Bad Request)
```json
{
  "error": "タイトルと本文は必須です"
}
```

###### 送信対象未指定 (400 Bad Request)
```json
{
  "error": "targetUserIdまたはtargetUserTypeのいずれかは必須です"
}
```

###### サブスクリプションなし (400 Bad Request)
```json
{
  "error": "アクティブなサブスクリプションがありません"
}
```

###### サーバーエラー (500 Internal Server Error)
```json
{
  "error": "サーバーエラーが発生しました"
}
```

---

### `GET /api/send-notification`

プッシュ通知機能の設定状態を確認します。

#### レスポンス

##### 成功時 (200 OK)
```json
{
  "vapidPublicKey": "BNcRd...",
  "subscriptionCount": 5,
  "status": "ready"
}
```

##### ステータス値
- `"ready"`: VAPID鍵が設定済みで通知送信可能
- `"not_configured"`: VAPID鍵が未設定

---

## プッシュ通知ペイロード

実際にデバイスに送信されるペイロードの構造：

```json
{
  "title": "通知タイトル",
  "body": "通知本文",
  "url": "/chats/123",
  "icon": "/icon-192x192.png",
  "badge": "/icon-192x192.png",
  "tag": "agentapi-notification",
  "data": {
    "sender": {
      "userId": "sender123",
      "userName": "example_user",
      "userType": "github"
    },
    "timestamp": "2024-01-20T10:30:00.000Z",
    "targetUserId": "12345",
    "targetUserType": null
  }
}
```

---

## 使用例

### 特定ユーザーへの通知送信

```javascript
async function sendNotificationToUser(userId, title, body) {
  const response = await fetch('/api/send-notification', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      title,
      body,
      targetUserId: userId,
      url: '/chats/new-message'
    }),
  });
  
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error);
  }
  
  const result = await response.json();
  console.log(`${result.sent}件の通知を送信しました`);
}
```

### 特定の認証タイプのユーザーへの通知送信

```javascript
async function sendNotificationToGitHubUsers(title, body) {
  const response = await fetch('/api/send-notification', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      title,
      body,
      targetUserType: 'github',
      tag: 'github-announcement'
    }),
  });
  
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error);
  }
  
  const result = await response.json();
  console.log(`GitHubユーザー${result.sent}名に通知を送信しました`);
}
```

### カスタムアイコンとアクションURLを指定した通知

```javascript
async function sendCustomNotification() {
  const response = await fetch('/api/send-notification', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      title: '新しいメッセージ',
      body: 'チャットに新しいメッセージがあります',
      targetUserId: 'user123',
      url: '/chats/conversation-123',
      icon: '/icons/message.png',
      badge: '/icons/badge.png',
      tag: 'chat-message-123'
    }),
  });
  
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error);
  }
}
```

---

## Service Worker での通知処理

`/public/sw-push.js` でプッシュイベントを処理する例：

```javascript
self.addEventListener('push', async (event) => {
  if (!event.data) return;
  
  try {
    const payload = event.data.json();
    const { title, body, url, icon, badge, tag, data } = payload;
    
    const notificationOptions = {
      body,
      icon: icon || '/icon-192x192.png',
      badge: badge || '/icon-192x192.png',
      tag: tag || 'default',
      data: { url, ...data },
      requireInteraction: true,
      actions: [
        { action: 'open', title: '開く' },
        { action: 'close', title: '閉じる' }
      ]
    };
    
    event.waitUntil(
      self.registration.showNotification(title, notificationOptions)
    );
  } catch (error) {
    console.error('通知の表示に失敗:', error);
  }
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  
  if (event.action === 'open' || !event.action) {
    const url = event.notification.data?.url || '/';
    event.waitUntil(
      clients.openWindow(url)
    );
  }
});
```

---

## 注意事項

1. **送信対象の指定必須**: ブロードキャスト機能は廃止されたため、必ず `targetUserId` または `targetUserType` を指定する必要があります

2. **通知の重複管理**: `tag` パラメータを使用して同一の通知を置き換えることができます

3. **送信者情報**: 認証済みユーザーからの送信時は送信者情報が自動的に付与されます

4. **エラーハンドリング**: 複数のサブスクリプションに送信する際、一部が失敗しても成功した分は送信されます

5. **通知の制限**: ブラウザやOSの設定により、通知が表示されない場合があります

---

## セキュリティ考慮事項

1. **VAPID認証**: Web Push Protocol の VAPID (Voluntary Application Server Identification) を使用して送信元を認証

2. **送信対象の制限**: 特定のユーザーまたはユーザータイプのみに送信可能

3. **ペイロードの暗号化**: Web Push の仕様により、ペイロードは自動的に暗号化されます

4. **環境変数の保護**:
   - `NEXT_PUBLIC_VAPID_PUBLIC_KEY`: 公開可能

---

## 関連API

- [Subscribe API](./push-notification-subscribe.md) - プッシュ通知のサブスクリプション管理
- [User Info API](./user-info.md) - ユーザー情報の取得