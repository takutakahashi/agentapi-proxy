# Session profile MCP servers

## API design

`SessionProfileConfig` accepts an optional `mcp_servers` map. Its keys are stable server names and its values use the same shape as `Settings.mcp_servers` (`type`, `url`, `command`, `args`, `env`, and `headers`).

```json
{
  "name": "repository tools",
  "config": {
    "mcp_servers": {
      "github": {
        "type": "http",
        "url": "https://mcp.example.com/github",
        "headers": { "Authorization": "Bearer ${GITHUB_MCP_TOKEN}" }
      }
    }
  }
}
```

The settings layers are resolved from lowest to highest priority:

`base → team → user → session profile → oneshot`

MCP maps merge by server name. A profile replaces a same-named tenant server as one atomic configuration and inherits servers with other names. The selected profile is propagated to direct, scheduled, webhook, SlackBot, and External Session Manager launches.

Profiles are stored in Kubernetes Secrets, like the rest of the profile configuration. Environment variables and headers may contain credentials and must not be logged.

## agentapi-ui design

Reuse `src/components/settings/MCPServerSettings.tsx` in `SessionProfileFormModal` instead of creating a second MCP editor. Move the reusable editor's value type to a shared module, then:

1. Add `mcp_servers?: Record<string, APIMCPServerConfig>` to `SessionProfileConfig`.
2. Keep an `mcpServers` form state initialized from `editingProfile.config?.mcp_servers ?? {}`.
3. Add an “MCP servers” section under advanced settings using the shared editor.
4. Include `mcp_servers` in the submitted profile config when non-empty.
5. Explain in the form that tenant servers are inherited and same-named entries are overridden.

The list card should show only the number and names of configured profile overrides; details remain in the edit modal. The existing session-profile selection UI requires no changes because resolution happens server-side.
