package settingspatch

import "sort"

// Apply merges higher on top of base and returns the resulting SettingsPatch.
// higher has priority over base for all fields.
//
// This operation forms a monoid:
//   - Identity element: SettingsPatch{}
//   - Associativity:    Apply(Apply(a, b), c) == Apply(a, Apply(b, c))
func Apply(base, higher SettingsPatch) SettingsPatch {
	result := base

	// Scalar fields: higher wins if non-empty
	if higher.AuthMode != "" {
		result.AuthMode = higher.AuthMode
	}
	if higher.OAuthToken != "" {
		result.OAuthToken = higher.OAuthToken
	}
	if higher.Bedrock != nil {
		result.Bedrock = mergeBedrockPatch(base.Bedrock, higher.Bedrock)
	}

	// Map fields: per-key merge
	result.MCPServers = mergePatchMap(base.MCPServers, higher.MCPServers)
	result.EnvVars = mergeStringMap(base.EnvVars, higher.EnvVars)
	result.Marketplaces = mergePatchMap(base.Marketplaces, higher.Marketplaces)

	// Hooks: last-wins per event name
	result.Hooks = mergeHooks(base.Hooks, higher.Hooks)

	// Plugins: accumulated union
	result.EnabledPlugins = unionStrings(base.EnabledPlugins, higher.EnabledPlugins)

	// PreferredTeamID: higher wins if non-empty
	if higher.PreferredTeamID != "" {
		result.PreferredTeamID = higher.PreferredTeamID
	}

	// MemoryEnabled: higher wins if non-nil
	if higher.MemoryEnabled != nil {
		result.MemoryEnabled = higher.MemoryEnabled
	}

	return result
}

// Resolve folds a sequence of layers from lowest to highest priority and
// returns the effective SettingsPatch.
//
// Usage:
//
//	resolved := Resolve(basePatch, teamPatch, userPatch, oneshotPatch)
func Resolve(layers ...SettingsPatch) SettingsPatch {
	result := SettingsPatch{}
	for _, layer := range layers {
		result = Apply(result, layer)
	}
	return result
}

// mergeBedrockPatch merges Bedrock patches field-by-field.
// When both base and higher are non-nil, higher's non-empty fields override base.
func mergeBedrockPatch(base, higher *BedrockPatch) *BedrockPatch {
	if base == nil && higher == nil {
		return nil
	}
	if base == nil {
		return higher
	}
	if higher == nil {
		return base
	}
	result := *base
	if higher.Model != "" {
		result.Model = higher.Model
	}
	if higher.AccessKeyID != "" {
		result.AccessKeyID = higher.AccessKeyID
	}
	if higher.SecretAccessKey != "" {
		result.SecretAccessKey = higher.SecretAccessKey
	}
	if higher.RoleARN != "" {
		result.RoleARN = higher.RoleARN
	}
	if higher.Profile != "" {
		result.Profile = higher.Profile
	}
	return &result
}

// mergePatchMap merges two patch maps following JSON Merge Patch semantics:
//   - Key absent in higher: inherited from base
//   - Key present in higher with nil value: deleted from result
//   - Key present in higher with non-nil value: overrides base
func mergePatchMap[V any](base, higher map[string]*V) map[string]*V {
	if len(higher) == 0 {
		return base
	}
	result := make(map[string]*V, len(base)+len(higher))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range higher {
		if v == nil {
			delete(result, k) // explicit delete
		} else {
			result[k] = v
		}
	}
	return result
}

// mergeStringMap merges two string maps; higher's keys override base's.
func mergeStringMap(base, higher map[string]string) map[string]string {
	if len(base) == 0 && len(higher) == 0 {
		return nil
	}
	result := make(map[string]string, len(base)+len(higher))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range higher {
		result[k] = v
	}
	return result
}

// mergeHooks merges hook maps; higher takes precedence per event name.
func mergeHooks(base, higher map[string]interface{}) map[string]interface{} {
	if len(higher) == 0 {
		return base
	}
	result := make(map[string]interface{}, len(base)+len(higher))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range higher {
		result[k] = v
	}
	return result
}

// unionStrings returns a sorted, deduplicated union of two string slices.
func unionStrings(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	result := make([]string, 0, len(a)+len(b))
	for _, v := range a {
		if v != "" && !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	for _, v := range b {
		if v != "" && !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	sort.Strings(result)
	return result
}
