# Push Notification Subscribe API 仕様書

## 概要

プッシュ通知のサブスクリプションを管理するためのAPIエンドポイントです。ユーザーのデバイスをプッシュ通知サービスに登録・削除・一覧取得することができます。

## エンドポイント

### `POST /api/subscribe`

プッシュ通知のサブスクリプションを登録します。

#### 認証

**必須**: ログイン認証が必要です。`agentapi_token` Cookieが設定されている必要があります。

#### リクエスト

##### Headers
```
Content-Type: application/json
Cookie: agentapi_token=<認証トークン>
```

##### Body
```json
{
  "endpoint": "https://fcm.googleapis.com/fcm/send/...",
  "keys": {
    "p256dh": "BNcRd...",
    "auth": "tBHI..."
  }
}
```

##### パラメータ説明

| パラメータ | 型 | 必須 | 説明 |
|----------|-----|------|------|
| endpoint | string | ✓ | プッシュサービスのエンドポイントURL |
| keys | object | ✓ | 暗号化キー情報 |
| keys.p256dh | string | ✓ | P-256曲線上のDiffie-Hellman公開鍵 |
| keys.auth | string | ✓ | 認証シークレット |

#### レスポンス

##### 成功時 (200 OK)
```json
{
  "success": true,
  "message": "サブスクリプションが正常に保存されました",
  "subscriptionCount": 1,
  "userInfo": {
    "userId": "12345",
    "userType": "github",
    "userName": "example_user"
  }
}
```

##### エラーレスポンス

###### 未認証 (401 Unauthorized)
```json
{
  "error": "プッシュ通知を利用するにはログインが必要です"
}
```

###### ユーザー情報取得失敗 (401 Unauthorized)
```json
{
  "error": "ユーザー情報の取得に失敗しました。再度ログインしてください"
}
```

###### 無効なデータ (400 Bad Request)
```json
{
  "error": "無効なサブスクリプションデータです"
}
```

###### サーバーエラー (500 Internal Server Error)
```json
{
  "error": "サーバーエラーが発生しました"
}
```

---

### `GET /api/subscribe`

登録されているサブスクリプションの一覧を取得します。

#### リクエスト

##### Headers
```
Cookie: agentapi_token=<認証トークン>（オプション）
```

#### レスポンス

##### 成功時 (200 OK)
```json
{
  "subscriptionCount": 2,
  "subscriptions": [
    {
      "endpoint": "https://fcm.googleapis.com/fcm/send/...",
      "hasKeys": true,
      "userId": "12345",
      "userType": "github",
      "userName": "example_user",
      "createdAt": "2024-01-20T10:30:00.000Z"
    },
    {
      "endpoint": "https://updates.push.services.mozilla.com/...",
      "hasKeys": true,
      "userId": "api_key_abc123",
      "userType": "api_key",
      "userName": "API User",
      "createdAt": "2024-01-20T11:00:00.000Z"
    }
  ]
}
```

---

### `DELETE /api/subscribe`

サブスクリプションを削除します。

#### リクエスト

##### Headers
```
Content-Type: application/json
Cookie: agentapi_token=<認証トークン>（オプション）
```

##### Body
```json
{
  "endpoint": "https://fcm.googleapis.com/fcm/send/..."
}
```

##### パラメータ説明

| パラメータ | 型 | 必須 | 説明 |
|----------|-----|------|------|
| endpoint | string | ✓ | 削除するサブスクリプションのエンドポイントURL |

#### レスポンス

##### 成功時 (200 OK)
```json
{
  "success": true,
  "message": "サブスクリプションが削除されました",
  "removedCount": 1
}
```

##### エラーレスポンス

###### エンドポイント未指定 (400 Bad Request)
```json
{
  "error": "エンドポイントが指定されていません"
}
```

###### サブスクリプション未発見 (404 Not Found)
```json
{
  "error": "指定されたサブスクリプションが見つかりません"
}
```

###### サーバーエラー (500 Internal Server Error)
```json
{
  "error": "サーバーエラーが発生しました"
}
```

---

## データモデル

### SubscriptionData

内部的に保存されるサブスクリプションデータの構造：

```typescript
interface SubscriptionData {
  endpoint: string;              // プッシュサービスのエンドポイント
  keys: {
    p256dh: string;             // P-256公開鍵
    auth: string;               // 認証シークレット
  };
  userId?: string;              // ユーザー識別子
  userType?: 'github' | 'api_key';  // 認証タイプ
  userName?: string;            // ユーザー名（表示用）
  createdAt?: Date;             // 作成日時
}
```

---

## 使用例

### JavaScript (ブラウザ)

```javascript
// サービスワーカーの登録とサブスクリプション取得
async function subscribeToPushNotifications() {
  // 1. サービスワーカーを登録
  const registration = await navigator.serviceWorker.register('/sw-push.js');
  
  // 2. プッシュマネージャーでサブスクリプションを作成
  const subscription = await registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(VAPID_PUBLIC_KEY)
  });
  
  // 3. サーバーにサブスクリプションを送信
  const response = await fetch('/api/subscribe', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(subscription.toJSON()),
  });
  
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error);
  }
  
  const result = await response.json();
  console.log('サブスクリプション登録成功:', result);
}

// サブスクリプションの削除
async function unsubscribeFromPushNotifications(subscription) {
  const response = await fetch('/api/subscribe', {
    method: 'DELETE',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ endpoint: subscription.endpoint }),
  });
  
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error);
  }
  
  // ブラウザ側のサブスクリプションも削除
  await subscription.unsubscribe();
}

// VAPID公開鍵をUint8Arrayに変換するユーティリティ関数
function urlBase64ToUint8Array(base64String) {
  const padding = '='.repeat((4 - base64String.length % 4) % 4);
  const base64 = (base64String + padding)
    .replace(/-/g, '+')
    .replace(/_/g, '/');
  
  const rawData = window.atob(base64);
  const outputArray = new Uint8Array(rawData.length);
  
  for (let i = 0; i < rawData.length; ++i) {
    outputArray[i] = rawData.charCodeAt(i);
  }
  return outputArray;
}
```

---

## 注意事項

1. **認証必須**: POST リクエストには必ずログイン認証が必要です
2. **ユーザー情報の自動付与**: サーバー側で認証情報からユーザー情報を自動的に取得・付与します
3. **重複チェック**: 同じエンドポイントのサブスクリプションは上書きされます
4. **メモリベース実装**: 現在の実装はメモリベースのため、サーバー再起動でデータが失われます。本番環境では永続化ストレージ（データベース、Redis等）の使用を推奨します

---

## セキュリティ考慮事項

1. **HTTPS必須**: プッシュ通知のエンドポイントはHTTPS経由でのみ動作します
2. **認証トークンの保護**: `agentapi_token` Cookieは httpOnly, secure, sameSite 属性で保護されている必要があります
3. **VAPID鍵の管理**: VAPID秘密鍵は環境変数で安全に管理し、公開しないようにしてください
4. **エンドポイントの検証**: 受信したエンドポイントURLの形式を検証することを推奨します

---

## 関連API

- [Send Notification API](./push-notification-send.md) - プッシュ通知を送信するAPI
- [Auth Status API](./auth-status.md) - 認証状態を確認するAPI