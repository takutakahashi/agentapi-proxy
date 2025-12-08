# KubernetesSessionManager 設計書

## 概要

`KubernetesSessionManager` は、既存の `SessionManager` インターフェースを実装し、セッションごとに独立した Kubernetes Pod を作成・管理するコンポーネントである。

現在の `LocalSessionManager` がローカルプロセスとしてセッションを管理するのに対し、`KubernetesSessionManager` は Kubernetes API を使用して Pod を動的に作成・削除することで、より高いスケーラビリティと分離性を実現する。

## 現状分析

### 既存アーキテクチャ

```
┌─────────────────────────────────────────────┐
│  Current: LocalSessionManager               │
│  ┌───────────────────────────────────────┐  │
│  │ Single Pod (agentapi-proxy)           │  │
│  │ ┌─────────────────────────────────┐   │  │
│  │ │ LocalSessionManager             │   │  │
│  │ │   ├─ Session A (exec.Cmd)       │   │  │
│  │ │   ├─ Session B (exec.Cmd)       │   │  │
│  │ │   └─ Session N (exec.Cmd)       │   │  │
│  │ └─────────────────────────────────┘   │  │
│  └───────────────────────────────────────┘  │
└─────────────────────────────────────────────┘
```

**課題:**
- 全セッションが単一 Pod 内で実行されるため、リソース競合が発生しやすい
- 1つのセッションがクラッシュすると、他のセッションに影響を与える可能性
- 水平スケーリングが困難
- セッションごとのリソース制限が難しい

### 目標アーキテクチャ

```
┌─────────────────────────────────────────────────────────────────────────┐
│  Target: KubernetesSessionManager                                       │
│  ┌───────────────────────────────────────────────────────────────────┐ │
│  │ Proxy Pod (agentapi-proxy)                                         │ │
│  │ ┌─────────────────────────────────┐                                │ │
│  │ │ KubernetesSessionManager        │                                │ │
│  │ │   ├─ Creates Deployment A ──────┼──► Deployment A (replicas=1)   │ │
│  │ │   │                             │    └─► Pod A                   │ │
│  │ │   │  Creates Service A ─────────┼──► Service A                   │ │
│  │ │   │  (selector: session-id=A)   │    (selector: session-id=A)    │ │
│  │ │   │  Creates PVC A ─────────────┼──► PVC A                       │ │
│  │ │   │                             │                                │ │
│  │ │   ├─ Creates Deployment B ──────┼──► Deployment B (replicas=1)   │ │
│  │ │   │                             │    └─► Pod B                   │ │
│  │ │   ...                           │                                │ │
│  │ └─────────────────────────────────┘                                │ │
│  └───────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────┘
```

**Deployment を使う理由:**
- Service の selector が Deployment の Pod template labels と一致
- session-id ラベルでリクエストが他セッションに混じらない
- Deployment が Pod のライフサイクルを管理（クラッシュ時の自動再起動など）

**メリット:**
- セッション間の完全な分離
- セッションごとのリソース制限（CPU, メモリ）
- 障害の局所化
- 動的スケーリング
- Kubernetes ネイティブの監視・ログ収集

## インターフェース

### SessionManager インターフェース（既存）

```go
type SessionManager interface {
    CreateSession(ctx context.Context, id string, req *RunServerRequest) (Session, error)
    GetSession(id string) Session
    ListSessions(filter SessionFilter) []Session
    DeleteSession(id string) error
    Shutdown(timeout time.Duration) error
}
```

### Session インターフェース（既存）

```go
type Session interface {
    ID() string
    Port() int
    UserID() string
    Tags() map[string]string
    Status() string
    StartedAt() time.Time
    Cancel()
}
```

## 詳細設計

### 1. KubernetesSessionManager 構造体

