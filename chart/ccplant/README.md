# ccplant Helm Chart

backendとfrontendを単一のHelm Releaseとしてデプロイします。

デフォルトは、backend/frontend各1 replica、直接Kubernetes session有効、
SCIA・asset server・永続PVC・各種background worker・Redis無効の最小構成です。

frontendの認証Cookie用Secretを先に作成してください。

## 推奨: installer CLI

導入は、抽象化された設定ファイルの生成、編集、適用の順で行います。設定の `spec` は
公開 hostname、TLS、永続化など配置先に依存しない意図を表し、Kubernetes や Compose
固有の項目は `target` 以下に分離されています。

本番では再現性のため chart version を固定してください。

```bash
agentapi-proxy control-plane init --target kubernetes --output control-plane.yaml
$EDITOR control-plane.yaml
agentapi-proxy control-plane apply --file control-plane.yaml
```

既存の Helm release から設定ファイルを生成する場合は `generate` を使います。Helm の
実効 values とクラスタ内の Cookie 暗号化 Secret を読み取るため、出力ファイルは
秘密情報を含み、パーミッション `0600` で新規作成されます。既存ファイルは上書きしません。

```bash
agentapi-proxy control-plane generate \
  --namespace ccplant \
  --release ccplant \
  --output control-plane.yaml
```

別の chart registry を利用する場合は、再適用時に使う参照を `--chart` で指定できます。
`export` は `generate` のエイリアスです。

`spec.version`はccplantリリース全体のversionです。Kubernetesではumbrella chartと
backend/frontend image、Composeでは両component imageへ同じversionを適用します。
`control-plane init --version 0.1.0`でも指定できます。`latest`は開発用途向けで、
本番では固定versionを指定してください。

Compose へ配置する場合も同じ `spec` を使います。

```bash
agentapi-proxy control-plane init --target compose --output control-plane.yaml
$EDITOR control-plane.yaml
agentapi-proxy control-plane apply --file control-plane.yaml
```

`control-plane apply --dry-run` は target adapter による生成と検証だけを行います。
Kubernetes adapter は抽象設定から Helm values を生成し、Helm/Kubernetes/chart の互換性と
`helm template` を検証してから install/upgrade します。Compose adapter は Compose
manifest を生成し、`docker compose config` を通してから適用します。

## SOPSによる任意の暗号化

設定はデフォルトでは平文です。SOPSとageを利用する場合は、生成時に公開鍵だけを
指定します。

```bash
agentapi-proxy control-plane init \
  --target kubernetes \
  --encryption sops \
  --age-recipient age1... \
  --output control-plane.yaml
```

既存の平文設定は後から暗号化できます。入力ファイルは変更せず、デフォルトでは
`control-plane.sops.yaml`へ出力します。既存の出力ファイルは上書きしません。

```bash
agentapi-proxy control-plane encrypt \
  --file control-plane.yaml \
  --age-recipient age1...

agentapi-proxy control-plane apply \
  --file control-plane.sops.yaml
```

秘密鍵はCLI引数や設定へ入れず、age identityファイルで渡します。

```bash
agentapi-proxy control-plane edit \
  --file control-plane.yaml \
  --sops-age-key-file ~/.config/sops/age/keys.txt

agentapi-proxy control-plane apply \
  --file control-plane.yaml \
  --sops-age-key-file ~/.config/sops/age/keys.txt
```

`--sops-age-key-file`を省略すると、`SOPS_AGE_KEY_FILE`、その後SOPS標準の鍵探索が
使われます。復号内容はメモリ上で解析し、平文の一時設定ファイルは作りません。

## Helm を直接使う場合

```bash
kubectl create namespace ccplant
kubectl create secret generic agentapi-ui-encryption \
  --namespace ccplant \
  --from-literal=cookie-encryption-secret="$(openssl rand -hex 32)"
```

```bash
helm install ccplant oci://ghcr.io/ccplant/charts/ccplant \
  --version 0.1.0 \
  --namespace ccplant \
  --create-namespace
```

`backend.*`と`frontend.*`以下には各コンポーネントChartのvaluesを指定できます。
デフォルトの `values.yaml` がこの最小構成を定義しています。

注意事項:

- sessionの作業領域はデフォルトで`emptyDir`です。永続化する場合は
  `backend.kubernetesSession.pvc.enabled=true`とStorageClassを設定してください。
- proxyを複数replicaにする場合は、`backend.redis.enabled=true`または
  `backend.externalRedis.addr`を設定する必要があります。
- SCIA、asset配信、schedule、SlackBot cleanup、stock inventoryは必要な場合だけ有効化してください。
