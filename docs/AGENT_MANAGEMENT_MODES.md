# エージェント管理モード (Agent Management Modes)

agentapi-proxyは、エージェントを2つの異なる方法で管理することができます。

## 概要

agentapi-proxyは2つの動作モードをサポートしています：

1. **ローカルプロビジョンモード** (デフォルト): エージェントをローカルプロセスとして管理
2. **K8sモード**: エージェントをKubernetes StatefulSetとして管理

## K8sモードの利点

- **スケーラビリティ**: Kubernetesの自動スケーリング機能
- **堅牢性**: Pod再起動、ヘルスチェック、リソース制限
- **永続化**: PersistentVolumeClaimによるデータ永続化
- **分離**: エージェントごとの専用StatefulSet
- **監視**: Kubernetesネイティブな監視とログ収集

## 設定方法

### 環境変数

```bash
# K8sモードを有効化
export AGENTAPI_K8S_MODE_ENABLED=true

# Kubernetesの設定
export AGENTAPI_K8S_MODE_NAMESPACE=agentapi-proxy
export AGENTAPI_K8S_MODE_IMAGE=agentapi-proxy:latest

# リソース設定
export AGENTAPI_K8S_MODE_RESOURCES_CPU_REQUEST=100m
export AGENTAPI_K8S_MODE_RESOURCES_CPU_LIMIT=500m
export AGENTAPI_K8S_MODE_RESOURCES_MEMORY_REQUEST=256Mi
export AGENTAPI_K8S_MODE_RESOURCES_MEMORY_LIMIT=512Mi
export AGENTAPI_K8S_MODE_RESOURCES_STORAGE_SIZE=1Gi
```

### 設定ファイル (JSON)

```json
{
  "k8s_mode": {
    "enabled": true,
    "namespace": "agentapi-proxy",
    "image": "agentapi-proxy:latest",
    "resources": {
      "cpu_request": "100m",
      "cpu_limit": "500m",
      "memory_request": "256Mi",
      "memory_limit": "512Mi",
      "storage_size": "1Gi"
    }
  }
}
```

### 設定ファイル (YAML)

```yaml
k8s_mode:
  enabled: true
  namespace: agentapi-proxy
  image: agentapi-proxy:latest
  resources:
    cpu_request: 100m
    cpu_limit: 500m
    memory_request: 256Mi
    memory_limit: 512Mi
    storage_size: 1Gi
```

## 設定項目

| 項目 | 説明 | デフォルト値 |
|------|------|-------------|
| `enabled` | K8sモードの有効/無効 | `false` |
| `namespace` | エージェント用Kubernetesネームスペース | `agentapi-proxy` |
| `image` | エージェントコンテナイメージ | `agentapi-proxy:latest` |
| `resources.cpu_request` | CPU要求値 | `100m` |
| `resources.cpu_limit` | CPU制限値 | `500m` |
| `resources.memory_request` | メモリ要求値 | `256Mi` |
| `resources.memory_limit` | メモリ制限値 | `512Mi` |
| `resources.storage_size` | ストレージサイズ | `1Gi` |

## 前提条件

K8sモードを使用するには、以下が必要です：

1. **Kubernetes クラスター**: 適切に設定されたKubernetesクラスター
2. **RBAC権限**: StatefulSet、Service、ConfigMap、PVCの作成・削除権限
3. **イメージ**: 指定したコンテナイメージがクラスターからアクセス可能
4. **ストレージクラス**: PVCが作成できるStorageClass

## アーキテクチャ

K8sモードでは、各エージェントは以下のKubernetesリソースを作成します：

### Agent-per-StatefulSet パターン
- **StatefulSet**: 各エージェントに専用のStatefulSet (replicas=1)
- **Headless Service**: StatefulSetのネットワーク管理用
- **PersistentVolumeClaim**: エージェントデータの永続化
- **ConfigMap**: エージェント設定の管理

### リソース命名規則
- StatefulSet: `agent-{agentID}`
- Service: `agent-{agentID}-headless` 
- Pod: `agent-{agentID}-0`
- PVC: `data-agent-{agentID}-0`

## モード切り替え

### ローカルプロビジョンモードからK8sモードへ
1. 既存のエージェントを停止
2. 設定でK8sモードを有効化
3. agentapi-proxyを再起動
4. 新しいエージェントはStatefulSetとして作成される

### K8sモードからローカルプロビジョンモードへ
1. 既存のStatefulSetを削除
2. 設定でK8sモードを無効化
3. agentapi-proxyを再起動
4. 新しいエージェントはプロセスとして作成される

## トラブルシューティング

### よくある問題

1. **Pod作成失敗**
   - 権限不足: RBAC設定を確認
   - イメージプル失敗: ImagePullPolicyとレジストリアクセスを確認

2. **PVC作成失敗**
   - StorageClassが存在しない
   - 十分なストレージ容量がない

3. **エージェントに接続できない**
   - Service設定を確認
   - ネットワークポリシーを確認

### ログ確認

```bash
# agentapi-proxyのログ
kubectl logs deployment/agentapi-proxy

# エージェントPodのログ  
kubectl logs agent-{agentID}-0

# StatefulSetの状態確認
kubectl get statefulset agent-{agentID}

# PVCの状態確認
kubectl get pvc data-agent-{agentID}-0
```

## 例

### 基本的な使用例

```bash
# K8sモードを有効化
export AGENTAPI_K8S_MODE_ENABLED=true

# agentapi-proxyを起動
./agentapi-proxy

# エージェントを作成 (StatefulSetとして作成される)
curl -X POST http://localhost:8080/sessions/my-session/agents
```

### カスタム設定の例

```bash
# 高リソース設定でK8sモード
export AGENTAPI_K8S_MODE_ENABLED=true
export AGENTAPI_K8S_MODE_RESOURCES_CPU_REQUEST=500m
export AGENTAPI_K8S_MODE_RESOURCES_CPU_LIMIT=2000m
export AGENTAPI_K8S_MODE_RESOURCES_MEMORY_REQUEST=1Gi
export AGENTAPI_K8S_MODE_RESOURCES_MEMORY_LIMIT=2Gi
export AGENTAPI_K8S_MODE_RESOURCES_STORAGE_SIZE=5Gi
```