```go
package proxy

import (
    "context"
    "fmt"
    "sync"
    "time"

    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"

    "github.com/takutakahashi/agentapi-proxy/pkg/config"
    "github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

type KubernetesSessionManager struct {
    config     *config.Config
    k8sConfig  *KubernetesConfig
    client     kubernetes.Interface
    verbose    bool
    logger     *logger.Logger
    sessions   map[string]*kubernetesSession
    mutex      sync.RWMutex
    namespace  string
}

type KubernetesConfig struct {
    // Session Pod の設定
    Namespace       string            // Pod を作成する namespace
    Image           string            // Session Pod 用のコンテナイメージ
    ImagePullPolicy corev1.PullPolicy // イメージ pull ポリシー

    // リソース制限
    Resources       corev1.ResourceRequirements

    // ラベルとアノテーション
    Labels          map[string]string
    Annotations     map[string]string

    // ServiceAccount
    ServiceAccount  string

    // ネットワーク設定
    ServiceType     corev1.ServiceType // ClusterIP or NodePort
    BasePort        int                // Session Service のベースポート

    // Pod テンプレート設定
    NodeSelector    map[string]string
    Tolerations     []corev1.Toleration
    Affinity        *corev1.Affinity

    // セキュリティコンテキスト
    SecurityContext *corev1.PodSecurityContext

    // PVC 設定（必須）
    PVCStorageClass string // StorageClass 名（空の場合はデフォルト）
    PVCStorageSize  string // ストレージサイズ（例: "10Gi"）

    // タイムアウト設定
    PodStartTimeout time.Duration
    PodStopTimeout  time.Duration
}

// PVC 設定（必須）
type PVCConfig struct {
    StorageClassName string // StorageClass 名（空の場合はデフォルト）
    StorageSize      string // ストレージサイズ（例: "10Gi"）
}
```

### 2. kubernetesSession 構造体

```go
type kubernetesSession struct {
    id         string
    request    *RunServerRequest
    podName    string
    serviceName string
    servicePort int
    namespace  string
    startedAt  time.Time
    status     string
    cancelFunc context.CancelFunc
    mutex      sync.RWMutex
}

// Session インターフェースの実装
func (s *kubernetesSession) ID() string           { return s.id }
func (s *kubernetesSession) Port() int            { return s.servicePort }
func (s *kubernetesSession) UserID() string       { return s.request.UserID }
func (s *kubernetesSession) Tags() map[string]string { return s.request.Tags }
func (s *kubernetesSession) Status() string       { s.mutex.RLock(); defer s.mutex.RUnlock(); return s.status }
func (s *kubernetesSession) StartedAt() time.Time { return s.startedAt }
func (s *kubernetesSession) Cancel()              { if s.cancelFunc != nil { s.cancelFunc() } }
```

### 3. コンストラクタ

```go
func NewKubernetesSessionManager(
    cfg *config.Config,
    k8sCfg *KubernetesConfig,
    verbose bool,
    lgr *logger.Logger,
) (*KubernetesSessionManager, error) {
    // In-cluster config を使用
    restConfig, err := rest.InClusterConfig()
    if err != nil {
        return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
    }

    client, err := kubernetes.NewForConfig(restConfig)
    if err != nil {
        return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
    }

    return &KubernetesSessionManager{
        config:    cfg,
        k8sConfig: k8sCfg,
        client:    client,
        verbose:   verbose,
        logger:    lgr,
        sessions:  make(map[string]*kubernetesSession),
        namespace: k8sCfg.Namespace,
    }, nil
}
```

### 4. CreateSession 実装

```go
func (m *KubernetesSessionManager) CreateSession(
    ctx context.Context,
    id string,
    req *RunServerRequest,
) (Session, error) {
    // 1. セッションコンテキストを作成
    sessionCtx, cancel := context.WithCancel(context.Background())

    // 2. リソース名を生成
    podName := fmt.Sprintf("agentapi-session-%s", id)
    serviceName := fmt.Sprintf("agentapi-session-%s-svc", id)

    // 3. kubernetesSession を作成
    session := &kubernetesSession{
        id:          id,
        request:     req,
        podName:     podName,
        serviceName: serviceName,
        servicePort: m.k8sConfig.BasePort,
        namespace:   m.namespace,
        startedAt:   time.Now(),
        status:      "creating",
        cancelFunc:  cancel,
    }

    // 4. セッションを登録
    m.mutex.Lock()
    m.sessions[id] = session
    m.mutex.Unlock()

    // 5. PVC を作成（必須）
    if err := m.createPVC(ctx, session); err != nil {
        m.cleanupSession(id)
        return nil, fmt.Errorf("failed to create pvc: %w", err)
    }

    // 6. Pod を作成
    pod := m.buildPodSpec(session, req)
    _, err := m.client.CoreV1().Pods(m.namespace).Create(ctx, pod, metav1.CreateOptions{})
    if err != nil {
        m.deletePVC(ctx, session)
        m.cleanupSession(id)
        return nil, fmt.Errorf("failed to create pod: %w", err)
    }

    // 8. Service を作成
    svc := m.buildServiceSpec(session)
    _, err = m.client.CoreV1().Services(m.namespace).Create(ctx, svc, metav1.CreateOptions{})
    if err != nil {
        m.deletePod(ctx, podName)
        m.deletePVC(ctx, session)
        m.cleanupSession(id)
        return nil, fmt.Errorf("failed to create service: %w", err)
    }

    // 9. Pod の起動を待機（別 goroutine で監視）
    go m.watchSession(sessionCtx, session)

    // 10. ログを記録
    repository := ""
    if req.RepoInfo != nil {
        repository = req.RepoInfo.FullName
    }
    if err := m.logger.LogSessionStart(id, repository); err != nil {
        log.Printf("Failed to log session start: %v", err)
    }

    return session, nil
}
```

