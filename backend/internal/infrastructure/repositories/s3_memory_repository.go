package repositories

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// S3Client is an interface for the S3 operations used by S3MemoryRepository.
// This allows injecting a mock client in tests.
type S3Client interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

// S3MemoryRepository implements MemoryRepository using Amazon S3 (or S3-compatible storage).
//
// Object key structure:
//
//	{prefix}user/{owner-hash}/{id}.json   – user-scoped memories
//	{prefix}team/{team-hash}/{id}.json    – team-scoped memories
//
// The owner/team hash is the first 16 hex chars of SHA-256, identical to the
// hash used in KubernetesMemoryRepository. This design allows ListObjectsV2
// with a prefix to efficiently filter by scope and owner/team.
type S3MemoryRepository struct {
	client S3Client
	bucket string
	prefix string // e.g. "agentapi-memory/"
}

// NewS3MemoryRepository creates a new S3MemoryRepository from configuration.
func NewS3MemoryRepository(ctx context.Context, cfg *config.MemoryS3Config) (*S3MemoryRepository, error) {
	if cfg == nil {
		return nil, fmt.Errorf("S3 memory config is required")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("S3 bucket name is required")
	}

	opts := []func(*awsconfig.LoadOptions) error{}
	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	s3Opts := []func(*s3.Options){}
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true // Required for S3-compatible endpoints (e.g. rustfs, localstack)
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)

	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "agentapi-memory/"
	}
	// Ensure prefix ends with "/"
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	return &S3MemoryRepository{
		client: client,
		bucket: cfg.Bucket,
		prefix: prefix,
	}, nil
}

// newS3MemoryRepositoryWithClient creates an S3MemoryRepository with a custom client (for testing).
func newS3MemoryRepositoryWithClient(client S3Client, bucket, prefix string) *S3MemoryRepository {
	if prefix == "" {
		prefix = "agentapi-memory/"
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return &S3MemoryRepository{
		client: client,
		bucket: bucket,
		prefix: prefix,
	}
}

// objectKey returns the S3 object key for the given memory entry.
func (r *S3MemoryRepository) objectKey(m *entities.Memory) string {
	switch m.Scope() {
	case entities.ScopeTeam:
		return r.teamKey(m.TeamID(), m.ID())
	default:
		return r.userKey(m.OwnerID(), m.ID())
	}
}

func (r *S3MemoryRepository) userKey(ownerID, id string) string {
	return fmt.Sprintf("%suser/%s/%s.json", r.prefix, hashID(ownerID), id)
}

func (r *S3MemoryRepository) teamKey(teamID, id string) string {
	return fmt.Sprintf("%steam/%s/%s.json", r.prefix, hashID(teamID), id)
}

// Create persists a new memory entry to S3.
// Returns an error if an entry with the same ID already exists.
func (r *S3MemoryRepository) Create(ctx context.Context, memory *entities.Memory) error {
	if err := memory.Validate(); err != nil {
		return fmt.Errorf("invalid memory entry: %w", err)
	}

	key := r.objectKey(memory)

	// Check for existence to prevent overwrite
	_, err := r.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err == nil {
		// Object already exists
		return fmt.Errorf("memory entry already exists: %s", memory.ID())
	}
	if !isS3NotFoundError(err) {
		return fmt.Errorf("failed to check memory existence: %w", err)
	}

	data, err := marshalMemoryJSON(memory)
	if err != nil {
		return fmt.Errorf("failed to marshal memory: %w", err)
	}

	_, err = r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(r.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
		Metadata:    buildS3Metadata(memory),
	})
	if err != nil {
		return fmt.Errorf("failed to put memory object: %w", err)
	}

	return nil
}

// GetByID retrieves a memory entry by its UUID.
// It probes both user and team key prefixes to locate the object.
func (r *S3MemoryRepository) GetByID(ctx context.Context, id string) (*entities.Memory, error) {
	// We don't know the scope/owner ahead of time, so we list with suffix id.json
	// across all prefixes. Typically IDs are UUIDs so collisions are not a concern.
	// Try listing with the full prefix and match by id in the key.
	mem, err := r.findByID(ctx, r.prefix, id)
	if err != nil {
		return nil, err
	}
	if mem == nil {
		return nil, entities.ErrMemoryNotFound{ID: id}
	}
	return mem, nil
}

