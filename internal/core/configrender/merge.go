package configrender

import "github.com/takutakahashi/agentapi-proxy/internal/domain/entities"

// MergeSessionConfigs merges two webhook session configs, with override taking precedence over base.
func MergeSessionConfigs(base, override *entities.WebhookSessionConfig) *entities.WebhookSessionConfig {
	if base == nil && override == nil {
		return nil
	}
	if base == nil {
		return override
	}
	if override == nil {
		return base
	}

	result := entities.NewWebhookSessionConfig()

	result.SetEnvironment(mergeMaps(base.Environment(), override.Environment()))
	result.SetTags(mergeMaps(base.Tags(), override.Tags()))

	result.SetInitialMessageTemplate(firstNonEmpty(override.InitialMessageTemplate(), base.InitialMessageTemplate()))
	result.SetReuseMessageTemplate(firstNonEmpty(override.ReuseMessageTemplate(), base.ReuseMessageTemplate()))
	result.SetSessionProfileID(firstNonEmpty(override.SessionProfileID(), base.SessionProfileID()))

	if override.Params() != nil {
		result.SetParams(override.Params())
	} else {
		result.SetParams(base.Params())
	}

	result.SetReuseSession(base.ReuseSession() || override.ReuseSession())
	result.SetMountPayload(base.MountPayload() || override.MountPayload())

	return result
}

func mergeMaps(base, override map[string]string) map[string]string {
	result := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		result[k] = v
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
