# Session control over long polling

Session control replaces proxy-to-session runtime requests with outbound-only HTTPS requests from
the session pod. Redis Streams are an internal backend implementation detail: session pods never
connect to Redis and never receive Redis addresses or credentials.

Enable the feature with `sessionControl.enabled=true` in the Helm values. Redis must also be
configured. If Redis is unavailable at backend startup, or the feature is disabled, the existing
direct session HTTP/SSE path remains active.

Compatibility is selected per session, not per deployment. A session provisioner renews a
75-second capability lease whenever it opens a command poll or uploads events. The backend only
enqueues commands for sessions with a live lease. Sessions created with an older image never
publish the lease and continue to receive the existing direct HTTP/RPC calls. If Redis or the
control client becomes unavailable and the lease expires, the backend automatically returns that
session to direct transport.

## Transport

The session provisioner polls for commands:

```http
GET /internal/session-control/{sessionId}/commands?after={cursor}&wait=30s
Authorization: Bearer {session-control-token}
```

The response is `204 No Content` on timeout, or a command batch with a Redis Stream cursor. The
session executes `prompt` and `cancel` against its loopback agent runtime and uploads completion
events:

```http
POST /internal/session-control/{sessionId}/events
Authorization: Bearer {session-control-token}
Content-Type: application/json

{"events":[{"id":"...","type":"command_completed","command_id":"...","command_stream_id":"..."}]}
```

Runtime SSE data is relayed through the same event endpoint. Authorized clients can consume the
short-lived relay with:

```http
GET /sessions/{sessionId}/control/events/wait?after={cursor}&wait=30s
```

## Delivery and retention

Commands and events use per-session Redis Streams with hash tags so related keys share a Redis
Cluster slot. Streams have an approximate 10,000-entry limit and a 30-minute TTL. Command ACK
cursors are shared in Redis, preventing an acknowledged prompt from being replayed merely because
a session reconnects through another backend pod. The session also persists its local cursor in
the workspace as a secondary safeguard.

Redis is a short-lived delivery buffer, not conversation storage. Completed conversation history
remains owned by the session runtime. A Redis-wide data loss can therefore lose in-flight deltas;
clients must treat the session history/snapshot as the resynchronization source.

## Network boundary

- Backend pods connect to Redis.
- Session pods connect only to the backend control-plane HTTPS endpoint and their own loopback
  runtime.
- Redis configuration and credentials are not copied into session pod environment variables.

Each session pod receives a distinct `SESSION_CONTROL_TOKEN`. The backend derives it
deterministically with HMAC-SHA-256 from the backend-only provisioner master token and the session
ID, so no session-to-token lookup table is stored. Control requests are authorized against the
session ID in the URL; a token issued to one session cannot access another session's streams.
