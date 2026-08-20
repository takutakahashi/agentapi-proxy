package entities

import "testing"

func TestSessionProfileMatchesSelectorTags(t *testing.T) {
	profile := NewSessionProfile("profile-1", "selected", "user-1")

	if profile.MatchesSelectorTags(map[string]string{"env": "dev"}) {
		t.Fatal("empty selector should not match")
	}

	profile.SetSelectorTags(map[string]string{"env": "dev", "repo": "owner/repo"})
	if !profile.MatchesSelectorTags(map[string]string{
		"env":   "dev",
		"repo":  "owner/repo",
		"extra": "kept",
	}) {
		t.Fatal("expected all selector tags to match")
	}
	if profile.MatchesSelectorTags(map[string]string{"env": "dev"}) {
		t.Fatal("missing selector tag should not match")
	}
	if profile.MatchesSelectorTags(map[string]string{"env": "prod", "repo": "owner/repo"}) {
		t.Fatal("different selector value should not match")
	}
}

func TestSessionProfileSelectorTagsReturnsCopy(t *testing.T) {
	profile := NewSessionProfile("profile-1", "selected", "user-1")
	profile.SetSelectorTags(map[string]string{"env": "dev"})

	tags := profile.SelectorTags()
	tags["env"] = "prod"

	if profile.SelectorTags()["env"] != "dev" {
		t.Fatal("SelectorTags should return a copy")
	}
}
