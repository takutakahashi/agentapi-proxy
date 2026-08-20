# KV Store

AgentAPI Proxy treats application-owned Kubernetes Secrets and ConfigMaps as KV
documents through one storage adapter. Operational resources such as Pod-mounted
Secrets, Deployments, Services, PVCs, and leader-election Leases remain
Kubernetes resources.

The boundary is dependency-based rather than name-based. The
`KubernetesSessionManager` receives only the raw Kubernetes client. Every
repository and handler outside it receives the persistence client, which is
backed exclusively by the selected KV backend.

```yaml
kv_store:
  backend: libsql
  database_url: http://libsql-server:8080
  auth_token: ""
```

Backend selection is exclusive: `kubernetes` stores application KV data only in
Kubernetes, while `libsql` stores it only in libSQL. Existing Kubernetes data is
not read, copied, changed, or deleted in libSQL mode. Pod-mounted Secrets and
ConfigMaps are operational resources outside this KV boundary.

The Helm values can contain the connection values directly:

```yaml
config:
  kvStore:
    backend: libsql
    databaseUrl: https://database.example.turso.io
    authToken: token
```

For production, the values can instead reference keys in a Kubernetes Secret.
Secret references take precedence over the direct values:

```yaml
config:
  kvStore:
    backend: libsql
    databaseUrlSecretRef:
      name: agentapi-libsql
      key: database-url
    authTokenSecretRef:
      name: agentapi-libsql
      key: auth-token
```

The server also accepts all three settings as environment variables:
`AGENTAPI_KV_STORE_BACKEND`, `AGENTAPI_KV_STORE_DATABASE_URL`, and
`AGENTAPI_KV_STORE_AUTH_TOKEN`. With Helm, leave `databaseUrl` and `authToken`
empty and provide the connection values from a Secret via `envFrom`:

```yaml
config:
  kvStore:
    backend: libsql

envFrom:
  - secretRef:
      name: agentapi-libsql
```

The Secret must contain keys named `AGENTAPI_KV_STORE_DATABASE_URL` and, when
authentication is enabled, `AGENTAPI_KV_STORE_AUTH_TOKEN`.

The `agentapi_kv` table stores a resource kind, namespace, key, complete JSON
document, and optimistic version. Both Secret and ConfigMap repositories retain
their existing labels and selectors. Migration is deliberately not performed by
server startup.

## Migrating from primary to secondary

With replicated storage configured, the command reads the same
`AGENTAPI_KV_STORE_PRIMARY_*` and `AGENTAPI_KV_STORE_SECONDARY_*` variables as
the server. Stop writes to the source deployment, then preview the application
records and destination conflicts:

```bash
agentapi-proxy kv-store migrate \
  --namespace agentapi-ui \
  --dry-run
```

Run the same command without `--dry-run` to copy the records. The command does
not modify or delete source objects. It copies only known application KV
resource families; when Kubernetes is the source, operational objects such as
Helm data, runtime configuration, Leases, and Pod-mounted notification
subscription Secrets remain in Kubernetes.

The migration is idempotent. An identical libSQL record is skipped. A different
record is reported as a conflict and is left unchanged; after reviewing the
conflict, `--overwrite` updates it from the Kubernetes source. `--output json`
provides machine-readable results. Explicit `--primary-backend`,
`--primary-database-url`, `--secondary-backend`, and
`--secondary-database-url` flags are also available. The legacy
`--database-url` and `--auth-token` form remains supported as a
Kubernetes-to-libSQL migration.

The Helm chart can run the idempotent migration in an init container before a
new proxy Pod starts accepting writes:

```yaml
config:
  kvStore:
    backend: ""
    primary:
      backend: kubernetes
    secondary:
      backend: libsql
      databaseUrlSecretRef:
        name: agentapi-libsql
        key: database-url
    replication:
      mode: rollback
    migration:
      enabled: true
      dryRun: false
      overwrite: false
```

The init container uses the same direct values or Secret references as the
proxy, and runs `kv-store verify` immediately after migration. A conflict or
verification mismatch prevents the new Pod from starting unless the migration
conflict is explicitly resolved with `overwrite`. Keep `overwrite: false` for
normal idempotent rollouts.

After migration, verify both directions with the configured store pair:

```bash
agentapi-proxy kv-store verify \
  --namespace agentapi-ui \
  --output text
```

The command compares application-owned record values while ignoring backend
versions. It reports `matched`, `missing-primary`, `missing-secondary`, and
`different` records, and exits non-zero when any mismatch exists. Use
`--output json` for automation.

For local development, a server is not required. A local SQLite-compatible
libSQL file can be used directly:

```bash
agentapi-proxy kv-store migrate \
  --namespace agentapi-ui-dev \
  --database-url "file:///tmp/agentapi-kv.db" \
  --dry-run
```

After a successful migration, configure `kv_store.backend: libsql` and the same
database connection values before restarting the deployment. Keep the Kubernetes
objects until the libSQL-backed deployment has been verified so rollback remains
possible.

For development-only validation, the Helm chart can start an ephemeral server
with `libsqlTrial.enabled=true`. Its `emptyDir` is discarded with the Pod and it
does not scan or import Kubernetes objects.
