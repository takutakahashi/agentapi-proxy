package githubsync

import "time"

// SyncMeta is stored as .sync-meta.yaml at the rootPath in the GitHub repository.
// It records the last sync timestamp and encryption parameters (NOT the DEK itself).
type SyncMeta struct {
	APIVersion string      `yaml:"apiVersion"`
	Kind       string      `yaml:"kind"`
	SyncedAt   time.Time   `yaml:"syncedAt"`
	Version    string      `yaml:"version"`
	Encryption SyncMetaEnc `yaml:"encryption"`
}

// SyncMetaEnc holds encryption metadata stored in .sync-meta.yaml.
// The encryptedDEK is NOT stored here — it lives in Settings (K8s Secret).
type SyncMetaEnc struct {
	Provider   string `yaml:"provider"` // always "aws_kms"
	KMSKeyARN  string `yaml:"kmsKeyArn"`
	Algorithm  string `yaml:"algorithm"` // "AES-256-GCM"
	DEKVersion int    `yaml:"dekVersion"`
}

// PushRequest is the HTTP request body for POST /sync/push.
type PushRequest struct {
	// CommitMessage overrides the default commit message.
	CommitMessage string `json:"commit_message,omitempty"`
}

// PushResponse is the HTTP response body for POST /sync/push.
type PushResponse struct {
	CommitSHA string      `json:"commit_sha"`
	PushedAt  time.Time   `json:"pushed_at"`
	Summary   SyncSummary `json:"summary"`
}

// PullRequest is the HTTP request body for POST /sync/pull.
type PullRequest struct {
	// DeleteOrphans removes local resources that no longer exist in GitHub.
	DeleteOrphans bool `json:"delete_orphans,omitempty"`
}

// PullResponse is the HTTP response body for POST /sync/pull.
type PullResponse struct {
	PulledAt time.Time   `json:"pulled_at"`
	Summary  SyncSummary `json:"summary"`
}

// RotateKeyResponse is the HTTP response body for POST /sync/rotate-key.
type RotateKeyResponse struct {
	CommitSHA  string    `json:"commit_sha"`
	RotatedAt  time.Time `json:"rotated_at"`
	DEKVersion int       `json:"dek_version"`
}

// SyncSummary summarises what happened during a push or pull.
type SyncSummary struct {
	FilesWritten int `json:"files_written"`
	FilesDeleted int `json:"files_deleted"`
}

// SyncEventRecord records the result of the last push or pull.
type SyncEventRecord struct {
	At        time.Time   `json:"at"`
	CommitSHA string      `json:"commit_sha,omitempty"`
	Summary   SyncSummary `json:"summary"`
}

// SyncAllRequest is the HTTP request body for POST /sync/all.
type SyncAllRequest struct {
	DeleteOrphans bool   `json:"delete_orphans,omitempty"`
	CommitMessage string `json:"commit_message,omitempty"`
}

// SyncAllResponse summarises the result of syncing all tenants.
type SyncAllResponse struct {
	SyncedAt time.Time       `json:"synced_at"`
	Results  []SyncAllResult `json:"results"`
}

// SyncAllResult holds the result for a single settings tenant.
// Direction is "push" or "pull", determined automatically by comparing
// the remote .sync-meta.yaml syncedAt against the local LastPushedAt.
type SyncAllResult struct {
	SettingsName string        `json:"settings_name"`
	Direction    string        `json:"direction"`
	Push         *PushResponse `json:"push,omitempty"`
	Pull         *PullResponse `json:"pull,omitempty"`
	Error        string        `json:"error,omitempty"`
}