// findByID lists objects under prefix and returns the first whose key ends with "/{id}.json".
func (r *S3MemoryRepository) findByID(ctx context.Context, prefix, id string) (*entities.Memory, error) {
	suffix := "/" + id + ".json"
	paginator := s3.NewListObjectsV2Paginator(r.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(r.bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list memory objects: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}
			if strings.HasSuffix(*obj.Key, suffix) {
				return r.getByKey(ctx, *obj.Key)
			}
		}
	}
	return nil, nil
}

// getByKey fetches and deserializes a memory object by its S3 key.
func (r *S3MemoryRepository) getByKey(ctx context.Context, key string) (*entities.Memory, error) {
	out, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3NotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get memory object %s: %w", key, err)
	}
	defer func() { _ = out.Body.Close() }()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read memory object body: %w", err)
	}

	return unmarshalMemoryJSON(data)
}

// List retrieves memory entries matching the filter.
// ListObjectsV2 with a computed prefix is used to narrow candidates;
// tag and text filtering is performed in-process.
func (r *S3MemoryRepository) List(ctx context.Context, filter repositories.MemoryFilter) ([]*entities.Memory, error) {
	// If TeamIDs (OR logic) is provided, do multiple prefix queries and merge.
	if len(filter.TeamIDs) > 0 {
		return r.listForTeamIDs(ctx, filter)
	}

	listPrefix := r.buildListPrefix(filter)
	result, err := r.listWithPrefix(ctx, listPrefix, filter)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = []*entities.Memory{}
	}
	return result, nil
}

// listForTeamIDs handles the OR-logic TeamIDs filter by issuing one query per team.
func (r *S3MemoryRepository) listForTeamIDs(ctx context.Context, filter repositories.MemoryFilter) ([]*entities.Memory, error) {
	seen := make(map[string]struct{})
	var result []*entities.Memory

	for _, teamID := range filter.TeamIDs {
		prefix := fmt.Sprintf("%steam/%s/", r.prefix, hashID(teamID))
		// Build a per-team filter (preserve tags and query but fix TeamID)
		teamFilter := repositories.MemoryFilter{
			Scope:  entities.ScopeTeam,
			TeamID: teamID,
			Tags:   filter.Tags,
			Query:  filter.Query,
		}
		memories, err := r.listWithPrefix(ctx, prefix, teamFilter)
		if err != nil {
			return nil, err
		}
		for _, m := range memories {
			if _, ok := seen[m.ID()]; !ok {
				seen[m.ID()] = struct{}{}
				result = append(result, m)
			}
		}
	}

	if result == nil {
		result = []*entities.Memory{}
	}
	return result, nil
}

// buildListPrefix builds the S3 key prefix for a List query.
func (r *S3MemoryRepository) buildListPrefix(filter repositories.MemoryFilter) string {
	switch {
	case filter.Scope == entities.ScopeUser && filter.OwnerID != "":
		return fmt.Sprintf("%suser/%s/", r.prefix, hashID(filter.OwnerID))
	case filter.Scope == entities.ScopeTeam && filter.TeamID != "":
		return fmt.Sprintf("%steam/%s/", r.prefix, hashID(filter.TeamID))
	case filter.Scope == entities.ScopeUser:
		return fmt.Sprintf("%suser/", r.prefix)
	case filter.Scope == entities.ScopeTeam:
		return fmt.Sprintf("%steam/", r.prefix)
	default:
		return r.prefix
	}
}

// listWithPrefix lists all objects under prefix and applies in-process filtering.
func (r *S3MemoryRepository) listWithPrefix(ctx context.Context, prefix string, filter repositories.MemoryFilter) ([]*entities.Memory, error) {
	paginator := s3.NewListObjectsV2Paginator(r.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(r.bucket),
		Prefix: aws.String(prefix),
	})

	var result []*entities.Memory
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list memory objects: %w", err)
		}

		for _, obj := range page.Contents {
			if obj.Key == nil || !strings.HasSuffix(*obj.Key, ".json") {
				continue
			}
			memory, err := r.getByKey(ctx, *obj.Key)
			if err != nil || memory == nil {
				// Skip malformed or missing entries
				continue
			}

			// In-process filter: OwnerID exact match when prefix was broader
			if filter.OwnerID != "" && memory.OwnerID() != filter.OwnerID {
				continue
			}
			// In-process filter: TeamID exact match when prefix was broader
			if filter.TeamID != "" && memory.TeamID() != filter.TeamID {
				continue
			}
			// In-process filter: tags (must contain ALL filter tags)
			if !memory.MatchesTags(filter.Tags) {
				continue
			}
			// In-process filter: full-text search (title + content)
			if !memory.MatchesText(filter.Query) {
				continue
			}

			result = append(result, memory)
		}
	}

	return result, nil
}

