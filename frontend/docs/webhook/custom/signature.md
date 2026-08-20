# 署名検証の設定

Custom Webhook では、HMAC 署名を使ってリクエストが正当なソースから送信されたことを検証します。これにより、なりすましや改ざんを防ぐことができます。

## 署名検証の仕組み

```
送信側:
1. ペイロードと Secret で HMAC を計算
2. 署名をヘッダーに追加して送信

受信側 (agentapi-proxy):
1. ヘッダーから署名を取得
2. ペイロードと Secret で HMAC を再計算
3. 署名が一致すれば OK、不一致なら拒否
```

## サポートされる署名アルゴリズム

| アルゴリズム | ヘッダー形式 | 使用例 |
|------------|-------------|---------|
| SHA-256 (推奨) | `sha256=<hex>` | GitHub, Slack |
| SHA-1 | `sha1=<hex>` | 古い GitHub Webhook |
| SHA-512 | `sha512=<hex>` | カスタム実装 |

## 基本的な設定

### 1. Webhook 作成時に Secret を取得

Webhook を作成すると、64文字の HEX 文字列が Secret として発行されます。

```json
{
  "id": "webhook-abc-123",
  "webhook_url": "https://agentapi.example.com/hooks/custom/webhook-abc-123",
  "secret": "a1b2c3d4e5f6...（64文字）"
}
```

### 2. 外部サービスに Secret を設定

サービスの Webhook 設定画面で、この Secret を入力します。

### 3. 署名ヘッダーの設定

デフォルトでは `X-Signature` ヘッダーを使用しますが、サービスによっては異なる名前を使います。

| サービス | ヘッダー名 | アルゴリズム |
|---------|-----------|------------|
| GitHub | `X-Hub-Signature-256` | SHA-256 |
| Slack | `X-Slack-Signature` | SHA-256 |
| カスタム | `X-Signature` | SHA-256 |

## 署名の計算方法

### SHA-256 の例（Node.js）

```javascript
const crypto = require('crypto');

const secret = 'your-webhook-secret';
const payload = JSON.stringify({
  event: {
    type: 'alert',
    message: 'Test'
  }
});

const hmac = crypto.createHmac('sha256', secret);
hmac.update(payload);
const signature = 'sha256=' + hmac.digest('hex');

console.log(signature);
// sha256=a1b2c3d4...
```

### SHA-256 の例（Python）

```python
import hmac
import hashlib
import json

secret = b'your-webhook-secret'
payload = json.dumps({
    'event': {
        'type': 'alert',
        'message': 'Test'
    }
})

signature = 'sha256=' + hmac.new(
    secret,
    payload.encode('utf-8'),
    hashlib.sha256
).hexdigest()

print(signature)
# sha256=a1b2c3d4...
```

### SHA-256 の例（Go）

```go
package main

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
)

func main() {
    secret := []byte("your-webhook-secret")
    payload := []byte(`{"event":{"type":"alert","message":"Test"}}`)

    h := hmac.New(sha256.New, secret)
    h.Write(payload)
    signature := "sha256=" + hex.EncodeToString(h.Sum(nil))

    fmt.Println(signature)
    // sha256=a1b2c3d4...
}
```

## cURL での送信例

```bash
SECRET="your-webhook-secret"
PAYLOAD='{"event":{"type":"alert","message":"Test"}}'

# 署名を計算
SIGNATURE="sha256=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "$SECRET" | sed 's/^.* //')"

# リクエストを送信
curl -X POST https://agentapi.example.com/hooks/custom/webhook-abc-123 \
  -H "X-Signature: $SIGNATURE" \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD"
```

## サービス別の設定例

### Slack

Slack は `X-Slack-Signature` ヘッダーを使い、タイムスタンプを含めた検証を行います。

```
# Slack の署名ベース文字列
sig_basestring = v0:timestamp:request_body

# 署名計算
signature = 'v0=' + hmac_sha256(signing_secret, sig_basestring)
```

**Slack Webhook の設定**:
1. Slack App 設定で Webhook URL を設定
2. Signing Secret をコピー
3. agentapi-proxy の Secret として使用

### Datadog

Datadog はカスタムヘッダーをサポートしています。

```bash
# Webhook 設定
URL: https://agentapi.example.com/hooks/custom/webhook-abc-123
Custom Header: X-Signature
Value: sha256=<computed-signature>
```

### GitHub（参考）

GitHub Webhook は専用のエンドポイント (`/hooks/github/{id}`) を使いますが、同様の仕組みです。

```
# GitHub の署名ヘッダー
X-Hub-Signature-256: sha256=<signature>
```

## トラブルシューティング

### 問題 1: 署名検証エラー（401 Unauthorized）

**原因**:
- Secret が間違っている
- ペイロードが異なる（改行、スペースなど）
- 署名アルゴリズムが異なる

**解決方法**:
1. Secret をコピー&ペーストで正確に設定
2. ペイロードをそのまま使って署名を計算（整形しない）
3. 正しいアルゴリズム（SHA-256 推奨）を使用

### 問題 2: 署名ヘッダーが見つからない

**原因**:
- ヘッダー名が間違っている
- サービスが署名をサポートしていない

**解決方法**:
1. サービスのドキュメントでヘッダー名を確認
2. Delivery Record でリクエストヘッダーを確認

### 問題 3: タイムスタンプエラー（Slack）

**原因**:
- サーバー時刻がずれている
- Slack の場合、5分以上古いリクエストは拒否される

**解決方法**:
1. サーバー時刻を NTP で同期
2. タイムスタンプを検証に含める

## セキュリティのベストプラクティス

### 1. Secret を安全に管理

❌ **やってはいけない**:
- Git にコミットする
- プレーンテキストで保存する
- 複数のサービスで同じ Secret を使い回す

✅ **推奨**:
- 環境変数や Secret 管理ツールを使用
- サービスごとに異なる Secret を使用
- 定期的にローテーションする

### 2. HTTPS を使用

署名検証だけでなく、必ず HTTPS で通信しましょう。

### 3. レート制限

同一 Webhook からの大量のリクエストを制限します。

### 4. ペイロードサイズ制限

最大 1MB までのペイロードに制限しています。

## デバッグ方法

### 1. ローカルでテスト

```bash
# ローカルで署名を計算
echo -n '{"test": "data"}' | openssl dgst -sha256 -hmac "your-secret"
```

### 2. Delivery Record を確認

agentapi-proxy の Delivery Record 機能で、受信したリクエストを確認できます。

### 3. オンラインツールを使用

- [HMAC Generator](https://www.freeformatter.com/hmac-generator.html)
- JSON のフォーマットに注意（改行、スペース）

## まとめ

署名検証は、Webhook のセキュリティに不可欠です。正しく設定することで、なりすましや改ざんを防ぎ、安全に外部サービスと連携できます。

## 関連ページ

- [JSONPath 条件の書き方](jsonpath.md)
- [演算子リファレンス](operators.md)
- [トラブルシューティング](../troubleshooting/signature-errors.md)
