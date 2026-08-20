# Runtime system settings

System settings use a single runtime provider. The immutable startup
configuration loaded from Helm, files, and environment variables is the base
layer. The current versioned KV document is overlaid on that base.

Precedence is:

1. Versioned KV system settings
2. Helm / environment / configuration file
3. Application defaults

The provider loads the current KV version before subsystems are initialized,
applies successful Admin API writes immediately, and polls the KV head every
five seconds so all replicas converge on the same version.

## Apply boundaries

The following settings are read from the provider at operation time:

- authentication headers and GitHub authorization rules;
- default AI, Bedrock, MCP, marketplace, plugin, and environment settings for
  newly created sessions;
- Kubernetes session image, resources, PVC, timeout, OpenTelemetry, and SCIA
  settings for newly created sessions;
- notification link base URL;
- schedule, Slack cleanup, and stock inventory worker configuration. Workers
  are stopped and recreated when the runtime version changes.

Infrastructure-owning settings are resolved through the provider during
process startup, but are not hot-swapped in a running process:

- KV backend and replication topology (the KV backend is necessarily a
  bootstrap setting because it is required to read the runtime document);
- usage database;
- Redis connections;
- encryption backend/key;
- session persistence and asset backend;
- OAuth provider enable/disable transitions and credentials that require a
  provider to be created or removed;
- Slack Socket Mode and external session-manager connections.

Changing an infrastructure-owning setting therefore requires a proxy rollout.
Existing sessions are never mutated; session-related changes apply to sessions
created after the new settings version becomes active.
