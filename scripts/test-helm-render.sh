#!/usr/bin/env bash
set -euo pipefail

HELM_BIN="${HELM:-helm}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

"$HELM_BIN" template backend "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --namespace default >"$TMP_DIR/backend-default.yaml"
"$HELM_BIN" template frontend "$REPO_ROOT/frontend/helm/agentapi-ui" >"$TMP_DIR/frontend-default.yaml"
"$HELM_BIN" template ccplant "$REPO_ROOT/chart/ccplant" >"$TMP_DIR/ccplant-default.yaml"

assert_contains() {
  local pattern="$1"
  local file="$2"
  if ! grep -Eq "$pattern" "$file"; then
    echo "expected pattern not found: $pattern ($file)" >&2
    exit 1
  fi
}

assert_not_contains() {
  local pattern="$1"
  local file="$2"
  if grep -Eq "$pattern" "$file"; then
    echo "unexpected pattern found: $pattern ($file)" >&2
    exit 1
  fi
}

# A default install is API-only. It must not receive workload RBAC or any
# worker/session-manager configuration.
assert_contains '^  replicas: 1$' "$TMP_DIR/backend-default.yaml"
assert_contains '^  name: "?control"?$' "$TMP_DIR/backend-default.yaml"
assert_contains 'helm.sh/resource-policy: keep' "$TMP_DIR/backend-default.yaml"
assert_contains 'serviceAccountName: backend-agentapi-proxy' "$TMP_DIR/backend-default.yaml"
assert_contains 'automountServiceAccountToken: false' "$TMP_DIR/backend-default.yaml"
assert_contains 'image: "ghcr.io/ccplant/ccplant-api:1.173.0"' "$TMP_DIR/backend-default.yaml"
assert_contains 'args: \["server"\]' "$TMP_DIR/backend-default.yaml"
assert_not_contains '^kind: Role$' "$TMP_DIR/backend-default.yaml"
assert_not_contains '^kind: RoleBinding$' "$TMP_DIR/backend-default.yaml"
assert_not_contains 'AGENTAPI_K8S_SESSION_' "$TMP_DIR/backend-default.yaml"
assert_not_contains 'AGENTAPI_WORKER_CONTROL_' "$TMP_DIR/backend-default.yaml"
assert_not_contains 'AGENTAPI_SESSION_MANAGER_' "$TMP_DIR/backend-default.yaml"

# The umbrella chart keeps the full image as the session/runtime fallback while
# selecting the lightweight image only for the API Deployment.
assert_contains 'image: "ghcr.io/ccplant/ccplant-api:1.173.0"' "$TMP_DIR/ccplant-default.yaml"

"$HELM_BIN" template backend-worker-default "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --set worker.enabled=true \
  --set api.workerControl.tokenSecretRef.name=worker-control \
  --set worker.controlApi.tokenSecretRef.name=worker-control \
  --set worker.kvStore.databaseUrl=file:///tmp/worker.db \
  --set worker.redis.addr=redis:6379 >"$TMP_DIR/backend-worker-default.yaml"
assert_contains 'image: "ghcr.io/ccplant/ccplant-api:1.173.0"' "$TMP_DIR/backend-worker-default.yaml"

# Role image tags are release-controlled by Chart.appVersion and are not valid
# Helm values.
if "$HELM_BIN" template backend-image-tag-override "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --set api.image.tag=untrusted >"$TMP_DIR/backend-image-tag-override.yaml" 2>"$TMP_DIR/backend-image-tag-override.err"; then
  echo "api.image.tag unexpectedly accepted" >&2
  exit 1
fi
assert_contains 'Additional property tag is not allowed' "$TMP_DIR/backend-image-tag-override.err"

# Optional controllers and persistent components are disabled in the minimal defaults.
assert_not_contains 'name: AGENTAPI_SCHEDULE_WORKER_ENABLED' "$TMP_DIR/backend-default.yaml"
assert_not_contains 'app.kubernetes.io/component: worker' "$TMP_DIR/backend-default.yaml"
assert_not_contains '^kind: PersistentVolumeClaim$' "$TMP_DIR/ccplant-default.yaml"
assert_not_contains 'app.kubernetes.io/component: scia' "$TMP_DIR/ccplant-default.yaml"
assert_not_contains 'app.kubernetes.io/component: asset' "$TMP_DIR/ccplant-default.yaml"