### 5. Pod 仕様の構築

```go
func (m *KubernetesSessionManager) buildPodSpec(
    session *kubernetesSession,
    req *RunServerRequest,
) *corev1.Pod {
    // 環境変数を構築
    envVars := m.buildEnvVars(session, req)

    // ラベルを構築
    labels := map[string]string{
        "app.kubernetes.io/name":       "agentapi-session",
        "app.kubernetes.io/instance":   session.id,
        "app.kubernetes.io/managed-by": "agentapi-proxy",
        "agentapi.proxy/session-id":    session.id,
        "agentapi.proxy/user-id":       req.UserID,
    }
    for k, v := range m.k8sConfig.Labels {
        labels[k] = v
    }
    // タグをラベルとして追加（k8s ラベル互換の文字列に変換）
    for k, v := range req.Tags {
        labels[fmt.Sprintf("agentapi.proxy/tag-%s", sanitizeLabelKey(k))] = sanitizeLabelValue(v)
    }

    pod := &corev1.Pod{
        ObjectMeta: metav1.ObjectMeta{
            Name:        session.podName,
            Namespace:   m.namespace,
            Labels:      labels,
            Annotations: m.k8sConfig.Annotations,
        },
        Spec: corev1.PodSpec{
            ServiceAccountName: m.k8sConfig.ServiceAccount,
            SecurityContext:    m.k8sConfig.SecurityContext,
            RestartPolicy:      corev1.RestartPolicyNever, // セッション Pod は再起動しない
            NodeSelector:       m.k8sConfig.NodeSelector,
            Tolerations:        m.k8sConfig.Tolerations,
            Affinity:           m.k8sConfig.Affinity,
            Containers: []corev1.Container{
                {
                    Name:            "agentapi",
                    Image:           m.k8sConfig.Image,
                    ImagePullPolicy: m.k8sConfig.ImagePullPolicy,
                    Ports: []corev1.ContainerPort{
                        {
                            Name:          "http",
                            ContainerPort: int32(m.k8sConfig.BasePort),
                            Protocol:      corev1.ProtocolTCP,
                        },
                    },
                    Env:       envVars,
                    Resources: m.k8sConfig.Resources,
                    LivenessProbe: &corev1.Probe{
                        ProbeHandler: corev1.ProbeHandler{
                            HTTPGet: &corev1.HTTPGetAction{
                                Path: "/health",
                                Port: intstr.FromInt(m.k8sConfig.BasePort),
                            },
                        },
                        InitialDelaySeconds: 30,
                        PeriodSeconds:       10,
                    },
                    ReadinessProbe: &corev1.Probe{
                        ProbeHandler: corev1.ProbeHandler{
                            HTTPGet: &corev1.HTTPGetAction{
                                Path: "/health",
                                Port: intstr.FromInt(m.k8sConfig.BasePort),
                            },
                        },
                        InitialDelaySeconds: 5,
                        PeriodSeconds:       5,
                    },
                },
            },
        },
    }

    return pod
}
```

### 6. Service 仕様の構築

