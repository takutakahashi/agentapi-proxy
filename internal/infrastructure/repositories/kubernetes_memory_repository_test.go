package repositories

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func newTestMemory(id, title, content string, scope entities.ResourceScope, ownerID, teamID string) *entities.Memory {
	return entities.NewMemory(id, title, content, scope, ownerID, teamID)
}

func TestKubernetesMemoryRepository_Create(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesMemoryRepository(client, "default")
	ctx := context.Background()

	m := newTestMemory("id-1", "Test Memory", "Some content", entities.ScopeUser, "user-1", "")
	if err := repo.Create(ctx, m); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify ConfigMap was created with correct labels
	cm, err := client.CoreV1().ConfigMaps("default").Get(ctx, MemoryConfigMapPrefix+"id-1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("ConfigMap not found: %v", err)
	}
	if cm.Labels[LabelMemoryType] != LabelMemoryTypeValue {
		t.Errorf("expected type label %s, got %s", LabelMemoryTypeValue, cm.Labels[LabelMemoryType])
	}
	if cm.Labels[LabelMemoryScope] != "user" {
		t.Errorf("expected scope label 'user', got %s", cm.Labels[LabelMemoryScope])
	}
	if cm.Annotations[AnnotationMemoryOwnerID] != "user-1" {
		t.Errorf("expected owner annotation 'user-1', got %s", cm.Annotations[AnnotationMemoryOwnerID])
	}
	if _, ok := cm.Data[ConfigMapKeyMemory]; !ok {
		t.Error("expected memory.json key in ConfigMap data")
	}
}

func TestKubernetesMemoryRepository_Create_Duplicate(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesMemoryRepository(client, "default")
	ctx := context.Background()

	m := newTestMemory("dup-id", "Title", "Content", entities.ScopeUser, "user-1", "")
	if err := repo.Create(ctx, m); err != nil {
		t.Fatalf("first Create failed: %v", err)
	}

	err := repo.Create(ctx, m)
	if err == nil {
		t.Fatal("expected error for duplicate create, got nil")
	}
}

func TestKubernetesMemoryRepository_GetByID(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesMemoryRepository(client, "default")
	ctx := context.Background()

	original := entities.NewMemoryWithTags("get-id", "Title", "Content body", entities.ScopeUser, "user-1", "", map[string]string{"k": "v"})
	if err := repo.Create(ctx, original); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByID(ctx, "get-id")
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.ID() != "get-id" {
		t.Errorf("expected id 'get-id', got '%s'", got.ID())
	}
	if got.Title() != "Title" {
		t.Errorf("expected title 'Title', got '%s'", got.Title())
	}
	if got.Content() != "Content body" {
		t.Errorf("expected content 'Content body', got '%s'", got.Content())
	}
	if got.Tags()["k"] != "v" {
		t.Errorf("expected tag k=v, got %v", got.Tags())
	}
}

func TestKubernetesMemoryRepository_GetByID_NotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesMemoryRepository(client, "default")
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected ErrMemoryNotFound, got nil")
	}
	var notFound entities.ErrMemoryNotFound
	if !errors.As(err, &notFound) {
		t.Errorf("expected ErrMemoryNotFound, got %T: %v", err, err)
	}
}

func TestKubernetesMemoryRepository_List_ByScope(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesMemoryRepository(client, "default")
	ctx := context.Background()

	user1 := newTestMemory("u1", "User Memory", "content", entities.ScopeUser, "user-1", "")
	team1 := newTestMemory("t1", "Team Memory", "content", entities.ScopeTeam, "user-1", "org/team")
	_ = repo.Create(ctx, user1)
	_ = repo.Create(ctx, team1)

	userOnly, err := repo.List(ctx, portrepos.MemoryFilter{Scope: entities.ScopeUser})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	for _, m := range userOnly {
		if m.Scope() != entities.ScopeUser {
			t.Errorf("expected user scope, got %s", m.Scope())
		}
	}

	teamOnly, err := repo.List(ctx, portrepos.MemoryFilter{Scope: entities.ScopeTeam})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	for _, m := range teamOnly {
		if m.Scope() != entities.ScopeTeam {
			t.Errorf("expected team scope, got %s", m.Scope())
		}
	}
}

func TestKubernetesMemoryRepository_List_ByOwner(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesMemoryRepository(client, "default")
	ctx := context.Background()

	_ = repo.Create(ctx, newTestMemory("o1", "User1 Memory", "c", entities.ScopeUser, "user-1", ""))
	_ = repo.Create(ctx, newTestMemory("o2", "User2 Memory", "c", entities.ScopeUser, "user-2", ""))

	results, err := repo.List(ctx, portrepos.MemoryFilter{Scope: entities.ScopeUser, OwnerID: "user-1"})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	for _, m := range results {
		if m.OwnerID() != "user-1" {
			t.Errorf("expected ownerID 'user-1', got '%s'", m.OwnerID())
		}
	}
}