// Update replaces an existing memory entry's content in S3.
func (r *S3MemoryRepository) Update(ctx context.Context, memory *entities.Memory) error {
	if err := memory.Validate(); err != nil {
		return fmt.Errorf("invalid memory entry: %w", err)
	}

	key := r.objectKey(memory)

	// Verify the entry exists
	_, err := r.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3NotFoundError(err) {
			return entities.ErrMemoryNotFound{ID: memory.ID()}
		}
		return fmt.Errorf("failed to check memory existence for update: %w", err)
	}

	data, err := marshalMemoryJSON(memory)
	if err != nil {
		return fmt.Errorf("failed to marshal memory: %w", err)
	}

	_, err = r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(r.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
		Metadata:    buildS3Metadata(memory),
	})
	if err != nil {
		return fmt.Errorf("failed to put memory object: %w", err)
	}

	return nil
}

// Delete removes a memory entry from S3.
func (r *S3MemoryRepository) Delete(ctx context.Context, id string) error {
	// Find the key first (we don't know scope/owner)
	mem, err := r.findByID(ctx, r.prefix, id)
	if err != nil {
		return fmt.Errorf("failed to find memory for deletion: %w", err)
	}
	if mem == nil {
		return entities.ErrMemoryNotFound{ID: id}
	}

	key := r.objectKey(mem)
	_, err = r.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3NotFoundError(err) {
			return entities.ErrMemoryNotFound{ID: id}
		}
		return fmt.Errorf("failed to delete memory object: %w", err)
	}

	return nil
}

// buildS3Metadata builds the S3 user-defined metadata map for a memory entry.
func buildS3Metadata(m *entities.Memory) map[string]string {
	metadata := map[string]string{
		"owner-id": m.OwnerID(),
		"scope":    string(m.Scope()),
	}
	if m.TeamID() != "" {
		metadata["team-id"] = m.TeamID()
	}
	return metadata
}

// marshalMemoryJSON serializes a Memory entity to JSON bytes.
// Reuses the same memoryJSON struct as the Kubernetes implementation.
func marshalMemoryJSON(m *entities.Memory) ([]byte, error) {
	mj := &memoryJSON{
		ID:        m.ID(),
		Title:     m.Title(),
		Content:   m.Content(),
		Tags:      m.Tags(),
		Scope:     string(m.Scope()),
		OwnerID:   m.OwnerID(),
		TeamID:    m.TeamID(),
		CreatedAt: m.CreatedAt(),
		UpdatedAt: m.UpdatedAt(),
	}
	return json.Marshal(mj)
}

// unmarshalMemoryJSON deserializes JSON bytes into a Memory entity.
func unmarshalMemoryJSON(data []byte) (*entities.Memory, error) {
	var mj memoryJSON
	if err := json.Unmarshal(data, &mj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal memory JSON: %w", err)
	}

	m := entities.NewMemoryWithTags(
		mj.ID,
		mj.Title,
		mj.Content,
		entities.ResourceScope(mj.Scope),
		mj.OwnerID,
		mj.TeamID,
		mj.Tags,
	)
	m.SetCreatedAt(mj.CreatedAt)
	m.SetUpdatedAt(mj.UpdatedAt)

	return m, nil
}

// isS3NotFoundError reports whether the error represents a 404 Not Found from S3.
func isS3NotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var nf *types.NoSuchKey
	if errors.As(err, &nf) {
		return true
	}
	// HeadObject returns NotFound (generic HTTP 404) rather than NoSuchKey
	var apiErr interface{ HTTPStatusCode() int }
	if errors.As(err, &apiErr) {
		return apiErr.HTTPStatusCode() == 404
	}
	// Fallback: check error message
	return strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "NoSuchKey")
}

// Ensure S3MemoryRepository implements MemoryRepository at compile time.
var _ repositories.MemoryRepository = (*S3MemoryRepository)(nil)
