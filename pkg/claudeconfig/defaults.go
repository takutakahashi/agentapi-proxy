package claudeconfig

// TrustedProjectDirs are the directories Claude Code can use as working
// directories in agentapi session containers.
var TrustedProjectDirs = []string{
	"/home/agentapi/workdir",
	"/home/agentapi/workdir/repo",
}

// EnsureClaudeJSONDefaults mutates config so Claude Code first-run, bypass
// permission, and workspace trust prompts are pre-accepted for session pods.
func EnsureClaudeJSONDefaults(config map[string]interface{}) map[string]interface{} {
	if config == nil {
		config = make(map[string]interface{})
	}

	config["hasCompletedOnboarding"] = true
	config["bypassPermissionsModeAccepted"] = true

	// Keep legacy/top-level keys for older Claude Code versions. Current
	// Claude Code stores trust state per project under ~/.claude.json.projects.
	config["hasTrustDialogAccepted"] = true
	config["hasCompletedProjectOnboarding"] = true
	config["dontCrawlDirectory"] = true

	projects, ok := config["projects"].(map[string]interface{})
	if !ok {
		projects = make(map[string]interface{})
		config["projects"] = projects
	}

	for _, dir := range TrustedProjectDirs {
		project, ok := projects[dir].(map[string]interface{})
		if !ok {
			project = make(map[string]interface{})
			projects[dir] = project
		}
		ensureProjectDefaults(project)
	}

	return config
}

func ensureProjectDefaults(project map[string]interface{}) {
	if _, ok := project["allowedTools"]; !ok {
		project["allowedTools"] = []interface{}{}
	}
	if _, ok := project["mcpContextUris"]; !ok {
		project["mcpContextUris"] = []interface{}{}
	}
	if _, ok := project["mcpServers"]; !ok {
		project["mcpServers"] = map[string]interface{}{}
	}
	if _, ok := project["enabledMcpjsonServers"]; !ok {
		project["enabledMcpjsonServers"] = []interface{}{}
	}
	if _, ok := project["disabledMcpjsonServers"]; !ok {
		project["disabledMcpjsonServers"] = []interface{}{}
	}

	project["hasTrustDialogAccepted"] = true
	project["hasCompletedProjectOnboarding"] = true
	project["projectOnboardingSeenCount"] = 4
	project["hasClaudeMdExternalIncludesApproved"] = true
	project["hasClaudeMdExternalIncludesWarningShown"] = true
}

// EnsureSettingsJSONDefaults mutates settings so Claude Code uses the official
// permissions settings for non-interactive agent containers.
func EnsureSettingsJSONDefaults(settings map[string]interface{}) map[string]interface{} {
	if settings == nil {
		settings = make(map[string]interface{})
	}

	permissions, ok := settings["permissions"].(map[string]interface{})
	if !ok {
		permissions = make(map[string]interface{})
		settings["permissions"] = permissions
	}

	permissions["defaultMode"] = "bypassPermissions"
	permissions["skipDangerousModePermissionPrompt"] = true
	settings["skipDangerousModePermissionPrompt"] = true

	return settings
}
