# Direct Session Runtime Control

## Status

The direct request/frame data path, per-generation authentication, ESM allocation bootstrap, and
stock/non-stock Pod worker are implemented behind
`AGENTAPI_DIRECT_SESSION_RUNTIME_ENABLED=true`. The typed lifecycle operation queue and explicit
event/snapshot endpoints remain later migration phases; delete and resume therefore continue to
use the ESM control tunnel for now.

## Summary

External Session Manager (ESM) remains responsible for placement and workload lifecycle, but it
must not relay steady-state session traffic. After an ESM creates or adopts a Session Pod, the Pod
opens an outbound long-poll connection directly to the parent proxy. The parent uses that channel
as a per-session reverse RPC transport for normal HTTP and streaming traffic.

```text
                                     lifecycle
                              +-----------------------+
                              |                       v
client -> parent proxy -> allocation queue -> ESM -> Kubernetes
             ^                                  |        |
             |                                  | create |
             |      direct runtime channel      |        v
             +<============================== Session Pod
                    outbound HTTPS only
```

The ESM data-plane control tunnel is retained only during migration. In the target state:

- the parent proxy owns public session IDs, authorization, routing, runtime command/event queues,
  and user-visible session state;
- the ESM owns allocator selection, stock adoption, Kubernetes resources, and lifecycle
  reconciliation;
- the Session Pod owns execution against its loopback agentapi runtime and connects directly to
  the parent;
- the parent never needs inbound network access to the ESM or Session Pod.

## Goals

- Remove the ESM from prompt, cancel, status, message, HTTP, and SSE data paths.
- Keep all ESM and Session Pod connections outbound-only.
- Preserve agentapi-compatible `/:sessionId/*` behavior, including streaming responses.
- Allow existing sessions to continue when the ESM process is unavailable.
- Support stock and newly-created Session Pods with the same runtime protocol.
- Make reconnects, duplicate delivery, fencing, and migration behavior explicit.

## Non-goals

- Removing the ESM. It is still required to manage its execution environment.
- Sending Kubernetes credentials or parent Redis credentials to Session Pods.
- Making Redis the durable source of conversation history.
- Sharing one runtime connection across multiple Session Pods.
- Guaranteeing exactly-once execution across an unrecoverable Pod and local-runtime crash.

## Responsibility boundaries

| Responsibility | Parent proxy | ESM | Session Pod |
| --- | --- | --- | --- |
| Authenticate users and authorize public session access | Yes | No | No |
| Select an ESM | Yes | No | No |
| Select/adopt stock in an ESM cluster | No | Yes | No |
| Create/delete Kubernetes resources | No | Yes | No |
| Hold public session route and state | Yes | Local diagnostic copy only | No |
| Relay steady-state session HTTP/SSE | Yes, through direct runtime channel | No | Yes |
| Execute requests against loopback agentapi | No | No | Yes |
| Persist canonical conversation history | No | No | Local runtime |

The ESM is therefore a placement and lifecycle agent, not a session traffic proxy.

## IDs and ownership

The parent-generated ID is the canonical `session_id` everywhere. An ESM may retain a local
resource ID for migration compatibility, but a direct-runtime Pod identifies itself using the
parent ID. New direct-runtime allocations should create local resources using the parent ID when
possible.

Each allocation also receives an immutable `manager_id` and a monotonically increasing
`generation`. The parent accepts a runtime connection only when all three values match the active
route:

```text
(session_id, manager_id, generation)
```

`generation` fences a stale Pod after replacement or reallocation. A Pod from an older generation
receives `409 Conflict` and must stop executing new commands.

## Bootstrap and authentication

### Credential issuance

For every allocation generation, the parent creates:

- a 256-bit random runtime bearer token;
- a stored hash of that token, bound to `session_id`, `manager_id`, and `generation`;
- an expiry matching the session lifetime, with immediate revocation on termination or
  reallocation.

The token is not derived from the ESM connection token or a cluster-wide provisioner secret. This
limits compromise to one session generation and lets ESM token rotation remain independent.

The parent sends the plaintext token once inside the authenticated allocation response. The ESM
places it in a session-scoped Kubernetes Secret. It must not place the token in labels,
annotations, logs, allocation result messages, or environment variables rendered in diagnostics.
The active route stores only the token hash. While an allocation is pending, its existing
access-controlled allocation Secret may contain the plaintext bootstrap token; that field is
deleted as soon as the ESM claims the allocation. A deployment using an external durable queue
must envelope-encrypt the field for the selected ESM instead of storing plaintext.

