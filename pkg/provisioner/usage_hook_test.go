package provisioner

import (
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestInjectUsageReportingHook(t *testing.T) {
	settings := &sessionsettings.SessionSettings{
		Session:               sessionsettings.SessionMeta{AgentType: "claude-acp"},
		UsageReportingEnabled: true,
	}
	injectUsageReportingHook(settings)
	for name, hooks := range map[string]map[string]interface{}{
		"claude": settings.Claude.SettingsJSON,
		"codex":  settings.Codex.HooksJSON,
	} {
		hookMap, ok := hooks["hooks"].(map[string]interface{})
		if !ok {
			t.Fatalf("%s hooks missing: %#v", name, hooks)
		}
		stops := asInterfaceSlice(hookMap["Stop"])
		if len(stops) != 1 || !strings.Contains(stops[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})["command"].(string), "client report-usage") {
			t.Fatalf("%s Stop hook = %#v", name, stops)
		}
	}
}

func TestInjectUsageReportingHookDisabled(t *testing.T) {
	settings := &sessionsettings.SessionSettings{}
	injectUsageReportingHook(settings)
	if settings.Claude.SettingsJSON != nil || settings.Codex.HooksJSON != nil {
		t.Fatal("disabled usage reporting must not inject hooks")
	}
}
