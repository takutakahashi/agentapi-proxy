package repositories

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// ---------------------------------------------------------------------------
// In-memory mock S3 client
// ---------------------------------------------------------------------------

type mockS3Object struct {
	body     []byte
	metadata map[string]string
}

type mockS3Client struct {
	mu      sync.RWMutex
	objects map[string]*mockS3Object // key -> object
}

func newMockS3Client() *mockS3Client {
	return &mockS3Client{objects: make(map[string]*mockS3Object)}
}

func (m *mockS3Client) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if params.Key == nil {
		return nil, fmt.Errorf("key is required")
	}
	body, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}
	meta := make(map[string]string, len(params.Metadata))
	for k, v := range params.Metadata {
		meta[k] = v
	}
	m.mu.Lock()
	m.objects[*params.Key] = &mockS3Object{body: body, metadata: meta}
	m.mu.Unlock()
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3Client) GetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if params.Key == nil {
		return nil, fmt.Errorf("key is required")
	}
	m.mu.RLock()
	obj, ok := m.objects[*params.Key]
	m.mu.RUnlock()
	if !ok {
		return nil, &types.NoSuchKey{}
	}
	return &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(obj.body)),
	}, nil
}

func (m *mockS3Client) HeadObject(_ context.Context, params *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	if params.Key == nil {
		return nil, fmt.Errorf("key is required")
	}
	m.mu.RLock()
	_, ok := m.objects[*params.Key]
	m.mu.RUnlock()
	if !ok {
		// Return a generic 404-like error that isS3NotFoundError will detect
		return nil, &notFoundError{}
	}
	return &s3.HeadObjectOutput{}, nil
}

func (m *mockS3Client) DeleteObject(_ context.Context, params *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if params.Key == nil {
		return nil, fmt.Errorf("key is required")
	}
	m.mu.Lock()
	delete(m.objects, *params.Key)
	m.mu.Unlock()
	return &s3.DeleteObjectOutput{}, nil
}

func (m *mockS3Client) ListObjectsV2(_ context.Context, params *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	prefix := ""
	if params.Prefix != nil {
		prefix = *params.Prefix
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var contents []types.Object
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			key := k
			contents = append(contents, types.Object{Key: aws.String(key)})
		}
	}

	return &s3.ListObjectsV2Output{
		Contents:              contents,
		IsTruncated:           aws.Bool(false),
		NextContinuationToken: nil,
	}, nil
}

// notFoundError mimics the HTTP 404 response HeadObject returns for missing keys.
type notFoundError struct{}

func (e *notFoundError) Error() string        { return "404 Not Found" }
func (e *notFoundError) HTTPStatusCode() int  { return 404 }
func (e *notFoundError) ErrorCode() string    { return "NotFound" }
func (e *notFoundError) ErrorMessage() string { return "Not Found" }
func (e *notFoundError) ErrorFault() string   { return "" }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestRepo(t *testing.T) (*S3MemoryRepository, *mockS3Client) {
	t.Helper()
	mock := newMockS3Client()
	repo := newS3MemoryRepositoryWithClient(mock, "test-bucket", "agentapi-memory/")
	return repo, mock
}

func newUserMemory(ownerID, title, content string) *entities.Memory {
	return entities.NewMemory(uuid.NewString(), title, content, entities.ScopeUser, ownerID, "")
}

