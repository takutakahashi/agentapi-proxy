package repositories

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const (
	// UserFilesSecretPrefix is the prefix for agentapi-user-files-* Secrets.
	UserFilesSecretPrefix = "agentapi-user-files-"

	// LabelUserFiles is the label key for user-files Secrets.
	LabelUserFiles = "agentapi.proxy/user-files"

	// Annotation keys for user-files Secrets.
	AnnotationUserFilesUpdatedAt = "agentapi.proxy/user-files-updated-at"
)

// KubernetesUserFileRepository implements UserFileRepository using a
// agentapi-user-files-{userID} Kubernetes Secret with index-based KV format.
//
// Each file is stored as:
//
//	"{i}.id"          → UUID
//	"{i}.name"        → display name
//	"{i}.path"        → destination path inside the container
//	"{i}.content"     → file content
//	"{i}.permissions" → permissions string (optional, e.g. "0600")
type KubernetesUserFileRepository struct {
	client    kubernetes.Interface
	namespace string
}

// NewKubernetesUserFileRepository creates a new KubernetesUserFileRepository.
func NewKubernetesUserFileRepository(client kubernetes.Interface, namespace string) *KubernetesUserFileRepository {
	return &KubernetesUserFileRepository{
		client:    client,
		namespace: namespace,
	}
}

// Save creates or updates a file entry inside the user's Secret.
func (r *KubernetesUserFileRepository) Save(ctx context.Context, userID string, file *entities.UserFile) error {
	if err := file.Validate(); err != nil {
		return fmt.Errorf("invalid user file: %w", err)
	}

	secretName := r.secretName(userID)
	now := time.Now().UTC().Format(time.RFC3339)

	// Load existing files to preserve others.
	existing, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	var currentFiles []*entities.UserFile
	if err == nil {
		currentFiles = secretDataToUserFiles(existing.Data)
	}

	// Update existing entry or append a new one.
	updated := false
	for i, f := range currentFiles {
		if f.ID() == file.ID() {
			currentFiles[i] = file
			updated = true
			break
		}
	}
	if !updated {
		currentFiles = append(currentFiles, file)
	}

	secretData := userFilesToSecretData(currentFiles)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: r.namespace,
			Labels: map[string]string{
				LabelUserFiles: "true",
			},
			Annotations: map[string]string{
				AnnotationUserFilesUpdatedAt: now,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: secretData,
	}

	_, createErr := r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if createErr != nil {
		if errors.IsAlreadyExists(createErr) {
			existing.Data = secretData
			existing.Labels = secret.Labels
			if existing.Annotations == nil {
				existing.Annotations = map[string]string{}
			}
			existing.Annotations[AnnotationUserFilesUpdatedAt] = now
			_, updateErr := r.client.CoreV1().Secrets(r.namespace).Update(ctx, existing, metav1.UpdateOptions{})
			if updateErr != nil {
				return fmt.Errorf("failed to update user files secret: %w", updateErr)
			}
			return nil
		}
		return fmt.Errorf("failed to create user files secret: %w", createErr)
	}
	return nil
}

// FindByID retrieves a single file by ID from the user's Secret.
func (r *KubernetesUserFileRepository) FindByID(ctx context.Context, userID string, fileID string) (*entities.UserFile, error) {
	files, err := r.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if f.ID() == fileID {
			return f, nil
		}
	}
	return nil, fmt.Errorf("user file not found: %s", fileID)
}

// List retrieves all files for the given user.
func (r *KubernetesUserFileRepository) List(ctx context.Context, userID string) ([]*entities.UserFile, error) {
	secretName := r.secretName(userID)
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return []*entities.UserFile{}, nil
		}
		return nil, fmt.Errorf("failed to get user files secret: %w", err)
	}
	return secretDataToUserFiles(secret.Data), nil
}

// Delete removes a file entry from the user's Secret.
func (r *KubernetesUserFileRepository) Delete(ctx context.Context, userID string, fileID string) error {
	secretName := r.secretName(userID)

	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("user file not found: %s", fileID)
		}
		return fmt.Errorf("failed to get user files secret: %w", err)
	}

	current := secretDataToUserFiles(secret.Data)
	filtered := make([]*entities.UserFile, 0, len(current))
	found := false
	for _, f := range current {
		if f.ID() == fileID {
			found = true
			continue
		}
		filtered = append(filtered, f)
	}
	if !found {
		return fmt.Errorf("user file not found: %s", fileID)
	}

	if len(filtered) == 0 {
		// Delete the secret entirely when no files remain.
		deleteErr := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
		if deleteErr != nil && !errors.IsNotFound(deleteErr) {
			return fmt.Errorf("failed to delete user files secret: %w", deleteErr)
		}
		return nil
	}

	secret.Data = userFilesToSecretData(filtered)
	if _, updateErr := r.client.CoreV1().Secrets(r.namespace).Update(ctx, secret, metav1.UpdateOptions{}); updateErr != nil {
		return fmt.Errorf("failed to update user files secret after deletion: %w", updateErr)
	}
	return nil
}

