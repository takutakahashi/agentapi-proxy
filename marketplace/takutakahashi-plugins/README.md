# takutakahashi's Claude Code Plugin Marketplace

Custom Claude Code plugins and skills for agentapi and related development tools.

## Installation

Add this marketplace to Claude Code:

```bash
/plugin marketplace add takutakahashi/marketplace
```

## Available Plugins

### ccplant

Comprehensive toolkit for agentapi-proxy with three main skills: API client for session management, webhook automation, and schedule management for delayed and recurring tasks.

**Install:**
```bash
/plugin install ccplant@takutakahashi-plugins
```

**Included Skills:**

#### 1. API Client (`api-client`)
- Complete API endpoint reference with curl examples
- Multiple authentication methods (X-API-Key, Bearer token, GitHub OAuth, AWS IAM)
- Session lifecycle management (create, search, delete, route)
- Detailed permission and RBAC documentation
- Helper script for making API requests

#### 2. Webhook Management (`webhook-management`)
- Create and manage GitHub webhooks for PR automation
- Configure custom webhooks for Slack, Datadog, PagerDuty
- Advanced trigger conditions (GitHub events, JSONPath, Go templates)
- Webhook lifecycle management (create, update, delete, regenerate secrets)
- Integration examples for popular services

#### 3. Schedule Management (`schedule-management`)
- Create one-time and recurring schedules for automated session execution
- Support for cron expressions and ISO 8601 timestamps
- Schedule lifecycle management (create, list, update, delete, trigger)
- Team and user-scoped access control
- Use cases: daily reports, code reviews, incident drills, delayed deployments

**Included Resources:**
- **skills/api-client/SKILL.md**: Session management workflows
- **skills/webhook-management/SKILL.md**: Webhook automation guide
- **skills/schedule-management/SKILL.md**: Schedule management guide
- **references/API_REFERENCE.md**: Complete API endpoint documentation (includes schedule endpoints)
- **references/AUTHENTICATION.md**: Authentication methods and setup
- **references/PERMISSIONS.md**: Role-based access control
- **references/WEBHOOK_REFERENCE.md**: Webhook API and configuration
- **references/WEBHOOK_TRIGGERS.md**: Trigger conditions and filtering
- **references/WEBHOOK_EXAMPLES.md**: Service integration examples
- **scripts/agentapi_request.sh**: Helper script for API requests
- **assets/**: Configuration examples

## Usage

After installation, Claude Code will have knowledge of:

**API Client:**
- agentapi-proxy API endpoints and usage
- API Key authentication methods
- Session management workflows
- Permission and role configurations

**Webhook Automation:**
- Webhook creation and configuration
- GitHub webhook integration for PR reviews
- Custom webhook setup for Slack, Datadog, PagerDuty
- Trigger conditions (GitHub events, JSONPath, Go templates)
- Automated session creation from external events

**Schedule Management:**
- Schedule delayed or recurring session execution
- Support for one-time and cron-based schedules
- Team and user-scoped schedules
- Manual schedule triggering
- Use cases: daily reports, code reviews, incident drills

Example interactions:
- "How do I create a new agentapi session with an API key?"
- "Set up a webhook to review PRs automatically"
- "Create a Slack webhook for critical incidents"
- "Show me how to configure webhook triggers with Go templates"
- "Create a daily schedule to run standup bot every weekday at 9am"
- "What permissions does the user role have?"

## Contributing

To add new plugins to this marketplace:

1. Create a plugin directory under `plugins/`
2. Add `.claude-plugin/plugin.json` with plugin metadata
3. Add your skill(s) to `skills/` subdirectory
4. Update `.claude-plugin/marketplace.json` to include the new plugin
5. Submit a pull request

## License

MIT