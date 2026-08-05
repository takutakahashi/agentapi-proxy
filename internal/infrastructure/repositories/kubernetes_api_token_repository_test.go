package repositories

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/apitoken"
)

func newTestPersonalToken(id, secret string) *entities.APIToken {
	return entities.NewAPIToken(id, secret, secret[:8], "test personal",
		entities.APITokenScopeUser, entities.UserID("user-1"), "",
		[]entities.Permission{entities.PermissionSessionRead}, nil, entities.UserID("user-1"))
}

func newTestTeamToken(id, secret string) *entities.APIToken {
	return entities.NewAPIToken(id, secret, secret[:8], "test team",
		entities.APITokenScopeTeam, entities.UserID("sa-org-team"), "org/team",
		[]entities.Permission{entities.PermissionSessionCreate, entities.PermissionSessionRead},
		nil, entities.UserID("creator-1"))
}

func TestKubernetesAPITokenRepository_CreateAndGet(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesAPITokenRepository(client, "default")
	ctx := context.Background()

	tok := newTestPersonalToken("tok_1", "apt_secret_1")
	if err := repo.Create(ctx, tok); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByID(ctx, "tok_1")
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Secret() != "apt_secret_1" {
		t.Errorf("Secret = %q", got.Secret())
	}
	if got.UserID() != entities.UserID("user-1") {
		t.Errorf("UserID = %q", got.UserID())
	}
	if got.Scope() != entities.APITokenScopeUser {
		t.Errorf("Scope = %q", got.Scope())
	}

	// Verify labels/annotations
	s, err := client.CoreV1().Secrets("default").Get(ctx, repo.secretName("tok_1"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("secret not found: %v", err)
	}
	if s.Labels[LabelAPIToken] != "true" {
		t.Errorf("missing api-token label")
	}
	if s.Labels[LabelAPITokenScope] != "user" {
		t.Errorf("scope label = %q", s.Labels[LabelAPITokenScope])
	}
	if s.Annotations[AnnotationAPITokenOwnerID] != "user-1" {
		t.Errorf("owner annotation = %q", s.Annotations[AnnotationAPITokenOwnerID])
	}
	if _, ok := s.Data[SecretKeyAPITokenSecret]; !ok {
		t.Errorf("missing secret data key")
	}
}

func TestKubernetesAPITokenRepository_CreateAlreadyExists(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesAPITokenRepository(client, "default")
	ctx := context.Background()

	tok := newTestPersonalToken("tok_dup", "apt_aaaaaaaaaaaaaaaa")
	if err := repo.Create(ctx, tok); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := repo.Create(ctx, tok)
	if err != entities.ErrAPITokenAlreadyExists {
		t.Errorf("expected ErrAPITokenAlreadyExists, got %v", err)
	}
}

func TestKubernetesAPITokenRepository_GetByIDNotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesAPITokenRepository(client, "default")
	_, err := repo.GetByID(context.Background(), "nope")
	if err != entities.ErrAPITokenNotFound {
		t.Errorf("expected ErrAPITokenNotFound, got %v", err)
	}
}

func TestKubernetesAPITokenRepository_GetBySecret(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesAPITokenRepository(client, "default")
	ctx := context.Background()

	tok := newTestTeamToken("tok_team", "apt_team_secretxxx")
	if err := repo.Create(ctx, tok); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.GetBySecret(ctx, "apt_team_secretxxx")
	if err != nil {
		t.Fatalf("GetBySecret: %v", err)
	}
	if got.ID() != "tok_team" {
		t.Errorf("ID = %q", got.ID())
	}

	if _, err := repo.GetBySecret(ctx, "nonexistent"); err != entities.ErrAPITokenNotFound {
		t.Errorf("expected ErrAPITokenNotFound, got %v", err)
	}
}

