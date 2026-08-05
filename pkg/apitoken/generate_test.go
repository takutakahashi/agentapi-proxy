package apitoken

import (
	"strings"
	"testing"
)

func TestGenerateTokenID_UniqueAndPrefixed(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id, err := GenerateTokenID()
		if err != nil {
			t.Fatalf("GenerateTokenID: %v", err)
		}
		if !strings.HasPrefix(id, TokenIDPrefix) {
			t.Errorf("id %q missing prefix", id)
		}
		if len(id) <= len(TokenIDPrefix) {
			t.Errorf("id %q too short", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestGenerateSecret_UniqueAndPrefixed(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		s, err := GenerateSecret()
		if err != nil {
			t.Fatalf("GenerateSecret: %v", err)
		}
		if !strings.HasPrefix(s, SecretPrefix) {
			t.Errorf("secret %q missing prefix", s)
		}
		if seen[s] {
			t.Fatalf("duplicate secret %q", s)
		}
		seen[s] = true
	}
}

func TestDisplayPrefix(t *testing.T) {
	s := "apt_abcdefghijklmnop"
	p := DisplayPrefix(s)
	if len(p) > displayPrefixLen && p == s {
		t.Errorf("display prefix returned full secret")
	}
	if !strings.HasPrefix(s, p) {
		t.Errorf("display prefix %q not a prefix of %q", p, s)
	}
	// short secret returned as-is
	short := "ab"
	if DisplayPrefix(short) != "ab" {
		t.Errorf("short secret not returned as-is: %q", DisplayPrefix(short))
	}
}

func TestMigrationTokenID_DeterministicAndSafe(t *testing.T) {
	id1 := MigrationTokenID("personal-user@example.com")
	id2 := MigrationTokenID("personal-user@example.com")
	if id1 != id2 {
		t.Errorf("migration id not deterministic: %q vs %q", id1, id2)
	}
	if !strings.HasPrefix(id1, TokenIDPrefix) {
		t.Errorf("missing prefix: %q", id1)
	}
	// distinct sources produce distinct ids
	if MigrationTokenID("personal-u1") == MigrationTokenID("team-u1") {
		t.Error("distinct sources produced identical id")
	}
	// the id suffix must only contain safe name characters
	suffix := strings.TrimPrefix(id1, TokenIDPrefix)
	for _, r := range suffix {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			t.Errorf("unsafe char %q in migration id suffix %q", r, suffix)
		}
	}
}

func TestMigrationTokenID_NoLeadingTrailingDash(t *testing.T) {
	// a source beginning/ending with special chars should not yield leading
	// or trailing dashes in the suffix.
	id := MigrationTokenID("---weird---")
	suffix := strings.TrimPrefix(id, TokenIDPrefix)
	if strings.HasPrefix(suffix, "-") || strings.HasSuffix(suffix, "-") {
		t.Errorf("suffix has leading/trailing dash: %q", suffix)
	}
}