func TestKubernetesMemoryRepository_List_ByTags(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesMemoryRepository(client, "default")
	ctx := context.Background()

	m1 := entities.NewMemoryWithTags("tag1", "T1", "c", entities.ScopeUser, "user-1", "", map[string]string{"cat": "a"})
	m2 := entities.NewMemoryWithTags("tag2", "T2", "c", entities.ScopeUser, "user-1", "", map[string]string{"cat": "b"})
	_ = repo.Create(ctx, m1)
	_ = repo.Create(ctx, m2)

	results, err := repo.List(ctx, portrepos.MemoryFilter{Tags: map[string]string{"cat": "a"}})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Tags()["cat"] != "a" {
		t.Error("expected tag cat=a")
	}
}

func TestKubernetesMemoryRepository_List_ByQuery(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesMemoryRepository(client, "default")
	ctx := context.Background()

	m1 := newTestMemory("q1", "Meeting Notes", "discussed roadmap", entities.ScopeUser, "user-1", "")
	m2 := newTestMemory("q2", "Random Entry", "nothing special", entities.ScopeUser, "user-1", "")
	_ = repo.Create(ctx, m1)
	_ = repo.Create(ctx, m2)

	results, err := repo.List(ctx, portrepos.MemoryFilter{Query: "roadmap"})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for query 'roadmap', got %d", len(results))
	}
	if results[0].ID() != "q1" {
		t.Errorf("expected 'q1', got '%s'", results[0].ID())
	}
}

func TestKubernetesMemoryRepository_List_ByTeamIDs(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesMemoryRepository(client, "default")
	ctx := context.Background()

	t1 := newTestMemory("tid1", "Team A", "c", entities.ScopeTeam, "u", "org/team-a")
	t2 := newTestMemory("tid2", "Team B", "c", entities.ScopeTeam, "u", "org/team-b")
	t3 := newTestMemory("tid3", "Team C", "c", entities.ScopeTeam, "u", "org/team-c")
	_ = repo.Create(ctx, t1)
	_ = repo.Create(ctx, t2)
	_ = repo.Create(ctx, t3)

	results, err := repo.List(ctx, portrepos.MemoryFilter{
		Scope:   entities.ScopeTeam,
		TeamIDs: []string{"org/team-a", "org/team-b"},
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results for TeamIDs filter, got %d", len(results))
	}
}

func TestKubernetesMemoryRepository_Update(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesMemoryRepository(client, "default")
	ctx := context.Background()

	m := newTestMemory("upd1", "Original", "original content", entities.ScopeUser, "user-1", "")
	if err := repo.Create(ctx, m); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	m.SetTitle("Updated Title")
	m.SetContent("updated content")
	if err := repo.Update(ctx, m); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got, err := repo.GetByID(ctx, "upd1")
	if err != nil {
		t.Fatalf("GetByID after update failed: %v", err)
	}
	if got.Title() != "Updated Title" {
		t.Errorf("expected updated title, got '%s'", got.Title())
	}
	if got.Content() != "updated content" {
		t.Errorf("expected updated content, got '%s'", got.Content())
	}
}

func TestKubernetesMemoryRepository_Update_NotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesMemoryRepository(client, "default")
	ctx := context.Background()

	m := newTestMemory("nonexistent", "Title", "Content", entities.ScopeUser, "user-1", "")
	err := repo.Update(ctx, m)
	if err == nil {
		t.Fatal("expected error for update of nonexistent entry, got nil")
	}
	var notFound entities.ErrMemoryNotFound
	if !errors.As(err, &notFound) {
		t.Errorf("expected ErrMemoryNotFound, got %T: %v", err, err)
	}
}

func TestKubernetesMemoryRepository_Delete(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesMemoryRepository(client, "default")
	ctx := context.Background()

	m := newTestMemory("del1", "Title", "Content", entities.ScopeUser, "user-1", "")
	if err := repo.Create(ctx, m); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := repo.Delete(ctx, "del1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err := repo.GetByID(ctx, "del1")
	if err == nil {
		t.Fatal("expected entry to be deleted")
	}
}

func TestKubernetesMemoryRepository_Delete_NotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesMemoryRepository(client, "default")
	ctx := context.Background()

	err := repo.Delete(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for delete of nonexistent entry, got nil")
	}
	var notFound entities.ErrMemoryNotFound
	if !errors.As(err, &notFound) {
		t.Errorf("expected ErrMemoryNotFound, got %T: %v", err, err)
	}
}

func TestKubernetesMemoryRepository_TeamID_SlashPreserved(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesMemoryRepository(client, "default")
	ctx := context.Background()

	teamID := "myorg/backend-team"
	m := newTestMemory("slash-id", "Title", "Content", entities.ScopeTeam, "user-1", teamID)
	if err := repo.Create(ctx, m); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByID(ctx, "slash-id")
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.TeamID() != teamID {
		t.Errorf("expected teamID '%s', got '%s'", teamID, got.TeamID())
	}
}
