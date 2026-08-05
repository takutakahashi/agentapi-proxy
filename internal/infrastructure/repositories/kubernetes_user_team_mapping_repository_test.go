package repositories

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

func TestKubernetesUserTeamMappingRepository_Get_Fresh(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesUserTeamMappingRepository(client, "default")
	ctx := context.Background()

	teams := []auth.GitHubTeamMembership{{Organization: "org", TeamSlug: "team", TeamName: "team", Role: "pull"}}
	if err := repo.Set(ctx, "alice", teams); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, found, err := repo.Get(ctx, "alice")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !found {
		t.Fatal("expected found=true for fresh entry")
	}
	if len(got) != 1 || got[0].Organization != "org" {
		t.Fatalf("unexpected teams: %v", got)
	}
}

func TestKubernetesUserTeamMappingRepository_Get_Expired(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesUserTeamMappingRepository(client, "default")
	ctx := context.Background()

	// Seed ConfigMap with an entry whose updated_at is older than TTL
	staleEntry := userTeamMappingEntry{
		Teams:     []auth.GitHubTeamMembership{{Organization: "org", TeamSlug: "team", TeamName: "team", Role: "pull"}},
		UpdatedAt: time.Now().Add(-10 * time.Minute),
	}
	raw, _ := json.Marshal(staleEntry)
	cm := repo.buildConfigMap(map[string]string{"bob": string(raw)})
	if _, err := client.CoreV1().ConfigMaps("default").Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed ConfigMap failed: %v", err)
	}

	_, found, err := repo.Get(ctx, "bob")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if found {
		t.Fatal("expected found=false for expired entry")
	}
}

func TestKubernetesUserTeamMappingRepository_Get_NotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesUserTeamMappingRepository(client, "default")
	ctx := context.Background()

	_, found, err := repo.Get(ctx, "nobody")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if found {
		t.Fatal("expected found=false for nonexistent user")
	}
}

func TestKubernetesUserTeamMappingRepository_Set_Concurrent(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesUserTeamMappingRepository(client, "default")
	ctx := context.Background()

	const numGoroutines = 10
	users := make([]string, numGoroutines)
	for i := range users {
		users[i] = "user" + string(rune('a'+i))
	}
	teams := []auth.GitHubTeamMembership{{Organization: "org", TeamSlug: "team", TeamName: "team", Role: "pull"}}

	var wg sync.WaitGroup
	errs := make([]error, numGoroutines)
	for i, u := range users {
		wg.Add(1)
		go func(idx int, username string) {
			defer wg.Done()
			errs[idx] = repo.Set(ctx, username, teams)
		}(i, u)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Set(%s) failed: %v", users[i], err)
		}
	}

	// All users should be readable
	for _, u := range users {
		got, found, err := repo.Get(ctx, u)
		if err != nil {
			t.Errorf("Get(%s) failed: %v", u, err)
			continue
		}
		if !found {
			t.Errorf("Get(%s): expected found=true", u)
			continue
		}
		if len(got) != 1 {
			t.Errorf("Get(%s): expected 1 team, got %d", u, len(got))
		}
	}
}