The ESM is already trusted with resolved session settings and can observe this bootstrap token.
Proof-of-possession with a Pod-generated key can be added later, but is not required for the first
version.

### Stock Session Pods

A stock Pod exists before a session credential does, so it cannot start the direct runtime channel
while idle. Adoption uses the existing local provision request once:

1. The ESM claims stock by changing `agentapi.proxy/stock=true` to `claiming`.
2. The ESM creates the session provision request containing resolved settings plus a
   `parent_runtime` block.
3. The stock provisioner applies the session settings and stores the runtime credential in a file
   readable only by the session user.
4. The provisioner starts the direct runtime worker.
5. The Pod connects to the parent and the ESM removes the stock marker as today.

`parent_runtime` contains the parent URL, public session ID, manager ID, generation, runtime token,
and protocol version. The local provision request is bootstrap traffic, not a steady-state relay.
Newly-created Pods use the same provision payload so stock and non-stock behavior cannot drift.

## Direct runtime protocol

Version 1 moves the generic HTTP reverse tunnel currently implemented by ESM control into the
Session Pod provisioner. Semantic `prompt` and `cancel` commands may remain as an optimization,
but public routing must work through generic request/response frames.

### Endpoints

All endpoints are on the parent proxy and use the per-session runtime bearer token. The first two
are implemented in the initial data-path release; `events` and `snapshot` are planned for the
state-reconciliation phase.

```text
GET  /internal/session-runtime/{sessionId}/requests
POST /internal/session-runtime/{sessionId}/frames
POST /internal/session-runtime/{sessionId}/events
POST /internal/session-runtime/{sessionId}/snapshot
```

`requests` accepts:

- `after`: last consumed stream cursor;
- `wait`: blocking duration, default and maximum 30 seconds;
- `count`: maximum batch size, capped at 100;
- `manager_id`, `generation`, `instance_id`, and `protocol_version`.

It returns `204 No Content` on a normal timeout or:

```json
{
  "requests": [
    {
      "id": "request-uuid",
      "stream_id": "123-0",
      "method": "POST",
      "path": "/message",
      "raw_query": "",
      "headers": {"Content-Type": ["application/json"]},
      "body": "base64-encoded bytes",
      "deadline": "2026-08-07T12:00:00Z",
      "created_at": "2026-08-07T11:59:30Z"
    }
  ],
  "next_cursor": "123-0"
}
```

The worker executes each request against the Pod's loopback agentapi endpoint. It posts one or more
ordered frames:

```json
{
  "frames": [
    {
      "id": "frame-uuid",
      "request_id": "request-uuid",
      "request_stream_id": "123-0",
      "sequence": 0,
      "status": 200,
      "headers": {"Content-Type": ["text/event-stream"]},
      "body": "base64-encoded bytes",
      "done": false,
      "created_at": "2026-08-07T11:59:31Z"
    }
  ]
}
```

The first frame carries status and headers. Subsequent frames carry response bytes. Exactly one
terminal frame has `done=true` or an `error`. The parent acknowledges the request cursor only after
accepting a terminal frame. Closing a public streaming response enqueues a cancellation request for
the same request ID.

### Events and snapshots

The Pod posts typed events separately from request responses:

- `runtime_connected` and `runtime_disconnected`;
- `status_changed`;
- `message_updated`;
- `command_completed` and `command_failed`;
- `heartbeat` with runtime and protocol health.

Events have globally unique IDs and are deduplicated by the parent. They are hints, not canonical
conversation storage. `message_updated` causes the parent to notify subscribers; message content
is fetched through the direct runtime channel.

After first connect, cursor loss, Redis loss, or an explicit parent request, the Pod posts a
snapshot containing current status, last message timestamp, runtime identity, and supported
capabilities. The parent reconciles its cached state from this snapshot.

### Header and path policy

The parent applies normal user authorization before enqueueing a request. It strips credentials
before writing to Redis, including `Authorization`, cookies, API keys, ESM tokens, and runtime
tokens. The Pod accepts only relative paths from an allowlist rooted at the loopback agentapi
server. It rejects absolute URLs, authority changes, hop-by-hop headers, and paths for proxy
administration.

Request and frame bodies have configurable per-frame and total-request limits. Large uploads must
use an object-store or dedicated upload flow rather than unbounded Redis frames.

