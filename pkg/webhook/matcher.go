package webhook

import (
	"path/filepath"
	"sort"
	"strings"
)

// GitHubPayload represents relevant fields from a GitHub webhook payload
type GitHubPayload struct {
	Action     string            `json:"action,omitempty"`
	Ref        string            `json:"ref,omitempty"`
	Repository *GitHubRepository `json:"repository,omitempty"`
	Sender     *GitHubUser       `json:"sender,omitempty"`

	// Pull request specific
	PullRequest *GitHubPullRequest `json:"pull_request,omitempty"`

	// Issue specific
	Issue *GitHubIssue `json:"issue,omitempty"`

	// Push specific
	Commits    []GitHubCommit `json:"commits,omitempty"`
	HeadCommit *GitHubCommit  `json:"head_commit,omitempty"`

	// Raw payload for template rendering
	Raw map[string]interface{} `json:"-"`
}

// GitHubRepository represents a GitHub repository
type GitHubRepository struct {
	FullName      string      `json:"full_name"`
	Name          string      `json:"name"`
	Owner         *GitHubUser `json:"owner,omitempty"`
	DefaultBranch string      `json:"default_branch,omitempty"`
	HTMLURL       string      `json:"html_url,omitempty"`
	CloneURL      string      `json:"clone_url,omitempty"`
}

// GitHubUser represents a GitHub user
type GitHubUser struct {
	Login     string `json:"login"`
	ID        int64  `json:"id"`
	AvatarURL string `json:"avatar_url,omitempty"`
	HTMLURL   string `json:"html_url,omitempty"`
}

// GitHubPullRequest represents a GitHub pull request
type GitHubPullRequest struct {
	Number   int           `json:"number"`
	Title    string        `json:"title"`
	Body     string        `json:"body,omitempty"`
	State    string        `json:"state"`
	Draft    bool          `json:"draft"`
	HTMLURL  string        `json:"html_url"`
	User     *GitHubUser   `json:"user,omitempty"`
	Head     *GitHubRef    `json:"head,omitempty"`
	Base     *GitHubRef    `json:"base,omitempty"`
	Labels   []GitHubLabel `json:"labels,omitempty"`
	Merged   bool          `json:"merged"`
	MergedAt string        `json:"merged_at,omitempty"`
}

// GitHubRef represents a git reference
type GitHubRef struct {
	Ref  string            `json:"ref"`
	SHA  string            `json:"sha"`
	Repo *GitHubRepository `json:"repo,omitempty"`
}

// GitHubIssue represents a GitHub issue
type GitHubIssue struct {
	Number  int           `json:"number"`
	Title   string        `json:"title"`
	Body    string        `json:"body,omitempty"`
	State   string        `json:"state"`
	HTMLURL string        `json:"html_url"`
	User    *GitHubUser   `json:"user,omitempty"`
	Labels  []GitHubLabel `json:"labels,omitempty"`
}

// GitHubLabel represents a GitHub label
type GitHubLabel struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// GitHubCommit represents a GitHub commit
type GitHubCommit struct {
	ID       string              `json:"id"`
	Message  string              `json:"message"`
	Author   *GitHubCommitAuthor `json:"author,omitempty"`
	Added    []string            `json:"added,omitempty"`
	Removed  []string            `json:"removed,omitempty"`
	Modified []string            `json:"modified,omitempty"`
}

// GitHubCommitAuthor represents a commit author
type GitHubCommitAuthor struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Username string `json:"username,omitempty"`
}

// MatchResult represents the result of matching a trigger
type MatchResult struct {
	Matched bool
	Trigger *Trigger
}

// MatchTriggers evaluates all triggers against a GitHub payload and returns the first matching trigger
// Triggers are evaluated in priority order (lower priority number = higher priority)
func MatchTriggers(triggers []Trigger, event string, payload *GitHubPayload) *MatchResult {
	// Sort triggers by priority
	sortedTriggers := make([]Trigger, len(triggers))
	copy(sortedTriggers, triggers)
	sort.Slice(sortedTriggers, func(i, j int) bool {
		return sortedTriggers[i].Priority < sortedTriggers[j].Priority
	})

	for _, trigger := range sortedTriggers {
		if !trigger.Enabled {
			continue
		}

		if matchTrigger(&trigger, event, payload) {
			return &MatchResult{
				Matched: true,
				Trigger: &trigger,
			}
		}
	}

	return &MatchResult{Matched: false}
}