# The frontend must configure the environment name consumed by the application
# and derive http URLs when TLS is disabled.
assert_contains 'name: ALLOWED_ORIGINS' "$TMP_DIR/frontend-default.yaml"
assert_contains 'value: "http://agentapi-ui.local"' "$TMP_DIR/frontend-default.yaml"
assert_not_contains 'name: NEXT_PUBLIC_ALLOWED_ORIGINS' "$TMP_DIR/frontend-default.yaml"
assert_contains 'key: cookie-encryption-secret' "$TMP_DIR/frontend-default.yaml"

# Sensitive runtime values can be supplied without being stored in Helm values.
"$HELM_BIN" template backend-secrets "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --set config.auth.github.oauth.clientSecretRef.name=backend-runtime \
  --set config.auth.github.oauth.clientSecretRef.key=oauth-client-secret \
  --set github.tokenRef.name=backend-runtime \
  --set config.vapid.privateKeyRef.name=backend-runtime >"$TMP_DIR/backend-secrets.yaml"
assert_contains 'name: "backend-runtime"' "$TMP_DIR/backend-secrets.yaml"
assert_contains 'key: "oauth-client-secret"' "$TMP_DIR/backend-secrets.yaml"

# OpenTelemetry metadata comes from values while endpoint and auth headers are
# loaded from an existing Secret. Pod and release identity are added automatically.
"$HELM_BIN" template backend-otel "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --set observability.openTelemetry.enabled=true \
  --set observability.openTelemetry.deploymentEnvironment=development \
  --set observability.openTelemetry.secretRef.name=grafana-cloud-otlp >"$TMP_DIR/backend-otel.yaml"
assert_contains 'name: OTEL_EXPORTER_OTLP_ENDPOINT' "$TMP_DIR/backend-otel.yaml"
assert_contains 'name: "grafana-cloud-otlp"' "$TMP_DIR/backend-otel.yaml"
assert_contains 'key: "OTEL_EXPORTER_OTLP_HEADERS"' "$TMP_DIR/backend-otel.yaml"
assert_contains 'fieldPath: metadata.name' "$TMP_DIR/backend-otel.yaml"
assert_contains 'deployment.environment=development,service.namespace=ccplant,service.version=1.173.0,service.instance.id=\$\(POD_NAME\)' "$TMP_DIR/backend-otel.yaml"

# Upgrades from chart versions that predate observability values may reuse a
# values object without that key. Rendering must remain backward compatible.
"$HELM_BIN" template backend-legacy "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --set-json 'observability=null' >"$TMP_DIR/backend-legacy.yaml"
assert_not_contains 'name: OTEL_EXPORTER_OTLP_ENDPOINT' "$TMP_DIR/backend-legacy.yaml"

"$HELM_BIN" template frontend-secrets "$REPO_ROOT/frontend/helm/agentapi-ui" \
  --set envFrom[0].secretRef.name=frontend-runtime >"$TMP_DIR/frontend-secrets.yaml"
assert_contains 'name: frontend-runtime' "$TMP_DIR/frontend-secrets.yaml"