```go
func (m *KubernetesSessionManager) buildServiceSpec(session *kubernetesSession) *corev1.Service {
    return &corev1.Service{
        ObjectMeta: metav1.ObjectMeta{
            Name:      session.serviceName,
            Namespace: m.namespace,
            Labels: map[string]string{
                "app.kubernetes.io/name":       "agentapi-session",
                "app.kubernetes.io/instance":   session.id,
                "app.kubernetes.io/managed-by": "agentapi-proxy",
                "agentapi.proxy/session-id":    session.id,
            },
        },
        Spec: corev1.ServiceSpec{
            Type: m.k8sConfig.ServiceType,
            Selector: map[string]string{
                "agentapi.proxy/session-id": session.id,
            },
            Ports: []corev1.ServicePort{
                {
                    Name:       "http",
                    Port:       int32(session.servicePort),
                    TargetPort: intstr.FromInt(m.k8sConfig.BasePort),
                    Protocol:   corev1.ProtocolTCP,
                },
            },
        },
    }
}
```

### 7. セッション監視

```go
func (m *KubernetesSessionManager) watchSession(ctx context.Context, session *kubernetesSession) {
    defer func() {
        // セッション終了時のクリーンアップ
        m.cleanupSession(session.id)
    }()

    // Pod の状態を監視
    watcher, err := m.client.CoreV1().Pods(m.namespace).Watch(ctx, metav1.ListOptions{
        FieldSelector: fmt.Sprintf("metadata.name=%s", session.podName),
    })
    if err != nil {
        log.Printf("Failed to watch pod %s: %v", session.podName, err)
        return
    }
    defer watcher.Stop()

    for {
        select {
        case <-ctx.Done():
            // コンテキストがキャンセルされた場合、Pod を削除
            m.deleteSessionResources(session)
            return

        case event, ok := <-watcher.ResultChan():
            if !ok {
                return
            }

            pod, ok := event.Object.(*corev1.Pod)
            if !ok {
                continue
            }

            // ステータスを更新
            session.mutex.Lock()
            switch pod.Status.Phase {
            case corev1.PodPending:
                session.status = "starting"
            case corev1.PodRunning:
                session.status = "active"
            case corev1.PodSucceeded, corev1.PodFailed:
                session.status = "stopped"
                session.mutex.Unlock()
                return
            }
            session.mutex.Unlock()
        }
    }
}
```

### 8. DeleteSession 実装

```go
func (m *KubernetesSessionManager) DeleteSession(id string) error {
    m.mutex.RLock()
    session, exists := m.sessions[id]
    m.mutex.RUnlock()

    if !exists {
        return fmt.Errorf("session not found: %s", id)
    }

    // キャンセルしてリソース削除をトリガー
    if session.cancelFunc != nil {
        session.cancelFunc()
    }

    // 削除完了を待機
    timeout := m.k8sConfig.PodStopTimeout
    if timeout == 0 {
        timeout = 30 * time.Second
    }

    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()

    // Pod と Service を削除
    if err := m.deleteSessionResources(session); err != nil {
        log.Printf("Warning: failed to delete session resources: %v", err)
    }

    // セッション登録を削除
    m.cleanupSession(id)

    // ログを記録
    if err := m.logger.LogSessionEnd(id, 0); err != nil {
        log.Printf("Failed to log session end: %v", err)
    }

    return nil
}

func (m *KubernetesSessionManager) deleteSessionResources(session *kubernetesSession) error {
    ctx := context.Background()
    deletePolicy := metav1.DeletePropagationForeground
    deleteOptions := metav1.DeleteOptions{
        PropagationPolicy: &deletePolicy,
    }

    // Service を削除
    err := m.client.CoreV1().Services(m.namespace).Delete(ctx, session.serviceName, deleteOptions)
    if err != nil && !errors.IsNotFound(err) {
        log.Printf("Failed to delete service %s: %v", session.serviceName, err)
    }

    // Pod を削除
    err = m.client.CoreV1().Pods(m.namespace).Delete(ctx, session.podName, deleteOptions)
    if err != nil && !errors.IsNotFound(err) {
        log.Printf("Failed to delete pod %s: %v", session.podName, err)
    }

    // PVC を削除
    if err := m.deletePVC(ctx, session); err != nil && !errors.IsNotFound(err) {
        log.Printf("Failed to delete pvc for session %s: %v", session.id, err)
    }

    return nil
}
```

### 9. ListSessions / GetSession 実装

