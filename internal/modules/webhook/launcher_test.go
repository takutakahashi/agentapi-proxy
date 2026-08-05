package webhook

import "testing"

func TestResolveTriggeredUsername(t *testing.T) {
	tests := []struct {
		name             string
		tags             map[string]string
		payload          map[string]interface{}
		credentialSource string
		want             string
	}{
		{
			name:             "uses explicit username tag",
			tags:             map[string]string{"username": " configured-user "},
			payload:          map[string]interface{}{"user": map[string]interface{}{"login": "payload-user"}},
			credentialSource: "triggered_user",
			want:             "configured-user",
		},
		{
			name: "uses github sender login as the triggering user",
			tags: map[string]string{},
			payload: map[string]interface{}{
				"sender": map[string]interface{}{"login": " github-sender "},
				"user":   map[string]interface{}{"login": "payload-user"},
			},
			credentialSource: "triggered_user",
			want:             "github-sender",
		},
		{
			name:             "defaults to payload user login for triggered user credentials",
			tags:             map[string]string{},
			payload:          map[string]interface{}{"user": map[string]interface{}{"login": " github-user "}},
			credentialSource: "triggered_user",
			want:             "github-user",
		},
		{
			name:             "does not infer user for other credential sources",
			tags:             map[string]string{},
			payload:          map[string]interface{}{"user": map[string]interface{}{"login": "github-user"}},
			credentialSource: "team",
			want:             "",
		},
		{
			name:             "missing user login remains unresolved",
			tags:             map[string]string{},
			payload:          map[string]interface{}{},
			credentialSource: "triggered_user",
			want:             "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveTriggeredUsername(tt.tags, tt.payload, tt.credentialSource); got != tt.want {
				t.Fatalf("resolveTriggeredUsername() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyTriggeredUsernameTags(t *testing.T) {
	tags := map[string]string{}

	got := applyTriggeredUsernameTags(tags, map[string]interface{}{
		"sender": map[string]interface{}{"login": "github-user"},
	}, "triggered_user")

	if got != "github-user" {
		t.Fatalf("applyTriggeredUsernameTags() = %q, want github-user", got)
	}
	if tags["username"] != "github-user" {
		t.Fatalf("username tag = %q, want github-user", tags["username"])
	}
	if tags["triggered_user_id"] != "github-user" {
		t.Fatalf("triggered_user_id tag = %q, want github-user", tags["triggered_user_id"])
	}
}