# Render all three process roles from independent values. Each role gets a
# unique image, command, ServiceAccount, credential audience, and env allowlist.
all_role_args=(
  --namespace separation-test
  --set api.image.repository=example/api
  --set api.kvStore.databaseUrlSecretRef.name=api-libsql
  --set api.redis.addr=redis-api.example:6380
  --set api.redis.db=4
  --set api.redis.tlsEnabled=true
  --set api.encryption.keySecretRef.name=shared-encryption
  --set api.workerControl.tokenSecretRef.name=worker-control
  --set api.sessionManager.url=http://backend-session-manager.separation-test.svc.cluster.local:8080
  --set api.sessionManager.tokenSecretRef.name=manager-internal
  --set worker.enabled=true
  --set worker.image.repository=example/worker
  --set worker.controlApi.tokenSecretRef.name=worker-control
  --set worker.kvStore.databaseUrlSecretRef.name=worker-libsql
  --set worker.redis.addr=redis.example:6380
  --set worker.redis.db=5
  --set worker.redis.tlsEnabled=true
  --set worker.redis.dialTimeout=9s
  --set worker.redis.readTimeout=8s
  --set worker.redis.writeTimeout=7s
  --set worker.stockInventory.enabled=true
  --set worker.stockInventory.pools[0].targetCount=3
  --set worker.stockInventory.pools[0].dockerEnabled=true
  --set sessionManager.enabled=true
  --set sessionManager.image.repository=example/session-manager
  --set sessionManager.internalApi.tokenSecretRef.name=manager-internal
  --set sessionManager.encryption.keySecretRef.name=shared-encryption
  --set sessionManager.kvStore.databaseUrlSecretRef.name=manager-libsql
  --set sessionManager.redis.addr=redis.example:6380
  --set sessionManager.sessionPersistence.backend=s3
  --set sessionManager.sessionPersistence.s3.bucket=manager-sessions
  --set sessionManager.sessionControl.enabled=true
  --set sessionManager.sessionControl.directRuntimeEnabled=true
  --set sessionControl.enabled=true
  --set sessionControl.directRuntimeEnabled=true
  --set sessionManager.scia.enabled=true
  --set scia.enabled=true
  --set sessionManager.scia.publicBaseUrl=https://api.example
  --set sessionManager.github.tokenRef.name=manager-github
  --set sessionManager.kubernetesSession.provisioner.tokenSecretRef.name=provisioner
)
"$HELM_BIN" template backend "$REPO_ROOT/backend/helm/agentapi-proxy" \
  "${all_role_args[@]}" >"$TMP_DIR/backend-all-roles.yaml"
for template in deployment worker-deployment session-manager-deployment; do
  "$HELM_BIN" template backend "$REPO_ROOT/backend/helm/agentapi-proxy" \
    --show-only "templates/${template}.yaml" "${all_role_args[@]}" \
    >"$TMP_DIR/backend-${template}.yaml"
done
"$HELM_BIN" template backend "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --show-only templates/rolebinding.yaml "${all_role_args[@]}" \
  >"$TMP_DIR/backend-manager-rolebinding.yaml"

# A parent-registered Kubernetes session manager is stateless and may run with
# multiple replicas without a remote Redis. Kubernetes Lease elects its single
# upstream allocation/control worker.
"$HELM_BIN" template backend "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --show-only templates/session-manager-deployment.yaml \
  "${all_role_args[@]}" \
  --set sessionManager.replicaCount=2 \
  --set-string sessionManager.redis.addr= \
  --set sessionManager.externalRegistration.enabled=true \
  --set sessionManager.externalRegistration.upstreamUrl=https://parent.example \
  --set sessionManager.externalRegistration.connectionTokenSecretRef.name=manager-connection \
  --set sessionManager.externalRegistration.hmacSecretRef.name=manager-hmac \
  >"$TMP_DIR/backend-remote-manager-no-redis.yaml"

assert_contains 'image: "example/api:1.173.0"' "$TMP_DIR/backend-deployment.yaml"
assert_contains 'serviceAccountName: backend-agentapi-proxy' "$TMP_DIR/backend-deployment.yaml"
assert_contains 'automountServiceAccountToken: false' "$TMP_DIR/backend-deployment.yaml"
assert_contains 'args: \["server"\]' "$TMP_DIR/backend-deployment.yaml"
assert_contains 'name: AGENTAPI_WORKER_CONTROL_TOKEN' "$TMP_DIR/backend-deployment.yaml"
assert_contains 'name: AGENTAPI_SESSION_MANAGER_API_URL' "$TMP_DIR/backend-deployment.yaml"
assert_contains 'name: AGENTAPI_SESSION_MANAGER_API_TOKEN' "$TMP_DIR/backend-deployment.yaml"
assert_contains 'replicas: 2' "$TMP_DIR/backend-remote-manager-no-redis.yaml"
assert_not_contains 'name: AGENTAPI_REDIS_ADDR' "$TMP_DIR/backend-remote-manager-no-redis.yaml"
assert_contains 'resources: \["leases"\]' "$TMP_DIR/backend-all-roles.yaml"
assert_contains 'name: SESSION_CONTROL_LONG_POLL_ENABLED' "$TMP_DIR/backend-deployment.yaml"
assert_contains 'name: AGENTAPI_DIRECT_SESSION_RUNTIME_ENABLED' "$TMP_DIR/backend-deployment.yaml"
assert_contains 'value: "true"' "$TMP_DIR/backend-deployment.yaml"
assert_contains 'name: "worker-control"' "$TMP_DIR/backend-deployment.yaml"
assert_contains 'name: "manager-internal"' "$TMP_DIR/backend-deployment.yaml"
assert_contains 'name: AGENTAPI_KV_STORE_DATABASE_URL' "$TMP_DIR/backend-deployment.yaml"
assert_contains 'name: "api-libsql"' "$TMP_DIR/backend-deployment.yaml"
assert_contains 'name: AGENTAPI_REDIS_DB, value: "4"' "$TMP_DIR/backend-deployment.yaml"
assert_contains 'name: AGENTAPI_REDIS_TLS_ENABLED, value: "true"' "$TMP_DIR/backend-deployment.yaml"
assert_contains 'name: "shared-encryption"' "$TMP_DIR/backend-deployment.yaml"
assert_not_contains 'AGENTAPI_SESSION_PERSISTENCE_' "$TMP_DIR/backend-deployment.yaml"
assert_not_contains 'AGENTAPI_K8S_SESSION_' "$TMP_DIR/backend-deployment.yaml"
assert_not_contains 'AGENTAPI_SCHEDULE_WORKER_' "$TMP_DIR/backend-deployment.yaml"
assert_not_contains 'AGENTAPI_SESSION_MANAGER_INTERNAL_API_TOKEN' "$TMP_DIR/backend-deployment.yaml"
assert_not_contains 'name: "provisioner"' "$TMP_DIR/backend-deployment.yaml"