```go
func (m *KubernetesSessionManager) GetSession(id string) Session {
    m.mutex.RLock()
    defer m.mutex.RUnlock()

    session, exists := m.sessions[id]
    if !exists {
        return nil
    }
    return session
}

func (m *KubernetesSessionManager) ListSessions(filter SessionFilter) []Session {
    m.mutex.RLock()
    defer m.mutex.RUnlock()

    var result []Session
    for _, session := range m.sessions {
        // User ID フィルタ
        if filter.UserID != "" && session.request.UserID != filter.UserID {
            continue
        }

        // Status フィルタ
        if filter.Status != "" && session.Status() != filter.Status {
            continue
        }

        // Tag フィルタ
        if len(filter.Tags) > 0 {
            matchAllTags := true
            for tagKey, tagValue := range filter.Tags {
                if sessionTagValue, exists := session.request.Tags[tagKey]; !exists || sessionTagValue != tagValue {
                    matchAllTags = false
                    break
                }
            }
            if !matchAllTags {
                continue
            }
        }

        result = append(result, session)
    }

    return result
}
```

### 10. Shutdown 実装

```go
func (m *KubernetesSessionManager) Shutdown(timeout time.Duration) error {
    m.mutex.RLock()
    sessions := make([]*kubernetesSession, 0, len(m.sessions))
    for _, session := range m.sessions {
        sessions = append(sessions, session)
    }
    m.mutex.RUnlock()

    log.Printf("Shutting down, terminating %d session pods...", len(sessions))

    if len(sessions) == 0 {
        return nil
    }

    // 全セッションを並列で削除
    var wg sync.WaitGroup
    for _, session := range sessions {
        wg.Add(1)
        go func(s *kubernetesSession) {
            defer wg.Done()
            if s.cancelFunc != nil {
                s.cancelFunc()
            }
            m.deleteSessionResources(s)
        }(session)
    }

    // タイムアウト付きで待機
    done := make(chan struct{})
    go func() {
        wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        log.Printf("All session pods terminated")
        return nil
    case <-time.After(timeout):
        log.Printf("Shutdown timeout reached")
        return fmt.Errorf("shutdown timeout")
    }
}
```

## ネットワーキング設計

### Session Pod へのルーティング

Proxy Pod から Session Pod へのルーティングは、Kubernetes Service を経由して行う。

```
Client Request
    │
    ▼
┌─────────────────────────┐
│  Ingress                │
└────────────┬────────────┘
             │
             ▼
┌─────────────────────────┐
│  agentapi-proxy Service │
│  (ClusterIP:8080)       │
└────────────┬────────────┘
             │
             ▼
┌─────────────────────────┐
│  agentapi-proxy Pod     │
│  KubernetesSessionMgr   │
│  ┌───────────────────┐  │
│  │ Reverse Proxy     │──┼──► agentapi-session-{id}-svc:9000
│  │ (session routing) │  │         │
│  └───────────────────┘  │         ▼
└─────────────────────────┘    ┌────────────────────┐
                               │ Session Pod        │
                               │ agentapi:9000      │
                               └────────────────────┘
```

### セッションへのリクエストルーティング

既存の `session_handlers.go` の `RouteToSession` メソッドを修正し、Service 経由でルーティング:

```go
// session_handlers.go の修正
func (h *SessionHandlers) RouteToSession(c echo.Context) error {
    sessionID := c.Param("sessionId")
    session := h.sessionManager.GetSession(sessionID)

    if session == nil {
        return c.JSON(http.StatusNotFound, map[string]string{
            "error": "Session not found",
        })
    }

    // KubernetesSession の場合、Service DNS 名を使用
    var targetURL string
    if ks, ok := session.(*kubernetesSession); ok {
        // Service DNS: {service-name}.{namespace}.svc.cluster.local:{port}
        targetURL = fmt.Sprintf("http://%s.%s.svc.cluster.local:%d",
            ks.serviceName, ks.namespace, ks.servicePort)
    } else {
        // LocalSession の場合（後方互換性）
        targetURL = fmt.Sprintf("http://localhost:%d", session.Port())
    }

    // リバースプロキシを使用
    proxy := httputil.NewSingleHostReverseProxy(targetURL)
    proxy.ServeHTTP(c.Response(), c.Request())
    return nil
}
```

## 設定

### 環境変数ベースの設定