## Delivery semantics and recovery

The transport is at-least-once. Redis Streams are an ephemeral delivery buffer, not the source of
truth.

The Session Pod persists, under the workspace or session PVC:

- the last accepted request cursor;
- a bounded journal of request IDs and terminal response metadata;
- unsent event and terminal frames.

On reconnect it uses the persisted cursor. If a request ID is redelivered:

- if the journal has a completed response, it resends the terminal result without re-executing;
- if execution is still active, it attaches to that execution;
- otherwise it executes the request once and records the result before acknowledging it.

Streaming bytes already delivered before a crash cannot be made exactly-once without durable body
storage. A broken stream is terminated and the public client must retry or resynchronize from
session history. Mutating operations carry an `Idempotency-Key` derived from the parent request ID
when the loopback runtime supports it. The limitation must remain documented until all supported
runtimes honor that key.

### Retry policy

- Normal `204`: reconnect immediately with a small 0-250 ms jitter.
- Network error, `429`, or `5xx`: exponential backoff starting at 1 second, capped at 30 seconds,
  with full jitter.
- `401` or `403`: stop after one refresh attempt; never busy-loop.
- `409` generation conflict: permanently fence this worker.
- Successful request delivery or a connection held for at least 30 seconds resets backoff.

The HTTP client timeout must exceed the server wait time (35 seconds for a 30-second poll). Frame
uploads use a separate bounded timeout.

The parent's runtime connection lease is refreshed on every poll or frame upload and expires after
75 seconds. Expiry changes connectivity, not session ownership, and does not cause automatic
reallocation.

## Routing and lifecycle flows

### Create

1. Parent authorizes `POST /start`, selects an ESM, creates generation 1, and stores a pending
   route.
2. Parent queues an allocation containing resolved settings and the runtime bootstrap credential.
3. ESM creates or adopts a local Session Pod and reports its Kubernetes identity.
4. Pod completes local provisioning and opens the direct runtime poll.
5. Parent marks the route ready only after both allocation success and a valid runtime lease.
6. Requests arriving earlier return `503` with `Retry-After: 2`.

### Normal request

1. Parent resolves the public route and authorizes the user.
2. If `transport=direct_session_runtime`, it enqueues a per-session request.
3. Pod receives it from the parent, calls loopback agentapi, and posts frames.
4. Parent converts frames into the public HTTP or SSE response.

No step contacts the ESM.

### Delete

Deletion remains a lifecycle operation:

1. Parent marks the route `terminating` and stops accepting new runtime requests.
2. Parent sends a lifecycle delete operation to the owning ESM.
3. ESM deletes the Deployment/Pod, Service, PVC according to retention policy, and local
   provision data, then reports completion.
4. Parent revokes the runtime credential and removes route and stream state.

The target design uses a small manager operation queue rather than the generic ESM HTTP tunnel:

```text
GET  /internal/external-session-managers/{managerId}/operations
POST /internal/external-session-managers/{managerId}/operations/{operationId}/result
```

Operations are typed and limited to `delete`, `reconcile`, and future lifecycle actions. During
migration, delete may continue over the existing ESM control tunnel. If the ESM is offline, the
parent keeps a tombstone and retries deletion when it reconnects; existing direct-runtime sessions
are otherwise unaffected by ESM availability.

## Storage model

Redis keys should be partitioned by public session ID:

```text
agentapi:runtime:{sessionId}:requests
agentapi:runtime:{sessionId}:request-ack
agentapi:runtime:{sessionId}:frames:{requestId}
agentapi:runtime:{sessionId}:events
agentapi:runtime:{sessionId}:connection
```

Request/frame TTL defaults to five minutes. Event TTL defaults to 30 minutes. Route,
credential-hash, generation, and deletion tombstone data use the configured durable parent
repository rather than Redis alone.

Only the parent accesses Redis. Neither ESM nor Session Pod receives Redis credentials.

## ESM capability negotiation and transport selection

ESM registration and heartbeat advertise capabilities, including
`direct_session_runtime_v1`. A session route stores one explicit transport:

- `direct_session_runtime`: target state;
- `esm_control_tunnel`: migration fallback;
- `direct_http`: legacy public URL fallback.

Transport is selected once per generation and never changes silently while a request is active.
Suggested feature flags are:

```text
AGENTAPI_DIRECT_SESSION_RUNTIME_ENABLED=true
AGENTAPI_DIRECT_SESSION_RUNTIME_REQUIRED=false
```

