# コンテナデプロイメントガイド

## Next.js推奨のRuntime Environment Variables

このアプリケーションはNext.jsのAPI Routesを使用してruntime環境変数をサポートします。

## VAPID キーの設定方法

### 1. Runtime Configuration（推奨：Next.js標準方式）

```bash
# コンテナ起動時に環境変数を渡す
docker run -e VAPID_PUBLIC_KEY="your-public-key-here" -p 3000:3000 your-app:latest
```

### 2. Build-time Configuration（フォールバック）

```bash
# ビルド時に環境変数を設定（フォールバック用）
docker build --build-arg NEXT_PUBLIC_VAPID_PUBLIC_KEY="your-public-key-here" .
```

## セキュリティ考慮事項

- VAPIDキーはBase64URL形式（`A-Za-z0-9_-`のみ）
- API Route（`/api/config`）でサーバー側検証
- 不正な形式のキーは自動で拒否

## Docker Compose例

```yaml
version: '3.8'
services:
  agentapi-ui:
    image: your-registry/agentapi-ui:latest
    ports:
      - "3000:3000"
    environment:
      - VAPID_PUBLIC_KEY=${VAPID_PUBLIC_KEY}
    volumes:
      # 設定ファイルを外部マウントする場合（オプション）
      - ./config.js:/app/public/config.js:ro
```

## Kubernetes例

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: agentapi-ui-config
data:
  VAPID_PUBLIC_KEY: "your-public-key-here"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agentapi-ui
spec:
  template:
    spec:
      containers:
      - name: agentapi-ui
        image: your-registry/agentapi-ui:latest
        envFrom:
        - configMapRef:
            name: agentapi-ui-config
```

## VAPIDキーの生成

```bash
npx web-push generate-vapid-keys
```