// matchTrigger checks if a single trigger matches the payload
func matchTrigger(trigger *Trigger, event string, payload *GitHubPayload) bool {
	if trigger.Conditions.GitHub == nil {
		return false
	}

	cond := trigger.Conditions.GitHub

	// Check event type
	if len(cond.Events) > 0 {
		if !containsString(cond.Events, event) {
			return false
		}
	}

	// Check action
	if len(cond.Actions) > 0 {
		if payload.Action == "" || !containsString(cond.Actions, payload.Action) {
			return false
		}
	}

	// Check repository
	if len(cond.Repositories) > 0 {
		if payload.Repository == nil {
			return false
		}
		matched := false
		for _, pattern := range cond.Repositories {
			if matchRepository(pattern, payload.Repository.FullName) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check branches
	if len(cond.Branches) > 0 {
		branch := extractBranch(event, payload)
		if branch == "" {
			return false
		}
		if !matchPatterns(cond.Branches, branch) {
			return false
		}
	}

	// Check base branches (for PR)
	if len(cond.BaseBranches) > 0 {
		if payload.PullRequest == nil || payload.PullRequest.Base == nil {
			return false
		}
		if !matchPatterns(cond.BaseBranches, payload.PullRequest.Base.Ref) {
			return false
		}
	}

	// Check draft status
	if cond.Draft != nil {
		if payload.PullRequest == nil {
			return false
		}
		if payload.PullRequest.Draft != *cond.Draft {
			return false
		}
	}

	// Check labels
	if len(cond.Labels) > 0 {
		labels := extractLabels(payload)
		if len(labels) == 0 {
			return false
		}
		// Check if any required label is present
		hasLabel := false
		for _, requiredLabel := range cond.Labels {
			if containsString(labels, requiredLabel) {
				hasLabel = true
				break
			}
		}
		if !hasLabel {
			return false
		}
	}

	// Check sender
	if len(cond.Sender) > 0 {
		if payload.Sender == nil {
			return false
		}
		if !containsString(cond.Sender, payload.Sender.Login) {
			return false
		}
	}

	// Check paths (for push events)
	if len(cond.Paths) > 0 {
		changedFiles := extractChangedFiles(payload)
		if len(changedFiles) == 0 {
			return false
		}
		// Check if any changed file matches any path pattern
		hasMatch := false
		for _, file := range changedFiles {
			for _, pattern := range cond.Paths {
				if matchPath(pattern, file) {
					hasMatch = true
					break
				}
			}
			if hasMatch {
				break
			}
		}
		if !hasMatch {
			return false
		}
	}

	return true
}

// extractBranch extracts the branch name from the payload based on event type
func extractBranch(event string, payload *GitHubPayload) string {
	switch event {
	case "push":
		// ref is like "refs/heads/main"
		if strings.HasPrefix(payload.Ref, "refs/heads/") {
			return strings.TrimPrefix(payload.Ref, "refs/heads/")
		}
		return payload.Ref
	case "pull_request":
		if payload.PullRequest != nil && payload.PullRequest.Head != nil {
			return payload.PullRequest.Head.Ref
		}
	case "create", "delete":
		// ref is the branch/tag name directly
		return payload.Ref
	}
	return ""
}

// extractLabels extracts labels from PR or issue
func extractLabels(payload *GitHubPayload) []string {
	var labels []string
	if payload.PullRequest != nil {
		for _, label := range payload.PullRequest.Labels {
			labels = append(labels, label.Name)
		}
	}
	if payload.Issue != nil {
		for _, label := range payload.Issue.Labels {
			labels = append(labels, label.Name)
		}
	}
	return labels
}

// extractChangedFiles extracts the list of changed files from push commits
func extractChangedFiles(payload *GitHubPayload) []string {
	fileSet := make(map[string]bool)
	for _, commit := range payload.Commits {
		for _, f := range commit.Added {
			fileSet[f] = true
		}
		for _, f := range commit.Modified {
			fileSet[f] = true
		}
		for _, f := range commit.Removed {
			fileSet[f] = true
		}
	}

	var files []string
	for f := range fileSet {
		files = append(files, f)
	}
	return files
}

// containsString checks if a slice contains a string
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// matchPatterns checks if value matches any of the patterns (supports glob)
func matchPatterns(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if matchGlob(pattern, value) {
			return true
		}
	}
	return false
}

// matchGlob matches a value against a glob pattern
func matchGlob(pattern, value string) bool {
	// Exact match
	if pattern == value {
		return true
	}

	// Use filepath.Match for glob matching
	matched, err := filepath.Match(pattern, value)
	if err != nil {
		return false
	}
	return matched
}

// matchPath matches a file path against a pattern (supports ** for recursive)
func matchPath(pattern, path string) bool {
	// Handle ** for recursive matching
	if strings.Contains(pattern, "**") {
		// Convert ** to a more flexible pattern
		// e.g., "src/**/*.go" should match "src/pkg/main.go"
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			prefix := strings.TrimSuffix(parts[0], "/")
			suffix := strings.TrimPrefix(parts[1], "/")

			// Check prefix
			if prefix != "" && !strings.HasPrefix(path, prefix) {
				return false
			}

			// Check suffix pattern
			if suffix != "" {
				remaining := path
				if prefix != "" {
					remaining = strings.TrimPrefix(path, prefix)
					remaining = strings.TrimPrefix(remaining, "/")
				}
				// Match suffix against the filename or any directory/filename in remaining path
				pathParts := strings.Split(remaining, "/")
				for i := range pathParts {
					testPath := strings.Join(pathParts[i:], "/")
					if matched, _ := filepath.Match(suffix, testPath); matched {
						return true
					}
					// Also try matching just the filename
					if matched, _ := filepath.Match(suffix, pathParts[len(pathParts)-1]); matched {
						return true
					}
				}
				return false
			}
			return true
		}
	}

	// Regular glob matching
	matched, err := filepath.Match(pattern, path)
	if err != nil {
		return false
	}
	return matched
}