assert_contains 'image: "example/worker:1.173.0"' "$TMP_DIR/backend-worker-deployment.yaml"
assert_contains 'serviceAccountName: backend-agentapi-proxy-worker' "$TMP_DIR/backend-worker-deployment.yaml"
assert_contains 'automountServiceAccountToken: false' "$TMP_DIR/backend-worker-deployment.yaml"
assert_contains 'args: \["worker"\]' "$TMP_DIR/backend-worker-deployment.yaml"
assert_contains 'name: AGENTAPI_WORKER_CONTROL_API_URL' "$TMP_DIR/backend-worker-deployment.yaml"
assert_contains 'name: AGENTAPI_WORKER_CONTROL_TOKEN' "$TMP_DIR/backend-worker-deployment.yaml"
assert_contains 'name: "worker-control"' "$TMP_DIR/backend-worker-deployment.yaml"
assert_contains 'name: AGENTAPI_KV_STORE_DATABASE_URL' "$TMP_DIR/backend-worker-deployment.yaml"
assert_contains 'name: AGENTAPI_REDIS_DB, value: "5"' "$TMP_DIR/backend-worker-deployment.yaml"
assert_contains 'name: AGENTAPI_REDIS_TLS_ENABLED, value: "true"' "$TMP_DIR/backend-worker-deployment.yaml"
assert_contains 'name: AGENTAPI_REDIS_DIAL_TIMEOUT, value: "9s"' "$TMP_DIR/backend-worker-deployment.yaml"
assert_contains 'name: AGENTAPI_SLACKBOT_CLEANUP_WORKER_LEASE_DURATION' "$TMP_DIR/backend-worker-deployment.yaml"
assert_contains 'name: AGENTAPI_STOCK_INVENTORY_WORKER_LEASE_DURATION' "$TMP_DIR/backend-worker-deployment.yaml"
assert_contains 'name: AGENTAPI_STOCK_INVENTORY_WORKER_POOLS' "$TMP_DIR/backend-worker-deployment.yaml"
assert_contains '\\"targetCount\\":3' "$TMP_DIR/backend-worker-deployment.yaml"
assert_not_contains 'AGENTAPI_K8S_SESSION_PROVISIONER_TOKEN' "$TMP_DIR/backend-worker-deployment.yaml"
assert_not_contains 'AGENTAPI_SESSION_MANAGER_' "$TMP_DIR/backend-worker-deployment.yaml"
assert_not_contains 'name: "manager-internal"' "$TMP_DIR/backend-worker-deployment.yaml"
assert_not_contains 'name: "provisioner"' "$TMP_DIR/backend-worker-deployment.yaml"