```go
// pkg/config/config.go に追加
type KubernetesSessionConfig struct {
    Enabled          bool   `mapstructure:"enabled"`
    Namespace        string `mapstructure:"namespace"`
    Image            string `mapstructure:"image"`
    ImagePullPolicy  string `mapstructure:"image_pull_policy"`
    ServiceAccount   string `mapstructure:"service_account"`
    BasePort         int    `mapstructure:"base_port"`
    PodStartTimeout  int    `mapstructure:"pod_start_timeout"`  // 秒
    PodStopTimeout   int    `mapstructure:"pod_stop_timeout"`   // 秒

    // リソース制限
    CPURequest    string `mapstructure:"cpu_request"`
    CPULimit      string `mapstructure:"cpu_limit"`
    MemoryRequest string `mapstructure:"memory_request"`
    MemoryLimit   string `mapstructure:"memory_limit"`
}
```

### 環境変数

| 環境変数 | 説明 | デフォルト値 |
|---------|------|------------|
| `AGENTAPI_K8S_SESSION_ENABLED` | Kubernetes Session Manager を有効化 | `false` |
| `AGENTAPI_K8S_SESSION_NAMESPACE` | Session Pod を作成する namespace | 現在の namespace |
| `AGENTAPI_K8S_SESSION_IMAGE` | Session Pod 用イメージ | 現在の Proxy イメージ |
| `AGENTAPI_K8S_SESSION_SERVICE_ACCOUNT` | Session Pod 用 ServiceAccount | `default` |
| `AGENTAPI_K8S_SESSION_BASE_PORT` | Session の listen ポート | `9000` |
| `AGENTAPI_K8S_SESSION_CPU_REQUEST` | CPU リクエスト | `500m` |
| `AGENTAPI_K8S_SESSION_CPU_LIMIT` | CPU リミット | `2` |
| `AGENTAPI_K8S_SESSION_MEMORY_REQUEST` | メモリリクエスト | `512Mi` |
| `AGENTAPI_K8S_SESSION_MEMORY_LIMIT` | メモリリミット | `4Gi` |
| `AGENTAPI_K8S_SESSION_PVC_STORAGE_CLASS` | PVC の StorageClass | `""` (デフォルト) |
| `AGENTAPI_K8S_SESSION_PVC_STORAGE_SIZE` | PVC のストレージサイズ | `10Gi` |
| `AGENTAPI_K8S_SESSION_POD_START_TIMEOUT` | Pod 起動タイムアウト（秒） | `120` |
| `AGENTAPI_K8S_SESSION_POD_STOP_TIMEOUT` | Pod 停止タイムアウト（秒） | `30` |

## RBAC 要件

