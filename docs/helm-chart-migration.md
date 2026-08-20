# 分離 Helm Chart から ccplant Chart への移行設計

## 方針

`agentapi-proxy` と `agentapi-ui` の旧Releaseを残したまま、別名の`ccplant` Releaseを
同じnamespaceへ先にinstallする。新Releaseが既存のruntime resourceを参照できることを
検証してから外部トラフィックを切り替え、旧Releaseを段階的にdrain・削除する。

保護対象は次に限定する。

- proxy が実行時に作成した session、設定、認証情報などの resource
- 外部から参照している Secret
- PVC など再作成するとデータを失う resource
- 既存 session が固定名で参照する Service、ServiceAccount、Secret

移行先は同じ namespace とする。namespace 自体は削除しない。

## 何が残り、何が消えるか

### 並行稼働後に削除する旧Helm resource

- backend/frontend Deployment、Service、Ingress
- Helm 管理のServiceAccount、Role、RoleBinding、ConfigMap
- Helm manifest に含まれる生成Secret
- optionalなSCIA、asset serverなどのworkload

### そのまま保持するruntime resource

proxyがKubernetes APIで作成する以下のresourceはHelm release manifestに含まれないため、
通常の`helm uninstall`では削除されない。

- `agentapi-session-*` Service、Pod/Deployment、PVC、settings Secret
- memory、schedule、webhook、SlackBot
- settings、credentials、API token、personal API key
- session profile、sandbox policy、team config、user/team mapping
- `agentapi-agent-files-*`、`agentapi-user-files-*`
- notification subscription、SCIAのdynamic user Secret

session専用Secretはsession ServiceをOwnerReferenceに持つ。移行中にsession Serviceを
削除しないことが、Secretを保持する条件になる。

## install-firstを可能にするresource名

新旧ReleaseのHelm resourceが衝突しないよう、ccplant側は既定の別名を使う。

| 用途 | 旧Release | 新ccplant Release |
|---|---|---|
| backend Service | `agentapi-proxy` | `ccplant-backend` |
| frontend Service | `agentapi-ui` | `ccplant-frontend` |
| backend Deployment | `agentapi-proxy` | `ccplant-backend` |
| frontend Deployment | `agentapi-ui` | `ccplant-frontend` |

`backend.fullnameOverride: agentapi-proxy`はinstall時に衝突するため設定しない。frontendは
既定で`http://ccplant-backend:8080`を参照できる。

Ingressは同一host/pathの重複を避けるため、新Releaseでは最初は無効にする。shadow検証は
port-forwardまたは一時的な内部hostnameを使い、cutover時に既存Ingressのbackendを
`ccplant-frontend`/`ccplant-backend`へ切り替える。

## 既存sessionとの互換性

既存sessionはRelease名ではなく、固定ラベルで再検出される。

```yaml
app.kubernetes.io/managed-by: agentapi-proxy
app.kubernetes.io/name: agentapi-session
agentapi.proxy/session-id: <session-id>
```

変更前のversionで作成されたsession Pod内のprovisionerは、次の旧Service名を参照する。

```text
http://agentapi-proxy.<namespace>.svc.cluster.local:8080
```

新しいbackendではsession provisionerの既定callback先をRelease名に依存しない
`control` Serviceとする。Service selectorを旧backendから新backendへ切り替える
ことで、対応versionで作成されたsessionは再作成せずにcontrol planeを切り替えられる。
変更前のversionで作成され、`agentapi-proxy`を直接参照するsessionだけはlegacy sessionとして
drainまたは互換Serviceで処理する。

session用ServiceAccount/Role/RoleBindingは現在固定名`agentapi-proxy-session`であり、並行
installすると衝突する。ccplant Chartに次のoptionを追加し、shadow installでは既存RBACを
共有する。

```yaml
backend:
  controlPlaneService:
    create: false
  kubernetesSession:
    rbac:
      create: false
    serviceAccountName: agentapi-proxy-session
```

control Serviceは`helm.sh/resource-policy: keep`で保持される。旧backendをuninstallする前に
selectorを新backendへ切り替える。session RBACは一時的に既存Releaseのものを共有し、旧backend
削除前にccplant管理へ引き継ぐかRelease外の共通RBACとして管理する。

## Secret互換性

Secretを次の4種類に分類する。

| 種類 | 例 | 処理 |
|---|---|---|
| session専用 | `agentapi-session-*-settings`、webhook payload | 保持 |
| application data | schedule、credentials、API token | 保持 |
| 外部参照 | OAuth、GitHub PEM、Slack、cookie暗号鍵 | 同じname/keyを再利用 |
| Helm生成 | GitHub session/config、SCIA credential | 同じ入力から再生成 |

暗号化済みデータには、同じ暗号方式と鍵が必須である。

- `AGENTAPI_ENCRYPTION_KEY`または`config.encryption.key`
- KMS key ID、region、IAM権限
- Git sync用KMS設定

移行ツールはSecret値を表示・複製せず、参照先のname/key、暗号方式、key fingerprintを
比較する。install後は実際のsettings/credential復号をprobeする。

## PVCの例外

runtime生成のsession PVCは残る。一方、Helm manifestに直接含まれるasset PVCなどは
`helm uninstall`で削除され得る。Redis StatefulSetのvolumeClaimTemplate由来PVCも含め、
実際の保持動作だけに依存せずpreflightで全PVCを分類する。

