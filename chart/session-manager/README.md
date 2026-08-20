# session-manager

Deploys only the Kubernetes External Session Manager execution plane. It does
not deploy the parent API, workers, or Redis. Multiple replicas coordinate the
single upstream runner/control loop with a Kubernetes Lease. Idle runners
atomically claim work from the parent; the manager is never pre-selected.

The parent API remains the source of truth for allocations, routes, quotas and
provision status. This chart keeps only Kubernetes workload resources and a
cached runtime profile in the remote cluster.

The `control` compatibility Service is enabled by default so session Pods
created by an earlier combined-chart installation keep their callback URL
during migration.

Required values are `parent.url`, `parent.publicUrl`, the parent connection/HMAC
Secret references, `runner.managerId`, `runner.pool`, `internalApi.tokenSecretRef.name`, and
`session.provisioner.tokenSecretRef.name`.