func newTeamMemory(ownerID, teamID, title, content string) *entities.Memory {
	return entities.NewMemory(uuid.NewString(), title, content, entities.ScopeTeam, ownerID, teamID)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestS3MemoryRepository_Create_And_GetByID(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	m := newUserMemory("user1", "hello", "world")
	require.NoError(t, repo.Create(ctx, m))

	got, err := repo.GetByID(ctx, m.ID())
	require.NoError(t, err)
	assert.Equal(t, m.ID(), got.ID())
	assert.Equal(t, m.Title(), got.Title())
	assert.Equal(t, m.Content(), got.Content())
	assert.Equal(t, m.OwnerID(), got.OwnerID())
	assert.Equal(t, m.Scope(), got.Scope())
}

func TestS3MemoryRepository_Create_Duplicate(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	m := newUserMemory("user1", "hello", "world")
	require.NoError(t, repo.Create(ctx, m))

	err := repo.Create(ctx, m)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestS3MemoryRepository_GetByID_NotFound(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "nonexistent-id")
	assert.ErrorAs(t, err, &entities.ErrMemoryNotFound{})
}

func TestS3MemoryRepository_Update(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	m := newUserMemory("user1", "original", "content")
	require.NoError(t, repo.Create(ctx, m))

	m.SetTitle("updated title")
	m.SetContent("updated content")
	require.NoError(t, repo.Update(ctx, m))

	got, err := repo.GetByID(ctx, m.ID())
	require.NoError(t, err)
	assert.Equal(t, "updated title", got.Title())
	assert.Equal(t, "updated content", got.Content())
}

func TestS3MemoryRepository_Update_NotFound(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	m := newUserMemory("user1", "title", "content")
	err := repo.Update(ctx, m)
	assert.ErrorAs(t, err, &entities.ErrMemoryNotFound{})
}

func TestS3MemoryRepository_Delete(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	m := newUserMemory("user1", "title", "content")
	require.NoError(t, repo.Create(ctx, m))

	require.NoError(t, repo.Delete(ctx, m.ID()))

	_, err := repo.GetByID(ctx, m.ID())
	assert.ErrorAs(t, err, &entities.ErrMemoryNotFound{})
}

func TestS3MemoryRepository_Delete_NotFound(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	err := repo.Delete(ctx, "nonexistent")
	assert.ErrorAs(t, err, &entities.ErrMemoryNotFound{})
}

func TestS3MemoryRepository_List_ByScopeAndOwner(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	m1 := newUserMemory("user1", "title1", "content1")
	m2 := newUserMemory("user1", "title2", "content2")
	m3 := newUserMemory("user2", "title3", "content3")
	require.NoError(t, repo.Create(ctx, m1))
	require.NoError(t, repo.Create(ctx, m2))
	require.NoError(t, repo.Create(ctx, m3))

	memories, err := repo.List(ctx, repositories.MemoryFilter{
		Scope:   entities.ScopeUser,
		OwnerID: "user1",
	})
	require.NoError(t, err)
	assert.Len(t, memories, 2)
}

func TestS3MemoryRepository_List_TeamScope(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	m1 := newTeamMemory("user1", "team-a", "for team a", "content")
	m2 := newTeamMemory("user2", "team-b", "for team b", "content")
	require.NoError(t, repo.Create(ctx, m1))
	require.NoError(t, repo.Create(ctx, m2))

	memories, err := repo.List(ctx, repositories.MemoryFilter{
		Scope:  entities.ScopeTeam,
		TeamID: "team-a",
	})
	require.NoError(t, err)
	assert.Len(t, memories, 1)
	assert.Equal(t, "for team a", memories[0].Title())
}

func TestS3MemoryRepository_List_TeamIDs(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	m1 := newTeamMemory("user1", "team-a", "alpha", "content")
	m2 := newTeamMemory("user2", "team-b", "beta", "content")
	m3 := newTeamMemory("user3", "team-c", "gamma", "content")
	require.NoError(t, repo.Create(ctx, m1))
	require.NoError(t, repo.Create(ctx, m2))
	require.NoError(t, repo.Create(ctx, m3))

	memories, err := repo.List(ctx, repositories.MemoryFilter{
		TeamIDs: []string{"team-a", "team-b"},
	})
	require.NoError(t, err)
	assert.Len(t, memories, 2)
}

func TestS3MemoryRepository_List_TextSearch(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	m1 := newUserMemory("user1", "Go programming", "goroutines are fun")
	m2 := newUserMemory("user1", "Python tips", "list comprehensions")
	require.NoError(t, repo.Create(ctx, m1))
	require.NoError(t, repo.Create(ctx, m2))

	memories, err := repo.List(ctx, repositories.MemoryFilter{
		Scope:   entities.ScopeUser,
		OwnerID: "user1",
		Query:   "goroutine",
	})
	require.NoError(t, err)
	assert.Len(t, memories, 1)
	assert.Equal(t, "Go programming", memories[0].Title())
}

func TestS3MemoryRepository_List_TagFilter(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	m1 := entities.NewMemoryWithTags(uuid.NewString(), "tagged", "content", entities.ScopeUser, "user1", "", map[string]string{"env": "prod"})
	m2 := entities.NewMemoryWithTags(uuid.NewString(), "not tagged", "content", entities.ScopeUser, "user1", "", map[string]string{"env": "dev"})
	require.NoError(t, repo.Create(ctx, m1))
	require.NoError(t, repo.Create(ctx, m2))

	memories, err := repo.List(ctx, repositories.MemoryFilter{
		Scope:   entities.ScopeUser,
		OwnerID: "user1",
		Tags:    map[string]string{"env": "prod"},
	})
	require.NoError(t, err)
	assert.Len(t, memories, 1)
	assert.Equal(t, "tagged", memories[0].Title())
}

func TestS3MemoryRepository_List_Empty(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	memories, err := repo.List(ctx, repositories.MemoryFilter{
		Scope:   entities.ScopeUser,
		OwnerID: "nobody",
	})
	require.NoError(t, err)
	assert.NotNil(t, memories)
	assert.Empty(t, memories)
}

func TestS3MemoryRepository_ObjectKey_Structure(t *testing.T) {
	repo, mock := newTestRepo(t)
	ctx := context.Background()

	// User-scoped: key should be under user/ prefix
	mu := newUserMemory("owner1", "title", "content")
	require.NoError(t, repo.Create(ctx, mu))
	userHash := hashID("owner1")
	expectedUserKey := fmt.Sprintf("agentapi-memory/user/%s/%s.json", userHash, mu.ID())
	mock.mu.RLock()
	_, exists := mock.objects[expectedUserKey]
	mock.mu.RUnlock()
	assert.True(t, exists, "user-scoped memory should be stored at %s", expectedUserKey)

	// Team-scoped: key should be under team/ prefix
	mt := newTeamMemory("owner1", "myteam", "title", "content")
	require.NoError(t, repo.Create(ctx, mt))
	teamHash := hashID("myteam")
	expectedTeamKey := fmt.Sprintf("agentapi-memory/team/%s/%s.json", teamHash, mt.ID())
	mock.mu.RLock()
	_, exists = mock.objects[expectedTeamKey]
	mock.mu.RUnlock()
	assert.True(t, exists, "team-scoped memory should be stored at %s", expectedTeamKey)
}

func TestS3MemoryRepository_JSONRoundtrip(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	m := entities.NewMemoryWithTags("fixed-id", "My Title", "My Content", entities.ScopeUser, "user42", "", map[string]string{"key": "val"})
	m.SetCreatedAt(now)
	m.SetUpdatedAt(now)

	require.NoError(t, repo.Create(ctx, m))

	got, err := repo.GetByID(ctx, m.ID())
	require.NoError(t, err)
	assert.Equal(t, m.ID(), got.ID())
	assert.Equal(t, m.Title(), got.Title())
	assert.Equal(t, m.Content(), got.Content())
	assert.Equal(t, m.OwnerID(), got.OwnerID())
	assert.Equal(t, m.Tags(), got.Tags())
	assert.True(t, m.CreatedAt().Equal(got.CreatedAt()), "CreatedAt should match")
}

// TestMarshalUnmarshalMemoryJSON ensures the JSON round-trip is correct.
func TestMarshalUnmarshalMemoryJSON(t *testing.T) {
	m := entities.NewMemoryWithTags("id-123", "title", "content", entities.ScopeTeam, "owner", "team-x", map[string]string{"a": "b"})
	now := time.Now().Truncate(time.Millisecond)
	m.SetCreatedAt(now)
	m.SetUpdatedAt(now)

	data, err := marshalMemoryJSON(m)
	require.NoError(t, err)

	// Verify valid JSON
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.Equal(t, "id-123", raw["id"])
	assert.Equal(t, "team", raw["scope"])

	// Round-trip
	got, err := unmarshalMemoryJSON(data)
	require.NoError(t, err)
	assert.Equal(t, m.ID(), got.ID())
	assert.Equal(t, m.Title(), got.Title())
	assert.Equal(t, m.TeamID(), got.TeamID())
	assert.Equal(t, m.Tags(), got.Tags())
}
