package repositories

import (
	"context"
	"encoding/json"
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
