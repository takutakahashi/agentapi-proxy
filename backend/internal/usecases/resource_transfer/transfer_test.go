package resource_transfer

import (
	"context"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type fakeMemoryRepo struct {
	memory          *entities.Memory
	updated         bool
	ignoreOwnership bool
}

func (r *fakeMemoryRepo) Create(context.Context, *entities.Memory) error { return nil }
func (r *fakeMemoryRepo) GetByID(context.Context, string) (*entities.Memory, error) {
	return cloneMemory(r.memory), nil
}
func (r *fakeMemoryRepo) List(context.Context, portrepos.MemoryFilter) ([]*entities.Memory, error) {
	return nil, nil
}
func (r *fakeMemoryRepo) Update(_ context.Context, memory *entities.Memory) error {
	if r.ignoreOwnership {
		r.updated = true
		return nil
	}
	r.memory = memory
	r.updated = true
	return nil
}
func (r *fakeMemoryRepo) Delete(context.Context, string) error { return nil }

func cloneMemory(m *entities.Memory) *entities.Memory {
	if m == nil {
		return nil
	}
	clone := entities.NewMemoryWithTags(m.ID(), m.Title(), m.Content(), m.Scope(), m.OwnerID(), m.TeamID(), m.Tags())
	clone.SetCreatedAt(m.CreatedAt())
	clone.SetUpdatedAt(m.UpdatedAt())
	return clone
}

func TestTransferMemoryDryRunDoesNotUpdate(t *testing.T) {
	repo := &fakeMemoryRepo{memory: entities.NewMemory("mem-1", "title", "content", entities.ScopeUser, "user-1", "")}
	uc := New(WithMemoryRepository(repo))

	res, err := uc.Transfer(context.Background(), Request{
		ResourceType: ResourceMemory,
		ResourceID:   "mem-1",
		TargetScope:  entities.ScopeTeam,
		TargetTeamID: "org/team-a",
		DryRun:       true,
		Actor:        teamMember("user-1", "org/team-a"),
	})
	if err != nil {
		t.Fatalf("Transfer() error = %v", err)
	}
	if res.Status != "dry_run" {
		t.Fatalf("expected dry_run status, got %q", res.Status)
	}
	if repo.updated {
		t.Fatal("dry_run should not update repository")
	}
	if repo.memory.Scope() != entities.ScopeUser {
		t.Fatalf("dry_run changed memory scope to %q", repo.memory.Scope())
	}
}

func TestTransferMemoryToAnotherUserRequiresAdmin(t *testing.T) {
	repo := &fakeMemoryRepo{memory: entities.NewMemory("mem-1", "title", "content", entities.ScopeUser, "user-1", "")}
	uc := New(WithMemoryRepository(repo))

	_, err := uc.Transfer(context.Background(), Request{
		ResourceType: ResourceMemory,
		ResourceID:   "mem-1",
		TargetScope:  entities.ScopeUser,
		TargetUserID: "user-2",
		Actor:        entities.NewUser("user-1", entities.UserTypeRegular, "user-1"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if repo.updated {
		t.Fatal("forbidden transfer should not update repository")
	}
}

func TestTransferMemoryToTeamUpdatesOwnership(t *testing.T) {
	repo := &fakeMemoryRepo{memory: entities.NewMemory("mem-1", "title", "content", entities.ScopeUser, "user-1", "")}
	uc := New(WithMemoryRepository(repo))

	_, err := uc.Transfer(context.Background(), Request{
		ResourceType: ResourceMemory,
		ResourceID:   "mem-1",
		TargetScope:  entities.ScopeTeam,
		TargetTeamID: "org/team-a",
		Actor:        teamMember("user-1", "org/team-a"),
	})
	if err != nil {
		t.Fatalf("Transfer() error = %v", err)
	}
	if !repo.updated {
		t.Fatal("expected repository update")
	}
	if repo.memory.Scope() != entities.ScopeTeam || repo.memory.TeamID() != "org/team-a" || repo.memory.OwnerID() != "user-1" {
		t.Fatalf("unexpected ownership: scope=%q owner=%q team=%q", repo.memory.Scope(), repo.memory.OwnerID(), repo.memory.TeamID())
	}
}

func TestTransferMemoryErrorsWhenOwnershipIsNotPersisted(t *testing.T) {
	repo := &fakeMemoryRepo{
		memory:          entities.NewMemory("mem-1", "title", "content", entities.ScopeUser, "user-1", ""),
		ignoreOwnership: true,
	}
	uc := New(WithMemoryRepository(repo))

	_, err := uc.Transfer(context.Background(), Request{
		ResourceType: ResourceMemory,
		ResourceID:   "mem-1",
		TargetScope:  entities.ScopeTeam,
		TargetTeamID: "org/team-a",
		Actor:        teamMember("user-1", "org/team-a"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ownership transfer was not persisted") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func teamMember(userID, teamID string) *entities.User {
	user := entities.NewGitHubUser(entities.UserID(userID), userID, "", entities.NewGitHubUserInfo(1, userID, userID, "", "", "", ""))
	parts := strings.SplitN(teamID, "/", 2)
	user.SetGitHubInfo(user.GitHubInfo(), []entities.GitHubTeamMembership{{Organization: parts[0], TeamSlug: parts[1]}})
	return user
}
