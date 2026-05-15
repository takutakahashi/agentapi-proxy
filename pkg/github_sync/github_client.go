package githubsync

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
)

// GitHubSyncClient wraps the GitHub Git Data API for atomic batch push/pull.
type GitHubSyncClient struct {
	client *github.Client
	owner  string
	repo   string
}

// NewGitHubSyncClient creates a client authenticated with the given PAT.
// repoFullName must be "owner/repo".
func NewGitHubSyncClient(ctx context.Context, token, repoFullName string) (*GitHubSyncClient, error) {
	parts := strings.SplitN(repoFullName, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("repoFullName must be 'owner/repo', got %q", repoFullName)
	}

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	return &GitHubSyncClient{
		client: github.NewClient(tc),
		owner:  parts[0],
		repo:   parts[1],
	}, nil
}

// PushFiles commits files to the repository in a single atomic commit.
// files maps repo-relative paths to content. nil content entries delete the file.
// Returns the new commit SHA.
func (c *GitHubSyncClient) PushFiles(ctx context.Context, branch, commitMessage string, files map[string][]byte) (string, error) {
	// 1. Resolve current HEAD SHA
	ref, _, err := c.client.Git.GetRef(ctx, c.owner, c.repo, "refs/heads/"+branch)
	if err != nil {
		return "", fmt.Errorf("get ref %s: %w", branch, err)
	}
	headSHA := ref.Object.GetSHA()

	// 2. Get base tree SHA from HEAD commit
	commit, _, err := c.client.Git.GetCommit(ctx, c.owner, c.repo, headSHA)
	if err != nil {
		return "", fmt.Errorf("get commit %s: %w", headSHA, err)
	}
	baseTreeSHA := commit.Tree.GetSHA()

	// 3. Build tree entries
	entries := make([]*github.TreeEntry, 0, len(files))
	for path, content := range files {
		if content == nil {
			// Deletion: SHA = nil clears the entry
			entries = append(entries, &github.TreeEntry{
				Path: github.String(path),
				Mode: github.String("100644"),
				Type: github.String("blob"),
			})
			continue
		}
		encoded := base64.StdEncoding.EncodeToString(content)
		blob, _, err := c.client.Git.CreateBlob(ctx, c.owner, c.repo, &github.Blob{
			Content:  github.String(encoded),
			Encoding: github.String("base64"),
		})
		if err != nil {
			return "", fmt.Errorf("create blob for %s: %w", path, err)
		}
		entries = append(entries, &github.TreeEntry{
			Path: github.String(path),
			Mode: github.String("100644"),
			Type: github.String("blob"),
			SHA:  blob.SHA,
		})
	}

	if len(entries) == 0 {
		return headSHA, nil
	}

	// 4. Create new tree on top of base tree
	newTree, _, err := c.client.Git.CreateTree(ctx, c.owner, c.repo, baseTreeSHA, entries)
	if err != nil {
		return "", fmt.Errorf("create tree: %w", err)
	}

	// 5. Create commit
	newCommit, _, err := c.client.Git.CreateCommit(ctx, c.owner, c.repo, &github.Commit{
		Message: github.String(commitMessage),
		Tree:    &github.Tree{SHA: newTree.SHA},
		Parents: []*github.Commit{{SHA: github.String(headSHA)}},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("create commit: %w", err)
	}

	// 6. Fast-forward branch ref
	_, _, err = c.client.Git.UpdateRef(ctx, c.owner, c.repo, &github.Reference{
		Ref:    github.String("refs/heads/" + branch),
		Object: &github.GitObject{SHA: newCommit.SHA},
	}, false)
	if err != nil {
		return "", fmt.Errorf("update ref: %w", err)
	}

	return newCommit.GetSHA(), nil
}

// ListFiles returns repo-relative paths of all blobs under the given prefix on the branch.
func (c *GitHubSyncClient) ListFiles(ctx context.Context, branch, prefix string) ([]string, error) {
	ref, _, err := c.client.Git.GetRef(ctx, c.owner, c.repo, "refs/heads/"+branch)
	if err != nil {
		return nil, fmt.Errorf("get ref %s: %w", branch, err)
	}

	tree, _, err := c.client.Git.GetTree(ctx, c.owner, c.repo, ref.Object.GetSHA(), true)
	if err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}

	var paths []string
	for _, entry := range tree.Entries {
		if entry.GetType() != "blob" {
			continue
		}
		p := entry.GetPath()
		if strings.HasPrefix(p, prefix) {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// GetFile returns the decoded content of a single file from the repository.
func (c *GitHubSyncClient) GetFile(ctx context.Context, branch, path string) ([]byte, error) {
	opts := &github.RepositoryContentGetOptions{Ref: branch}
	fc, _, _, err := c.client.Repositories.GetContents(ctx, c.owner, c.repo, path, opts)
	if err != nil {
		return nil, fmt.Errorf("get file %s: %w", path, err)
	}
	if fc == nil {
		return nil, fmt.Errorf("file %s not found", path)
	}
	content, err := fc.GetContent()
	if err != nil {
		return nil, fmt.Errorf("decode file %s: %w", path, err)
	}
	return []byte(content), nil
}