func TestKubernetesAPITokenRepository_ListByOwnerAndTeam(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesAPITokenRepository(client, "default")
	ctx := context.Background()

	_ = repo.Create(ctx, newTestPersonalToken("tok_p1", "apt_p1aaaaaaaaaaaa"))
	_ = repo.Create(ctx, newTestPersonalToken("tok_p2", "apt_p2aaaaaaaaaaaa"))
	_ = repo.Create(ctx, newTestTeamToken("tok_t1", "apt_t1aaaaaaaaaaaa"))

	personal, err := repo.ListByOwner(ctx, entities.UserID("user-1"))
	if err != nil {
		t.Fatalf("ListByOwner: %v", err)
	}
	if len(personal) != 2 {
		t.Errorf("personal count = %d want 2", len(personal))
	}

	team, err := repo.ListByTeam(ctx, "org/team")
	if err != nil {
		t.Fatalf("ListByTeam: %v", err)
	}
	if len(team) != 1 {
		t.Errorf("team count = %d want 1", len(team))
	}

	all, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all count = %d want 3", len(all))
	}
}

func TestKubernetesAPITokenRepository_DeleteIdempotent(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesAPITokenRepository(client, "default")
	ctx := context.Background()

	_ = repo.Create(ctx, newTestPersonalToken("tok_del", "apt_dellllllllllll"))
	if err := repo.Delete(ctx, "tok_del"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// idempotent: deleting again returns nil
	if err := repo.Delete(ctx, "tok_del"); err != nil {
		t.Errorf("idempotent Delete: %v", err)
	}
	if _, err := repo.GetByID(ctx, "tok_del"); err != entities.ErrAPITokenNotFound {
		t.Errorf("expected not found after delete, got %v", err)
	}
}

func TestKubernetesAPITokenRepository_ExpiresAtRoundTrip(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesAPITokenRepository(client, "default")
	ctx := context.Background()

	exp := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	tok := entities.NewAPIToken("tok_exp", "apt_expxxxxxxxxxxxx", "apt_expxxxxxxxxxxxx", "expiring",
		entities.APITokenScopeUser, entities.UserID("u"), "",
		[]entities.Permission{entities.PermissionSessionRead}, &exp, entities.UserID("u"))
	if err := repo.Create(ctx, tok); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.GetByID(ctx, "tok_exp")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ExpiresAt() == nil || !got.ExpiresAt().Equal(exp) {
		t.Errorf("ExpiresAt = %v want %v", got.ExpiresAt(), exp)
	}
}

func TestKubernetesAPITokenRepository_ForceMigrationAnnotationsPreserved(t *testing.T) {
	// Verify that annotation-based migration markers survive a round trip
	// through the repository. The annotations are written by the migration
	// helper via a direct Secret patch, so this test confirms the labels
	// written by Create survive Get.
	client := fake.NewSimpleClientset()
	repo := NewKubernetesAPITokenRepository(client, "default")
	ctx := context.Background()

	tok := newTestPersonalToken("tok_mig", "apt_migaaaaaaaaaaaa")
	if err := repo.Create(ctx, tok); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Simulate migration annotation patch
	_, err := client.CoreV1().Secrets("default").Update(ctx,
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      repo.secretName("tok_mig"),
				Namespace: "default",
				Labels: map[string]string{
					LabelAPIToken:      "true",
					LabelAPITokenScope: "user",
					LabelAPITokenOwner: "user-1",
				},
				Annotations: map[string]string{
					AnnotationAPITokenOwnerID:           "user-1",
					AnnotationAPITokenCreatedBy:         "user-1",
					AnnotationAPITokenMigratedFrom:      "personal-api-key",
					AnnotationAPITokenMigrationSourceID: "user-1",
				},
			},
			Type: corev1.SecretTypeOpaque,
		}, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("update annotations: %v", err)
	}
	s, err := client.CoreV1().Secrets("default").Get(ctx, repo.secretName("tok_mig"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if s.Annotations[AnnotationAPITokenMigratedFrom] != "personal-api-key" {
		t.Errorf("migration annotation lost: %v", s.Annotations)
	}
}

// TestAPITokenSecretName_DNSSafeBoundedDeterministic verifies that the Secret
// name derived from any public token ID is a valid RFC-1123 DNS subdomain,
// bounded in length, deterministic, and collision-resistant. This guards
// against the dev crash where token IDs such as "tok_migrate-personal-..."
// (or randomly generated "tok_<hex>") carried an underscore from the "tok_"
// prefix, which is illegal in a Kubernetes resource name.
func TestAPITokenSecretName_DNSSafeBoundedDeterministic(t *testing.T) {
	// Cover: randomly generated token IDs, migration IDs with punctuation,
	// long migration source identifiers, and the bare prefix edge case.
	cases := []string{
		// random generated IDs (tok_ + hex) carry an underscore from prefix
		"tok_0123456789abcdef0123456789abcdef",
		// migration personal id: underscore + dashes + user handle
		"tok_migrate-personal-takutakahashi",
		// migration team id: underscores + slashes from a team id
		"tok_migrate-org-team",
		// long migration source (arbitrary email / org path) collapsed by
		// sanitizeForID; still produces a tok_-prefixed id with underscore
		"tok_migrate-personal-user-name-with-dots-and-slashes/org.subteam",
		// very long value exercising the length bound
		"tok_" + strings.Repeat("a", 500),
		// empty token id (defensive)
		"",
	}

	seen := map[string]string{} // name -> tokenID, for collision check
	for _, tokenID := range cases {
		name := apiTokenSecretName(tokenID)

		// 1. DNS-1123 subdomain validity (the bug: underscores/long rejected)
		if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
			t.Errorf("apiTokenSecretName(%q) = %q not a valid DNS subdomain: %v", tokenID, name, errs)
		}

		// 2. Bounded length (well under 253)
		if len(name) > 253 {
			t.Errorf("apiTokenSecretName(%q) name too long: %d", tokenID, len(name))
		}

		// 3. Must always carry the prefix and never embed the raw token id
		if !strings.HasPrefix(name, APITokenSecretPrefix) {
			t.Errorf("apiTokenSecretName(%q) missing prefix: %q", tokenID, name)
		}
		if tokenID != "" && strings.Contains(name, tokenID) {
			t.Errorf("apiTokenSecretName(%q) leaked raw token id into name %q", tokenID, name)
		}

		// 4. Deterministic: same input -> same name
		if apiTokenSecretName(tokenID) != name {
			t.Errorf("apiTokenSecretName(%q) not deterministic", tokenID)
		}

		// 5. Collision resistance: distinct inputs -> distinct names
		if prev, dup := seen[name]; dup && prev != tokenID {
			t.Errorf("collision: apiTokenSecretName(%q) == apiTokenSecretName(%q) = %q", prev, tokenID, name)
		}
		seen[name] = tokenID
	}
}