assert_contains 'image: "example/session-manager:1.173.0"' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'name: AGENTAPI_K8S_SESSION_IMAGE' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'value: "example/session-manager:1.173.0"' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'serviceAccountName: backend-agentapi-proxy-session-manager' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'automountServiceAccountToken: true' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'args: \["session-manager", "--port", "8080"\]' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'name: AGENTAPI_SESSION_MANAGER_INTERNAL_API_TOKEN' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'name: AGENTAPI_SESSION_MANAGER_ALLOCATION_LEASE_DURATION' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'name: AGENTAPI_ENCRYPTION_KEY' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'name: "shared-encryption"' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'name: AGENTAPI_SESSION_PERSISTENCE_S3_BUCKET, value: "manager-sessions"' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'name: SESSION_CONTROL_LONG_POLL_ENABLED, value: "true"' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'name: AGENTAPI_DIRECT_SESSION_RUNTIME_ENABLED, value: "true"' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'name: AGENTAPI_SCIA_ENABLED, value: "true"' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'name: AGENTAPI_K8S_SESSION_GITHUB_SECRET_NAME' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'name: AGENTAPI_K8S_SESSION_PROVISIONER_TOKEN' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'livenessProbe:' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'readinessProbe:' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'path: /livez' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_contains 'path: /readyz' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_not_contains 'AGENTAPI_SESSION_MANAGER_UPSTREAM_URL' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_not_contains 'AGENTAPI_WORKER_CONTROL_' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_not_contains 'AGENTAPI_SCHEDULE_WORKER_' "$TMP_DIR/backend-session-manager-deployment.yaml"
assert_not_contains 'name: "worker-control"' "$TMP_DIR/backend-session-manager-deployment.yaml"

# Kubernetes-only application persistence is a supported customer mode. API
# and worker receive only the shared KV Role; workload mutation remains bound
# exclusively to the session-manager Role.
"$HELM_BIN" template backend-kubernetes-kv "$REPO_ROOT/backend/helm/agentapi-proxy" \
  "${all_role_args[@]}" \
  --set api.kvStore.backend=kubernetes \
  --set api.kvStore.databaseUrlSecretRef.name= \
  --set worker.kvStore.backend=kubernetes \
  --set worker.kvStore.databaseUrlSecretRef.name= \
  --set sessionManager.kvStore.backend=kubernetes \
  --set sessionManager.kvStore.databaseUrlSecretRef.name= \
  >"$TMP_DIR/backend-kubernetes-kv.yaml"
assert_contains 'name: AGENTAPI_KV_STORE_BACKEND' "$TMP_DIR/backend-kubernetes-kv.yaml"
assert_contains 'value: "kubernetes"' "$TMP_DIR/backend-kubernetes-kv.yaml"
assert_contains 'name: backend-kubernetes-kv-agentapi-proxy-kvstore' "$TMP_DIR/backend-kubernetes-kv.yaml"
assert_contains 'name: backend-kubernetes-kv-agentapi-proxy-worker' "$TMP_DIR/backend-kubernetes-kv.yaml"
assert_contains 'automountServiceAccountToken: true' "$TMP_DIR/backend-kubernetes-kv.yaml"
assert_not_contains 'name: AGENTAPI_KV_STORE_DATABASE_URL' "$TMP_DIR/backend-kubernetes-kv.yaml"
assert_not_contains 'name: AGENTAPI_KV_STORE_AUTH_TOKEN' "$TMP_DIR/backend-kubernetes-kv.yaml"

assert_contains '^  name: backend-agentapi-proxy-session-manager$' "$TMP_DIR/backend-manager-rolebinding.yaml"
assert_contains '^    name: backend-agentapi-proxy-session-manager$' "$TMP_DIR/backend-manager-rolebinding.yaml"
assert_not_contains '^    name: backend-agentapi-proxy$' "$TMP_DIR/backend-manager-rolebinding.yaml"
assert_not_contains '^    name: backend-agentapi-proxy-worker$' "$TMP_DIR/backend-manager-rolebinding.yaml"

assert_contains 'name: backend-agentapi-proxy-worker' "$TMP_DIR/backend-all-roles.yaml"
assert_contains 'name: backend-agentapi-proxy-session-manager' "$TMP_DIR/backend-all-roles.yaml"
assert_contains 'app.kubernetes.io/component: session-manager' "$TMP_DIR/backend-all-roles.yaml"
assert_contains 'name: backend-agentapi-proxy-session-manager' "$TMP_DIR/backend-all-roles.yaml"
assert_contains 'verbs: \["get", "list", "create", "update", "patch", "delete"\]' "$TMP_DIR/backend-all-roles.yaml"

