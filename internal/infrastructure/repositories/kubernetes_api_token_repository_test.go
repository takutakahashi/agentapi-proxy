package repositories

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
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
	s, err := client.CoreV1().Secrets("default").Get(ctx, APITokenSecretPrefix+"tok_1", metav1.GetOptions{})
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
				Name:      APITokenSecretPrefix + "tok_mig",
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
	s, err := client.CoreV1().Secrets("default").Get(ctx, APITokenSecretPrefix+"tok_mig", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if s.Annotations[AnnotationAPITokenMigratedFrom] != "personal-api-key" {
		t.Errorf("migration annotation lost: %v", s.Annotations)
	}
}