Session Pod を管理するために、agentapi-proxy の ServiceAccount に以下の権限が必要:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: agentapi-proxy-session-manager
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch", "create", "delete"]
  - apiGroups: [""]
    resources: ["services"]
    verbs: ["get", "list", "create", "delete"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
  - apiGroups: [""]
    resources: ["persistentvolumeclaims"]
    verbs: ["get", "list", "create", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: agentapi-proxy-session-manager
subjects:
  - kind: ServiceAccount
    name: agentapi-proxy
roleRef:
  kind: Role
  name: agentapi-proxy-session-manager
  apiGroup: rbac.authorization.k8s.io
```

## ファイル構成

```
pkg/
├── proxy/
│   ├── session.go                    # 既存: インターフェース定義
│   ├── local_session_manager.go      # 既存: ローカル実装
│   ├── kubernetes_session_manager.go # 新規: Kubernetes 実装
│   ├── kubernetes_session.go         # 新規: kubernetesSession 構造体
│   └── session_handlers.go           # 修正: ルーティング対応
├── config/
│   └── config.go                     # 修正: K8s 設定追加
└── ...

helm/
└── agentapi-proxy/
    ├── templates/
    │   ├── role.yaml                 # 新規: RBAC Role
    │   ├── rolebinding.yaml          # 新規: RBAC RoleBinding
    │   └── statefulset.yaml          # 修正: K8s session 設定追加
    └── values.yaml                   # 修正: K8s session 設定追加
```

## 実装フェーズ

### Phase 1: 基本実装
1. `kubernetes.Interface` を使った基本的な KubernetesSessionManager の実装
2. Pod/Service の作成・削除機能
3. セッション状態の監視

### Phase 2: Proxy 統合
1. config への設定追加
2. Proxy の初期化ロジック修正
3. SessionHandlers のルーティング対応

### Phase 3: Helm Chart 更新
1. RBAC リソースの追加
2. values.yaml への設定追加
3. StatefulSet テンプレートの更新

### Phase 4: テスト
1. Unit テスト（mock kubernetes client）
2. Integration テスト（kind/minikube）
3. E2E テスト

## 依存関係の追加

`go.mod` に以下を追加:

```
k8s.io/api v0.29.0
k8s.io/apimachinery v0.29.0
k8s.io/client-go v0.29.0
```

## 考慮事項

### Session Pod のイメージ

Session Pod では agentapi サーバーのみを実行する。現在の Docker イメージ（agentapi-proxy）には agentapi も含まれているため、同じイメージを使用可能。ただし、将来的には Session 専用の軽量イメージを作成することを検討。

### 起動時の復元

KubernetesSessionManager は起動時に、既存の Session Pod を検出してセッション情報を復元する機能を実装する必要がある:

```go
func (m *KubernetesSessionManager) restoreExistingSessions(ctx context.Context) error {
    pods, err := m.client.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{
        LabelSelector: "app.kubernetes.io/managed-by=agentapi-proxy",
    })
    if err != nil {
        return err
    }

    for _, pod := range pods.Items {
        sessionID := pod.Labels["agentapi.proxy/session-id"]
        if sessionID != "" {
            // セッション情報を復元
            m.restoreSession(&pod)
        }
    }

    return nil
}
```

### 永続ボリューム（必須）

セッションごとに PVC を作成し、作業ディレクトリを永続化する。これにより:
- セッション中のコード変更が保持される
- Pod の再起動時にもデータが失われない
- セッション終了時に PVC も削除される

```go
func (m *KubernetesSessionManager) createPVC(ctx context.Context, session *kubernetesSession) error {
    pvc := &corev1.PersistentVolumeClaim{
        ObjectMeta: metav1.ObjectMeta{
            Name:      fmt.Sprintf("agentapi-session-%s-workdir", session.id),
            Namespace: m.namespace,
            Labels: map[string]string{
                "app.kubernetes.io/name":       "agentapi-session",
                "app.kubernetes.io/instance":   session.id,
                "app.kubernetes.io/managed-by": "agentapi-proxy",
                "agentapi.proxy/session-id":    session.id,
            },
        },
        Spec: corev1.PersistentVolumeClaimSpec{
            AccessModes: []corev1.PersistentVolumeAccessMode{
                corev1.ReadWriteOnce,
            },
            StorageClassName: &m.k8sConfig.PVCStorageClass,
            Resources: corev1.ResourceRequirements{
                Requests: corev1.ResourceList{
                    corev1.ResourceStorage: resource.MustParse(m.k8sConfig.PVCStorageSize),
                },
            },
        },
    }

    _, err := m.client.CoreV1().PersistentVolumeClaims(m.namespace).Create(ctx, pvc, metav1.CreateOptions{})
    return err
}

func (m *KubernetesSessionManager) deletePVC(ctx context.Context, session *kubernetesSession) error {
    pvcName := fmt.Sprintf("agentapi-session-%s-workdir", session.id)
    return m.client.CoreV1().PersistentVolumeClaims(m.namespace).Delete(
        ctx, pvcName, metav1.DeleteOptions{})
}
```

Pod 仕様に PVC をマウント:

```go
// buildPodSpec 内で Volume と VolumeMount を追加
pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
    Name: "workdir",
    VolumeSource: corev1.VolumeSource{
        PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
            ClaimName: fmt.Sprintf("agentapi-session-%s-workdir", session.id),
        },
    },
})

pod.Spec.Containers[0].VolumeMounts = append(
    pod.Spec.Containers[0].VolumeMounts,
    corev1.VolumeMount{
        Name:      "workdir",
        MountPath: "/home/agentapi/workdir",
    },
)
```

## まとめ

KubernetesSessionManager は、既存の SessionManager インターフェースを実装することで、コードベースへの影響を最小限に抑えつつ、Kubernetes ネイティブなセッション管理を実現する。これにより、セッション間の完全な分離、リソース制限、障害の局所化といったメリットが得られる。