- データ不要: 再作成を許可
- snapshot/backup可能: backup完了後に移行
- 保持必須: 旧Releaseを削除する前にretain可能な構成へupgradeする
- 分類不能: 移行をblockする

保持必須PVC向けにcomponent Chartへ`helm.sh/resource-policy: keep`を設定できる
`persistence.retainOnDelete`相当のoptionを追加する。古いChartが対応しない場合は、まず
同じ分離Release名のまま互換Chartへupgradeしてからuninstallする。

## サポートツール

`agentapi-proxy helm migrate plan`と`agentapi-proxy helm migrate verify`はclusterを変更しない。
install、Service selector変更、uninstallは実行せず、診断結果と実行候補commandだけを出力する。

実行例:

```bash
agentapi-proxy helm migrate plan \
  --namespace agentapi-ui \
  --backend-release agentapi-proxy \
  --frontend-release agentapi-ui \
  --target-release ccplant \
  --chart oci://ghcr.io/ccplant/charts/ccplant \
  --version 0.3.2 \
  --values-out ccplant-shadow-values.yaml
```

`--version`は`latest`やrangeを受け付けず、完全なsemantic versionを必須とする。`--output`は
`text`、`json`、`yaml`に対応する。生成valuesはSecretを含む可能性があるためmode `0600`で保存する。

preflightは次を検査する。

1. 旧Release revision、values、manifestを読む
2. backend valuesを`backend.*`、frontend valuesを`frontend.*`へ変換する
3. target Release名が未使用であることを確認する
4. `control` Serviceの存在、selector、`helm.sh/resource-policy: keep`を確認する
5. 固定名`agentapi-proxy-session`のServiceAccount、Role、RoleBindingを確認する
6. valuesから参照されるSecret name/keyの存在を値を表示せず確認する
7. runtime Secret/PVCに旧Helm ownershipが誤って付いていないか確認する
8. session Podのcallback URLを調べ、legacy session数を報告する
9. Secret値を表示せず、参照Secret群のfingerprintを出力する
10. Ingress、control Service作成、session RBAC作成を無効にしたshadow valuesを生成する

blockが1件でもあればexit codeを非zeroにする。legacy sessionと旧Releaseが所有する共有RBACは
warningとして報告し、旧backendのuninstall前に手動で解消する。preflightが`READY`でも、表示された
commandはoperatorが内容を確認して個別に実行する。

shadow install後とcutover後には次を実行する。

```bash
agentapi-proxy helm migrate verify \
  --namespace agentapi-ui \
  --backend-release agentapi-proxy \
  --target-release ccplant
```

`verify`はtarget Helm Release、backend/frontend Deployment、Service Endpoint、Kubernetes API
proxy経由のbackend `/health`、`control` selector、session callback、runtime Secret/PVC数を検査する。
`control`が旧backendを向いていれば`shadow`、新backendを向いていれば`cutover`と判定する。

workerのLease名はRelease名に依存しない次の固定名を使う。旧・新backendを同じnamespaceで
並行起動すると同じLeaseを競うため、各workerのleaderは常に1 Podだけになる。

- `agentapi-schedule-worker`
- `agentapi-slackbot-cleanup-worker`
- `agentapi-stock-inventory-worker`
- `agentapi-session-allocator`

有効なworkerについてLeaseの存在とholderも検査する。Lease duration内では旧leaderが処理を継続し、
更新停止後に新Podが引き継ぐため、切替直後に必ず新ReleaseのPodがleaderになるとは限らない。

## 移行手順

1. stagingでproduction相当のresource構成を再現してrollbackまで試験
2. productionで`doctor`と`helm migrate plan`を実行
3. Secret/PVC backupを取得
4. `ccplant`をshadow installして`helm migrate verify`を実行
5. routingをcutoverし、再度`helm migrate verify`を実行
6. 旧frontendを削除し、旧backendをlegacy callback用にdrain
7. legacy sessionがなくなったら共通RBACを引き継ぎ、旧backendを削除
8. 問題がなければ`finalize`

通常の停止はIngress/routing反映中の既存接続再確立だけに限定する。Podを先にReadyにするため、
新規HTTP requestはほぼ無停止で切り替えられる。SSE/WebSocketはclient再接続を前提とする。

## テスト戦略

- golden: values変換、shadow resource名、共有RBAC、Ingress無効化
- kind: 新旧backendの並行稼働と既存sessionの列挙・操作
- kind: routing切り替え中の連続HTTP probeとSSE再接続
- kind: settings/credentials/API token/schedule等の再読込
- kind: session Pod再起動後のprovision成功
- kind: workerの旧→新handoverで二重実行されないこと
- kind: legacy session drain後に旧backendを削除できること
- PVC: retain、snapshot/restore、blockの各経路
- fault injection: 各uninstall後、install失敗、rollout timeout時のrollback
- version matrix: サポート対象の直近2 minorから最新ccplantへ移行

## 非対応

- namespaceをまたぐ移行
- namespace削除を伴う移行
- application data schemaのmigrationを同時に行う移行
- storage class変更やPVC resizeを同時に行う移行
- 複数の同一component Releaseを1つへ統合する移行

これらは別途データ移送を伴うblue/green手順を使用する。
