package entities

import (
	"errors"
	"net/url"
	"strings"
)

// RepositoryURL represents a repository URL
type RepositoryURL string

// Repository represents a Git repository entity
type Repository struct {
	url         RepositoryURL
	owner       string
	name        string
	branch      string
	hostname    string
	protocol    string
	accessToken string
}

// NewRepository creates a new repository from a URL
func NewRepository(repoURL RepositoryURL) (*Repository, error) {
	repo := &Repository{
		url:    repoURL,
		branch: "main", // default branch
	}

	if err := repo.parseURL(); err != nil {
		return nil, err
	}

	return repo, nil
}

// NewRepositoryWithBranch creates a new repository with a specific branch
func NewRepositoryWithBranch(repoURL RepositoryURL, branch string) (*Repository, error) {
	repo, err := NewRepository(repoURL)
	if err != nil {
		return nil, err
	}

	repo.branch = branch
	return repo, nil
}

// URL returns the repository URL
func (r *Repository) URL() RepositoryURL {
	return r.url
}

// Owner returns the repository owner
func (r *Repository) Owner() string {
	return r.owner
}

// Name returns the repository name
func (r *Repository) Name() string {
	return r.name
}

// FullName returns the full repository name (owner/name)
func (r *Repository) FullName() string {
	if r.owner == "" || r.name == "" {
		return ""
	}
	return r.owner + "/" + r.name
}

// Branch returns the repository branch
func (r *Repository) Branch() string {
	return r.branch
}

// Hostname returns the repository hostname
func (r *Repository) Hostname() string {
	return r.hostname
}

// Protocol returns the repository protocol
func (r *Repository) Protocol() string {
	return r.protocol
}

// SetBranch sets the repository branch
func (r *Repository) SetBranch(branch string) error {
	if branch == "" {
		return errors.New("branch cannot be empty")
	}
	r.branch = branch
	return nil
}

// AccessToken returns the repository access token
func (r *Repository) AccessToken() string {
	return r.accessToken
}

// SetAccessToken sets the repository access token
func (r *Repository) SetAccessToken(token string) {
	r.accessToken = token
}

// IsGitHub returns true if the repository is hosted on GitHub
func (r *Repository) IsGitHub() bool {
	return r.hostname == "github.com" || strings.HasSuffix(r.hostname, ".github.com")
}

// IsSSH returns true if the repository uses SSH protocol
func (r *Repository) IsSSH() bool {
	return r.protocol == "ssh" || r.protocol == "git"
}

// IsHTTPS returns true if the repository uses HTTPS protocol
func (r *Repository) IsHTTPS() bool {
	return r.protocol == "https"
}

// GetHTTPSURL returns the HTTPS version of the repository URL
func (r *Repository) GetHTTPSURL() string {
	if r.owner == "" || r.name == "" || r.hostname == "" {
		return ""
	}
	return "https://" + r.hostname + "/" + r.owner + "/" + r.name + ".git"
}

// GetSSHURL returns the SSH version of the repository URL
func (r *Repository) GetSSHURL() string {
	if r.owner == "" || r.name == "" || r.hostname == "" {
		return ""
	}
	return "git@" + r.hostname + ":" + r.owner + "/" + r.name + ".git"
}

// parseURL parses the repository URL and extracts components
func (r *Repository) parseURL() error {
	urlStr := string(r.url)
	if urlStr == "" {
		return errors.New("repository URL cannot be empty")
	}

	// Handle SSH URLs: git@hostname:owner/repo.git
	if strings.Contains(urlStr, "@") && strings.Contains(urlStr, ":") && !strings.Contains(urlStr, "://") {
		return r.parseSSHURL(urlStr)
	}

	// Handle standard URLs with protocol
	if strings.Contains(urlStr, "://") {
		return r.parseStandardURL(urlStr)
	}

	return errors.New("unsupported repository URL format")
}

// parseSSHURL parses SSH format URLs: git@hostname:owner/repo.git
func (r *Repository) parseSSHURL(urlStr string) error {
	r.protocol = "ssh"

	// Split on @
	parts := strings.Split(urlStr, "@")
	if len(parts) != 2 {
		return errors.New("invalid SSH URL format")
	}

	// Split the second part on :
	hostAndPath := strings.Split(parts[1], ":")
	if len(hostAndPath) != 2 {
		return errors.New("invalid SSH URL format")
	}

	r.hostname = hostAndPath[0]
	path := hostAndPath[1]

	// Remove .git suffix if present
	path = strings.TrimSuffix(path, ".git")

	// Split path into owner/repo
	pathParts := strings.Split(path, "/")
	if len(pathParts) < 2 {
		return errors.New("invalid repository path")
	}

	r.owner = pathParts[0]
	r.name = strings.Join(pathParts[1:], "/")

	return nil
}

// parseStandardURL parses standard URLs with protocol
func (r *Repository) parseStandardURL(urlStr string) error {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return err
	}

	r.protocol = parsedURL.Scheme
	r.hostname = parsedURL.Host

	// Remove leading slash and .git suffix
	path := strings.TrimPrefix(parsedURL.Path, "/")
	path = strings.TrimSuffix(path, ".git")

	if path == "" {
		return errors.New("empty repository path")
	}

	// Split path into owner/repo
	pathParts := strings.Split(path, "/")
	if len(pathParts) < 2 {
		return errors.New("invalid repository path")
	}

	r.owner = pathParts[0]
	r.name = strings.Join(pathParts[1:], "/")

	return nil
}

// Validate ensures the repository is in a valid state
func (r *Repository) Validate() error {
	if r.url == "" {
		return errors.New("repository URL cannot be empty")
	}

	if r.owner == "" {
		return errors.New("repository owner cannot be empty")
	}

	if r.name == "" {
		return errors.New("repository name cannot be empty")
	}

	if r.hostname == "" {
		return errors.New("repository hostname cannot be empty")
	}

	if r.protocol == "" {
		return errors.New("repository protocol cannot be empty")
	}

	if r.branch == "" {
		return errors.New("repository branch cannot be empty")
	}

	// Validate protocol
	validProtocols := []string{"https", "http", "ssh", "git"}
	protocolValid := false
	for _, validProtocol := range validProtocols {
		if r.protocol == validProtocol {
			protocolValid = true
			break
		}
	}
	if !protocolValid {
		return errors.New("invalid repository protocol")
	}

	return nil
}

// Clone creates a copy of the repository
func (r *Repository) Clone() *Repository {
	return &Repository{
		url:         r.url,
		owner:       r.owner,
		name:        r.name,
		branch:      r.branch,
		hostname:    r.hostname,
		protocol:    r.protocol,
		accessToken: r.accessToken,
	}
}

// Equal returns true if the repositories are equal
func (r *Repository) Equal(other *Repository) bool {
	if other == nil {
		return false
	}

	return r.owner == other.owner &&
		r.name == other.name &&
		r.hostname == other.hostname &&
		r.branch == other.branch
}