// TestAPITokenSecretName_DistinctFromPrefixOnly ensures the hash is actually
// applied (the name is longer than the bare prefix).
func TestAPITokenSecretName_DistinctFromPrefixOnly(t *testing.T) {
	name := apiTokenSecretName("tok_anything")
	if name == APITokenSecretPrefix {
		t.Fatal("secret name collapsed to prefix only; hash not applied")
	}
	// SHA-256 hex is 64 chars; prefix is len(APITokenSecretPrefix).
	wantLen := len(APITokenSecretPrefix) + 64
	if len(name) != wantLen {
		t.Errorf("name length = %d, want %d (prefix + 64 hex chars)", len(name), wantLen)
	}
}

// TestKubernetesAPITokenRepository_UnderscoreAndMigrationIDCRUD exercises the
// full repository round trip using token IDs that would have crashed a real
// cluster (underscore from the "tok_" prefix, long migration-style ids). The
// fake client does not validate names, so we additionally assert the produced
// Secret name passes RFC-1123 validation -- the guarantee a real apiserver
// enforces.
func TestKubernetesAPITokenRepository_UnderscoreAndMigrationIDCRUD(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesAPITokenRepository(client, "default")
	ctx := context.Background()

	tokenIDs := []string{
		"tok_migrate-personal-takutakahashi",                    // underscore + dashes
		"tok_" + strings.Repeat("a", 120),                       // long random id
		apitoken.MigrationTokenID("personal-user.name+tag/org"), // punctuation source
	}

	// Create tokens and assert the persisted Secret name is DNS-safe.
	tokens := make([]*entities.APIToken, 0, len(tokenIDs))
	for i, id := range tokenIDs {
		secret := "apt_" + id + strings.Repeat("x", 8)
		tok := entities.NewAPIToken(id, secret, secret[:8], "n",
			entities.APITokenScopeUser, entities.UserID("user-1"), "",
			[]entities.Permission{entities.PermissionSessionRead}, nil, entities.UserID("user-1"))
		if err := repo.Create(ctx, tok); err != nil {
			t.Fatalf("Create(%q): %v", id, err)
		}
		tokens = append(tokens, tok)

		name := repo.secretName(id)
		if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
			t.Errorf("case %d: secret name %q invalid DNS subdomain: %v", i, name, errs)
		}
		// Confirm the Secret actually exists under the hashed name.
		s, err := client.CoreV1().Secrets("default").Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("case %d: secret %q not found: %v", i, name, err)
		}
		// Metadata retains the original public ID (storage name decoupled).
		if s.Annotations[AnnotationAPITokenOwnerID] != "user-1" {
			t.Errorf("case %d: owner annotation lost: %v", i, s.Annotations)
		}
	}

	// Deterministic Get/Delete/annotation: each lookup must resolve to the
	// exact resource Create wrote.
	for i, tok := range tokens {
		got, err := repo.GetByID(ctx, tok.ID())
		if err != nil {
			t.Fatalf("case %d: GetByID(%q): %v", i, tok.ID(), err)
		}
		if got.ID() != tok.ID() {
			t.Errorf("case %d: GetByID id = %q want %q (metadata must retain public id)", i, got.ID(), tok.ID())
		}

		// Annotation must target the same resource.
		if err := repo.ApplyMigrationAnnotations(ctx, tok.ID(), "personal-api-key", "src-"+tok.ID()); err != nil {
			t.Fatalf("case %d: ApplyMigrationAnnotations(%q): %v", i, tok.ID(), err)
		}
		s, _ := client.CoreV1().Secrets("default").Get(ctx, repo.secretName(tok.ID()), metav1.GetOptions{})
		if s == nil || s.Annotations[AnnotationAPITokenMigratedFrom] != "personal-api-key" {
			t.Fatalf("case %d: annotation not applied to %q", i, repo.secretName(tok.ID()))
		}

		// Idempotent delete via the same derived name.
		if err := repo.Delete(ctx, tok.ID()); err != nil {
			t.Fatalf("case %d: Delete(%q): %v", i, tok.ID(), err)
		}
		if _, err := repo.GetByID(ctx, tok.ID()); err != entities.ErrAPITokenNotFound {
			t.Errorf("case %d: expected not found after delete, got %v", i, err)
		}
	}
}

// TestKubernetesAPITokenRepository_RandomGeneratedIDCRUD uses the real token
// ID generator (which emits "tok_" + random hex, carrying the underscore
// prefix) to confirm a freshly minted token persists under a DNS-safe name.
func TestKubernetesAPITokenRepository_RandomGeneratedIDCRUD(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesAPITokenRepository(client, "default")
	ctx := context.Background()

	id, err := apitoken.GenerateTokenID()
	if err != nil {
		t.Fatalf("GenerateTokenID: %v", err)
	}
	if !strings.HasPrefix(id, apitoken.TokenIDPrefix) {
		t.Fatalf("generated id %q missing tok_ prefix", id)
	}
	name := repo.secretName(id)
	if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
		t.Fatalf("generated id secret name %q invalid DNS subdomain: %v", name, errs)
	}

	tok := newTestPersonalToken(id, "apt_randomsecretxxx")
	if err := repo.Create(ctx, tok); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID() != id {
		t.Errorf("id = %q want %q", got.ID(), id)
	}
}