When `REQUIRED=true`, allocation to an ESM without the capability fails before Pod creation. There
is no fallback to the ESM tunnel or local Session Manager. This flag is also the enforcement point
for a future external-only parent mode.

## Availability properties

| Failure | Expected behavior |
| --- | --- |
| ESM process unavailable | Existing sessions continue; create/delete/reconcile wait |
| Parent replica restarts | Another replica resumes from shared Redis and durable route state |
| Parent unavailable | Pod journals events/results and reconnects with backoff |
| Session Pod restarts | It reloads generation, token, cursor, and journal from its Secret/PVC |
| Redis loses ephemeral streams | Parent requests in flight fail; Pod posts a snapshot and clients resync |
| Stale Pod reconnects | Generation mismatch returns 409 and fences it |
| Runtime process restarts inside Pod | Worker reports disconnected, restarts/reattaches locally, then snapshots |

## Observability

Metrics must distinguish placement and runtime connectivity:

- `agentapi_esm_allocation_connected{manager_id}`;
- `agentapi_session_runtime_connected{manager_id,protocol_version}`;
- poll duration and reconnect count by result class;
- request queue age, execution duration, frames, and bytes;
- duplicate request/frame/event counts;
- generation conflicts and fenced workers;
- lifecycle operation queue age.

Logs include session ID, manager ID, generation, request ID, and cursor, but never runtime tokens or
request bodies. The session status API should expose transport, runtime connection state, last
heartbeat, and owning manager without exposing credentials.

## Migration plan

### Phase 1: Introduce the per-session protocol

- Generalize the current ESM command/frame store and tunnel into reusable reverse-RPC components.
- Add per-session runtime controllers, authentication, generation fencing, snapshots, and tests.
- Keep all existing routing on `esm_control_tunnel`.

### Phase 2: Add the Pod worker

- Move the generic HTTP execution worker into `pkg/provisioner`.
- Add bootstrap fields to allocation and provision settings.
- Support stock adoption and persist cursor/journal state.
- Advertise `direct_session_runtime_v1` from capable ESMs.

### Phase 3: Opt-in routing

- Create new capable sessions with `transport=direct_session_runtime` behind the enabled flag.
- Keep existing sessions on their original transport until deletion.
- Compare success rate, latency, reconnects, and stream completion with the ESM tunnel.

### Phase 4: Make direct runtime required

- Enable `REQUIRED=true` after all active ESM versions support the protocol.
- Add the typed manager lifecycle operation queue.
- Stop creating new `esm_control_tunnel` routes.

### Phase 5: Remove the ESM data plane

- Drain or delete all legacy routes.
- Remove generic request/frame forwarding from the ESM worker.
- Retain only allocation, lifecycle operations, heartbeat, and registration.
- Remove `SESSION_MANAGER_PUBLIC_URL` after the supported downgrade window.

Rollback before Phase 4 is generation-based: create a replacement generation using the previous
transport. Never switch transport in-place for a live generation.

## Validation and acceptance criteria

The implementation is complete when all of the following hold:

- A new and a stock-adopted Pod both establish a direct parent lease.
- Prompt, cancel, status, messages, JSON-RPC, ordinary HTTP, and SSE pass without ESM relay.
- Killing the ESM does not interrupt an established session.
- Killing a parent replica does not cause duplicate prompt execution.
- A stale generation cannot execute commands or post frames/events.
- Parent or Redis interruption produces a bounded failure and successful snapshot recovery.
- ESM-offline deletion remains pending and completes after reconnect.
- Mixed-version ESMs use the declared transport without silent fallback.
- No user credential, ESM token, or runtime token is written into Redis command headers or logs.

## Main implementation areas

- `internal/core/sessioncontrol`: extend or replace semantic commands with generic runtime
  requests and frames.
- `internal/infrastructure/sessioncontrol`: per-session Redis request/frame streams, deduplication,
  lease, and acknowledgements.
- `internal/interfaces/controllers`: runtime poll/frame/event/snapshot endpoints.
- `pkg/provisioner`: direct runtime worker, local execution, retry, cursor, and journal.
- `internal/app`: route transport selection, generation fencing, and direct tunnel dispatch.
- `internal/modules/sessionmanager`: remove runtime forwarding; add typed lifecycle operations.
- `pkg/sessionsettings`: parent runtime bootstrap block.
- `spec/openapi.json`: document the internal protocol when implementation begins.
