package settingspatch

import "encoding/json"

// FromJSON parses raw JSON (settings.json Secret data) into a SettingsPatch.
//
// The SettingsPatch JSON format is identical to the format stored in Kubernetes Secrets,
// so no field-by-field conversion is needed. Unknown fields (e.g. "name", "created_at",
// "updated_at", "bedrock.enabled") are silently ignored.
func FromJSON(data []byte) (SettingsPatch, error) {
	var patch SettingsPatch
	if err := json.Unmarshal(data, &patch); err != nil {
		return SettingsPatch{}, err
	}
	return patch, nil
}