# A shadow API release can reuse the stable control-plane Service without
# creating it. Role-local values remain nil-safe for legacy --reuse-values.
"$HELM_BIN" template ccplant-shadow "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --namespace default \
  --set controlPlaneService.create=false >"$TMP_DIR/backend-shadow.yaml"
assert_not_contains '^  name: "?control"?$' "$TMP_DIR/backend-shadow.yaml"
assert_not_contains '^  name: agentapi-proxy-session$' "$TMP_DIR/backend-shadow.yaml"

"$HELM_BIN" template backend-legacy "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --skip-schema-validation \
  --set-json 'api=null' \
  --set-json 'worker=null' \
  --set-json 'sessionManager=null' >"$TMP_DIR/backend-legacy-roles.yaml"
assert_contains 'args: \["server"\]' "$TMP_DIR/backend-legacy-roles.yaml"
assert_not_contains 'app.kubernetes.io/component: worker' "$TMP_DIR/backend-legacy-roles.yaml"
assert_not_contains 'app.kubernetes.io/component: session-manager' "$TMP_DIR/backend-legacy-roles.yaml"

# Enabling a role without its independent credentials/storage is rejected at
# schema validation rather than failing later in a Pod.
if "$HELM_BIN" template invalid-worker "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --set worker.enabled=true >"$TMP_DIR/backend-invalid-worker.yaml" 2>/dev/null; then
  echo "worker.enabled=true without role credentials unexpectedly passed" >&2
  exit 1
fi
if "$HELM_BIN" template invalid-manager "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --set sessionManager.enabled=true >"$TMP_DIR/backend-invalid-manager.yaml" 2>/dev/null; then
  echo "sessionManager.enabled=true without role credentials unexpectedly passed" >&2
  exit 1
fi

# External registration is an optional manager mode. Local managers do not
# need parent credentials, while enabling registration requires its three
# independent inputs.
if "$HELM_BIN" template invalid-manager-registration "$REPO_ROOT/backend/helm/agentapi-proxy" \
  "${all_role_args[@]}" \
  --set sessionManager.externalRegistration.enabled=true \
  >"$TMP_DIR/backend-invalid-manager-registration.yaml" 2>/dev/null; then
  echo "external manager registration without credentials unexpectedly passed" >&2
  exit 1
fi
"$HELM_BIN" template manager-registration "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --show-only templates/session-manager-deployment.yaml \
  "${all_role_args[@]}" \
  --set sessionManager.externalRegistration.enabled=true \
  --set sessionManager.externalRegistration.upstreamUrl=https://upstream.example \
  --set sessionManager.externalRegistration.connectionTokenSecretRef.name=manager-connection \
  --set sessionManager.externalRegistration.hmacSecretRef.name=manager-hmac \
  >"$TMP_DIR/backend-manager-registration.yaml"
assert_contains 'name: AGENTAPI_SESSION_MANAGER_UPSTREAM_URL' "$TMP_DIR/backend-manager-registration.yaml"
assert_contains 'name: AGENTAPI_SESSION_MANAGER_CONNECTION_TOKEN' "$TMP_DIR/backend-manager-registration.yaml"
assert_contains 'name: AGENTAPI_SESSION_MANAGER_HMAC_SECRET' "$TMP_DIR/backend-manager-registration.yaml"

# TLS-enabled frontend URLs must use https.
"$HELM_BIN" template frontend "$REPO_ROOT/frontend/helm/agentapi-ui" \
  --set ingress.enabled=true \
  --set ingress.tls[0].secretName=frontend-tls \
  --set ingress.tls[0].hosts[0]=agentapi.example.com \
  --set hostname=agentapi.example.com >"$TMP_DIR/frontend-tls.yaml"
assert_contains 'value: "https://agentapi.example.com"' "$TMP_DIR/frontend-tls.yaml"

# Multiple proxy replicas require shared Redis state.
if "$HELM_BIN" template backend "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --set api.replicaCount=2 >"$TMP_DIR/backend-invalid-replicas.yaml" 2>/dev/null; then
  echo "api.replicaCount=2 without Redis unexpectedly passed schema validation" >&2
  exit 1
fi
"$HELM_BIN" template backend "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --set api.replicaCount=2 \
  --set redis.enabled=true >"$TMP_DIR/backend-replicas.yaml"

echo "Helm render assertions passed"