// ToManagedFiles converts all user files to the ManagedFile format used by
// SessionSettings so they can be written to the container by the provisioner.
func (r *KubernetesUserFileRepository) ToManagedFiles(ctx context.Context, userID string) ([]sessionsettings.ManagedFile, error) {
	files, err := r.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	managed := make([]sessionsettings.ManagedFile, 0, len(files))
	for _, f := range files {
		managed = append(managed, sessionsettings.ManagedFile{
			Path:    f.Path(),
			Content: f.Content(),
		})
	}
	return managed, nil
}

// secretName returns the Secret name for the given userID.
func (r *KubernetesUserFileRepository) secretName(userID string) string {
	sanitized := sanitizeSecretName(userID)
	maxLen := 253 - len(UserFilesSecretPrefix)
	if len(sanitized) > maxLen {
		sanitized = sanitized[:maxLen]
	}
	return UserFilesSecretPrefix + sanitized
}

// --- serialization helpers ---

// userFilesToSecretData serialises a slice of UserFile into a flat map
// suitable for storing in a Kubernetes Secret.
//
// Format:
//
//	"{i}.id"          → file.ID()
//	"{i}.name"        → file.Name()
//	"{i}.path"        → file.Path()
//	"{i}.content"     → file.Content()
//	"{i}.permissions" → file.Permissions()  (omitted when empty)
//	"{i}.created_at"  → RFC3339 timestamp
//	"{i}.updated_at"  → RFC3339 timestamp
func userFilesToSecretData(files []*entities.UserFile) map[string][]byte {
	data := make(map[string][]byte, len(files)*7)
	for i, f := range files {
		prefix := strconv.Itoa(i)
		data[prefix+".id"] = []byte(f.ID())
		data[prefix+".name"] = []byte(f.Name())
		data[prefix+".path"] = []byte(f.Path())
		data[prefix+".content"] = []byte(f.Content())
		if f.Permissions() != "" {
			data[prefix+".permissions"] = []byte(f.Permissions())
		}
		data[prefix+".created_at"] = []byte(f.CreatedAt().UTC().Format(time.RFC3339))
		data[prefix+".updated_at"] = []byte(f.UpdatedAt().UTC().Format(time.RFC3339))
	}
	return data
}

// secretDataToUserFiles reconstructs a slice of UserFile from the flat map
// produced by userFilesToSecretData.
func secretDataToUserFiles(data map[string][]byte) []*entities.UserFile {
	// Collect unique indices.
	indexSet := map[int]struct{}{}
	for k := range data {
		if idx, ok := parseUserFileSecretKey(k); ok {
			indexSet[idx] = struct{}{}
		}
	}

	indices := make([]int, 0, len(indexSet))
	for idx := range indexSet {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	files := make([]*entities.UserFile, 0, len(indices))
	for _, idx := range indices {
		prefix := strconv.Itoa(idx)
		id := string(data[prefix+".id"])
		if id == "" {
			continue
		}
		path := string(data[prefix+".path"])
		if path == "" {
			continue
		}
		name := string(data[prefix+".name"])
		content := string(data[prefix+".content"])
		permissions := string(data[prefix+".permissions"])

		f := entities.NewUserFile(id, name, path, content, permissions)
		if v, ok := data[prefix+".created_at"]; ok {
			if t, err := time.Parse(time.RFC3339, string(v)); err == nil {
				f.SetCreatedAt(t)
			}
		}
		if v, ok := data[prefix+".updated_at"]; ok {
			if t, err := time.Parse(time.RFC3339, string(v)); err == nil {
				f.SetUpdatedAt(t)
			}
		}
		files = append(files, f)
	}
	return files
}

// parseUserFileSecretKey parses a key like "0.id", "1.path", etc.
// and returns the index and true on success.
var userFileSecretSuffixes = []string{"id", "name", "path", "content", "permissions", "created_at", "updated_at"}

func parseUserFileSecretKey(k string) (int, bool) {
	dot := strings.LastIndex(k, ".")
	if dot < 0 {
		return 0, false
	}
	suffix := k[dot+1:]
	valid := false
	for _, s := range userFileSecretSuffixes {
		if suffix == s {
			valid = true
			break
		}
	}
	if !valid {
		return 0, false
	}
	idx, err := strconv.Atoi(k[:dot])
	if err != nil {
		return 0, false
	}
	return idx, true
}
