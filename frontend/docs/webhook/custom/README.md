# カスタム Webhook

Custom Webhook 機能を使うと、GitHub 以外の任意のサービスからの Webhook を受け取り、AI エージェントと連携できます。Go Template を使った柔軟な条件設定により、様々なユースケースに対応できます。

## このセクションで学ぶこと

- Custom Webhook の基本的な仕組み
- Go Template を使った条件設定
- 署名検証の設定
- 実用的な設定例

## Custom Webhook が解決する課題

GitHub Webhook は GitHub 特化型の条件設定を提供していますが、他のサービスには対応していません。Custom Webhook を使うことで：

✅ **任意の JSON 構造に対応**: どんなペイロード形式でも処理可能
✅ **柔軟な条件評価**: Go Template で複雑な条件を記述
✅ **複数のサービスに対応**: Slack、Datadog、PagerDuty など
✅ **標準的な署名検証**: HMAC による安全な通信

## 対応サービスの例

Custom Webhook は以下のようなサービスと連携できます：

| サービス | ユースケース | 詳細 |
|---------|-------------|------|
| **Slack** | インシデント通知、チャンネルイベント | [設定例](../integrations/slack.md) |
| **Datadog** | モニタリングアラート、メトリクス異常 | [設定例](../integrations/datadog.md) |
| **PagerDuty** | インシデント管理、エスカレーション | [設定例](../integrations/pagerduty.md) |
| **カスタムサービス** | 独自のアプリケーション | [設定例](../integrations/others.md) |

## Custom Webhook の構造

Custom Webhook は以下の要素で構成されます：

```json
{
  "name": "Webhook 名",
  "type": "custom",
  "triggers": [
    {
      "name": "トリガー名",
      "priority": 10,
      "enabled": true,
      "conditions": {
        "go_template": "{{eq .event.type \"alert\"}}"
      },
      "session_config": {
        "initial_message_template": "アラート: {{.event.message}}",
        "tags": {
          "source": "custom"
        }
      }
    }
  ]
}
```

### 主要なフィールド

| フィールド | 説明 |
|----------|------|
| `name` | Webhook の識別名 |
| `type` | `custom` を指定 |
| `triggers` | トリガー条件の配列（複数設定可能） |
| `triggers[].conditions.go_template` | Go Template 条件式 |
| `triggers[].session_config` | セッション設定とテンプレート |

## クイックスタート

### 1. Custom Webhook の作成

UI または API で Custom Webhook を作成します。

```bash
curl -X POST https://agentapi.example.com/webhooks \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "My Custom Webhook",
    "type": "custom",
    "triggers": [...]
  }'
```

### 2. Webhook URL を取得

レスポンスから Webhook URL と Secret を取得します：

```json
{
  "id": "webhook-abc-123",
  "webhook_url": "https://agentapi.example.com/hooks/custom/webhook-abc-123",
  "secret": "64文字のHEX文字列"
}
```

### 3. 外部サービスに設定

Webhook URL を外部サービスに登録し、Secret を設定します。

### 4. テスト送信

```bash
curl -X POST https://agentapi.example.com/hooks/custom/webhook-abc-123 \
  -H "X-Signature: sha256=computed-signature" \
  -H "Content-Type: application/json" \
  -d '{
    "event": {
      "type": "alert",
      "message": "Test alert"
    }
  }'
```

## 次のステップ

- [Go Template 条件の書き方](go-template.md) - 条件設定の基礎
- [署名検証の設定](signature.md) - セキュリティ設定

## よくある質問

**Q: GitHub Webhook と Custom Webhook の違いは？**
A: GitHub Webhook は GitHub 専用の条件設定を提供します。Custom Webhook は Go Template を使って任意の JSON 構造に対応できます。

**Q: 複数の条件を設定できますか？**
A: はい、Go Template 内で複数の条件を組み合わせて記述できます。

**Q: 署名検証は必須ですか？**
A: はい、セキュリティのため署名検証が必須です。HMAC-SHA256 による検証を推奨します。

**Q: ペイロードサイズに制限はありますか？**
A: 最大 1MB までのペイロードを受け付けます。

## まとめ

Custom Webhook を使うことで、GitHub 以外の任意のサービスと agentapi-proxy を連携できます。Go Template による柔軟な条件設定と、標準的な署名検証により、安全で強力な統合が実現できます。
