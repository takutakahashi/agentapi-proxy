package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/apitoken"
)

// fakePersonalRepo implements repositories.PersonalAPIKeyRepository in memory.
type fakePersonalRepo struct {
	keys map[entities.UserID]*entities.PersonalAPIKey
}

func newFakePersonalRepo() *fakePersonalRepo {
	return &fakePersonalRepo{keys: map[entities.UserID]*entities.PersonalAPIKey{}}
}

func (r *fakePersonalRepo) FindByUserID(_ context.Context, id entities.UserID) (*entities.PersonalAPIKey, error) {
	if k, ok := r.keys[id]; ok {
		return k, nil
	}
	return nil, &notFoundError{}
}
func (r *fakePersonalRepo) Save(_ context.Context, k *entities.PersonalAPIKey) error {
	r.keys[k.UserID()] = k
	return nil
}
func (r *fakePersonalRepo) Delete(_ context.Context, id entities.UserID) error {
	delete(r.keys, id)
	return nil
}
func (r *fakePersonalRepo) List(_ context.Context) ([]*entities.PersonalAPIKey, error) {
	out := make([]*entities.PersonalAPIKey, 0, len(r.keys))
	for _, k := range r.keys {
		out = append(out, k)
	}
	return out, nil
}

// fakeTeamConfigRepo implements repositories.TeamConfigRepository in memory.
type fakeTeamConfigRepo struct {
	configs map[string]*entities.TeamConfig
}

func newFakeTeamConfigRepo() *fakeTeamConfigRepo {
	return &fakeTeamConfigRepo{configs: map[string]*entities.TeamConfig{}}
}

func (r *fakeTeamConfigRepo) Save(_ context.Context, c *entities.TeamConfig) error {
	r.configs[c.TeamID()] = c
	return nil
}
func (r *fakeTeamConfigRepo) FindByTeamID(_ context.Context, teamID string) (*entities.TeamConfig, error) {
	if c, ok := r.configs[teamID]; ok {
		return c, nil
	}
	return nil, &notFoundError{}
}
func (r *fakeTeamConfigRepo) Delete(_ context.Context, teamID string) error {
	delete(r.configs, teamID)
	return nil
}
func (r *fakeTeamConfigRepo) Exists(_ context.Context, teamID string) (bool, error) {
	_, ok := r.configs[teamID]
	return ok, nil
}
func (r *fakeTeamConfigRepo) List(_ context.Context) ([]*entities.TeamConfig, error) {
	out := make([]*entities.TeamConfig, 0, len(r.configs))
	for _, c := range r.configs {
		out = append(out, c)
	}
	return out, nil
}

// tokenRepoForMigrationTest reuses the in-memory fake from the usecase test
// package? No—different package. Define a minimal one here.
type memTokenRepo struct {
	byID map[string]*entities.APIToken
}

func newMemTokenRepo() *memTokenRepo {
	return &memTokenRepo{byID: map[string]*entities.APIToken{}}
}

func (r *memTokenRepo) Create(_ context.Context, t *entities.APIToken) error {
	if _, ok := r.byID[t.ID()]; ok {
		return entities.ErrAPITokenAlreadyExists
	}
	r.byID[t.ID()] = t
	return nil
}
func (r *memTokenRepo) GetByID(_ context.Context, id string) (*entities.APIToken, error) {
	t, ok := r.byID[id]
	if !ok {
		return nil, entities.ErrAPITokenNotFound
	}
	return t, nil
}
func (r *memTokenRepo) GetBySecret(_ context.Context, s string) (*entities.APIToken, error) {
	for _, t := range r.byID {
		if t.Secret() == s {
			return t, nil
		}
	}
	return nil, entities.ErrAPITokenNotFound
}
func (r *memTokenRepo) ListByOwner(_ context.Context, u entities.UserID) ([]*entities.APIToken, error) {
	var out []*entities.APIToken
	for _, t := range r.byID {
		if t.Scope() == entities.APITokenScopeUser && t.UserID() == u {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *memTokenRepo) ListByTeam(_ context.Context, teamID string) ([]*entities.APIToken, error) {
	var out []*entities.APIToken
	for _, t := range r.byID {
		if t.Scope() == entities.APITokenScopeTeam && t.TeamID() == teamID {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *memTokenRepo) ListAll(_ context.Context) ([]*entities.APIToken, error) {
	out := make([]*entities.APIToken, 0, len(r.byID))
	for _, t := range r.byID {
		out = append(out, t)
	}
	return out, nil
}
func (r *memTokenRepo) Delete(_ context.Context, id string) error {
	delete(r.byID, id)
	return nil
}

// noopAnnotator records calls.
type noopAnnotator struct{ calls []string }

func (a *noopAnnotator) ApplyMigrationAnnotations(_ context.Context, tokenID, source, sourceID string) error {
	a.calls = append(a.calls, source+":"+sourceID)
	return nil
}

func TestMigrateAPITokens_PersonalKeys(t *testing.T) {
	auth := NewSimpleAuthService()
	tokenRepo := newMemTokenRepo()
	personalRepo := newFakePersonalRepo()

	_ = personalRepo.Save(context.Background(), entities.NewPersonalAPIKey(entities.UserID("u1"), "ap_legacy_1"))
	_ = personalRepo.Save(context.Background(), entities.NewPersonalAPIKey(entities.UserID("u2"), "ap_legacy_2"))

	ann := &noopAnnotator{}
	if err := MigrateAPITokens(context.Background(), auth, tokenRepo, personalRepo, nil, ann); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	all, _ := tokenRepo.ListAll(context.Background())
	if len(all) != 2 {
		t.Fatalf("expected 2 migrated tokens, got %d", len(all))
	}
	for _, tok := range all {
		if tok.Scope() != entities.APITokenScopeUser {
			t.Errorf("scope = %q", tok.Scope())
		}
		if tok.Secret() == "" {
			t.Error("secret empty")
		}
		// deterministic id
		if tok.ID() != apitoken.MigrationTokenID("personal-"+string(tok.UserID())) {
			t.Errorf("non-deterministic id %q", tok.ID())
		}
		// authenticates via legacy plaintext
		if _, err := auth.ValidateAPIKey(context.Background(), tok.Secret()); err != nil {
			t.Errorf("legacy secret %q does not authenticate: %v", tok.Secret(), err)
		}
	}
	if len(ann.calls) != 2 {
		t.Errorf("expected 2 annotation calls, got %d", len(ann.calls))
	}

	// Legacy data must remain untouched.
	if _, err := personalRepo.FindByUserID(context.Background(), entities.UserID("u1")); err != nil {
		t.Errorf("legacy personal key was deleted: %v", err)
	}
}

func TestMigrateAPITokens_Idempotent(t *testing.T) {
	auth := NewSimpleAuthService()
	tokenRepo := newMemTokenRepo()
	personalRepo := newFakePersonalRepo()
	_ = personalRepo.Save(context.Background(), entities.NewPersonalAPIKey(entities.UserID("u1"), "ap_legacy_1"))

	ann := &noopAnnotator{}
	if err := MigrateAPITokens(context.Background(), auth, tokenRepo, personalRepo, nil, ann); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	count1, _ := tokenRepo.ListAll(context.Background())

	// Run again.
	if err := MigrateAPITokens(context.Background(), auth, tokenRepo, personalRepo, nil, ann); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	count2, _ := tokenRepo.ListAll(context.Background())
	if len(count1) != len(count2) {
		t.Errorf("migration not idempotent: %d -> %d", len(count1), len(count2))
	}
}

func TestMigrateAPITokens_TeamServiceAccounts(t *testing.T) {
	auth := NewSimpleAuthService()
	tokenRepo := newMemTokenRepo()
	teamRepo := newFakeTeamConfigRepo()
	sa := entities.NewServiceAccount("org/team", entities.UserID("sa-org-team"), "ap_team_legacy",
		[]entities.Permission{entities.PermissionSessionCreate})
	tc := entities.NewTeamConfig("org/team", sa, nil)
	_ = teamRepo.Save(context.Background(), tc)

	if err := MigrateAPITokens(context.Background(), auth, tokenRepo, nil, teamRepo, &noopAnnotator{}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	all, _ := tokenRepo.ListAll(context.Background())
	if len(all) != 1 {
		t.Fatalf("expected 1 team token, got %d", len(all))
	}
	tok := all[0]
	if tok.Scope() != entities.APITokenScopeTeam {
		t.Errorf("scope = %q", tok.Scope())
	}
	if tok.TeamID() != "org/team" {
		t.Errorf("team = %q", tok.TeamID())
	}
	if tok.Secret() != "ap_team_legacy" {
		t.Errorf("secret changed: %q", tok.Secret())
	}
	if _, err := auth.ValidateAPIKey(context.Background(), "ap_team_legacy"); err != nil {
		t.Errorf("legacy team secret does not authenticate: %v", err)
	}
}

func TestMigrateAPITokens_ConflictingSecretFailsFast(t *testing.T) {
	auth := NewSimpleAuthService()
	tokenRepo := newMemTokenRepo()
	personalRepo := newFakePersonalRepo()
	_ = personalRepo.Save(context.Background(), entities.NewPersonalAPIKey(entities.UserID("u1"), "ap_legacy_1"))

	// Pre-create a token with the SAME deterministic id but a DIFFERENT secret.
	conflictID := apitoken.MigrationTokenID("personal-u1")
	conflict := entities.NewAPIToken(conflictID, "different_secret", "different", "x",
		entities.APITokenScopeUser, entities.UserID("u1"), "",
		defaultMigrationPermissions, nil, entities.UserID("u1"))
	if err := tokenRepo.Create(context.Background(), conflict); err != nil {
		t.Fatalf("seed conflict: %v", err)
	}

	// Migration must fail fast and propagate the conflict rather than swallow
	// it: a deterministic-ID secret mismatch is an unsafe state and must
	// prevent serving traffic.
	err := MigrateAPITokens(context.Background(), auth, tokenRepo, personalRepo, nil, &noopAnnotator{})
	if err == nil {
		t.Fatal("expected migration to fail on secret conflict, got nil")
	}

	// The conflicting token must be untouched.
	got, _ := tokenRepo.GetByID(context.Background(), conflictID)
	if got.Secret() != "different_secret" {
		t.Errorf("conflicting token was overwritten: %q", got.Secret())
	}
	// Legacy data untouched.
	if _, err := personalRepo.FindByUserID(context.Background(), entities.UserID("u1")); err != nil {
		t.Errorf("legacy key removed on conflict: %v", err)
	}
}

// failingAnnotator returns an error on every annotation call to verify that
// annotation errors propagate through migration (fail-safe startup).
type failingAnnotator struct{}

func (failingAnnotator) ApplyMigrationAnnotations(context.Context, string, string, string) error {
	return errors.New("annotator unavailable")
}

func TestMigrateAPITokens_AnnotationErrorPropagates(t *testing.T) {
	auth := NewSimpleAuthService()
	tokenRepo := newMemTokenRepo()
	personalRepo := newFakePersonalRepo()
	_ = personalRepo.Save(context.Background(), entities.NewPersonalAPIKey(entities.UserID("u1"), "ap_legacy_1"))

	err := MigrateAPITokens(context.Background(), auth, tokenRepo, personalRepo, nil, failingAnnotator{})
	if err == nil {
		t.Fatal("expected migration to fail when annotation fails, got nil")
	}
	if !strings.Contains(err.Error(), "annotate") {
		t.Errorf("expected annotation error to propagate, got: %v", err)
	}
}

// failingTokenRepo wraps memTokenRepo to make Create return an arbitrary error
// for a specific id, simulating a repository outage during migration.
type failingTokenRepo struct {
	*memTokenRepo
	failNext bool
}

func (f *failingTokenRepo) Create(ctx context.Context, t *entities.APIToken) error {
	if f.failNext {
		f.failNext = false
		return errors.New("repository unavailable")
	}
	return f.memTokenRepo.Create(ctx, t)
}

func TestMigrateAPITokens_RepositoryErrorPropagates(t *testing.T) {
	auth := NewSimpleAuthService()
	repo := &failingTokenRepo{memTokenRepo: newMemTokenRepo(), failNext: true}
	personalRepo := newFakePersonalRepo()
	_ = personalRepo.Save(context.Background(), entities.NewPersonalAPIKey(entities.UserID("u1"), "ap_legacy_1"))

	err := MigrateAPITokens(context.Background(), auth, repo, personalRepo, nil, &noopAnnotator{})
	if err == nil {
		t.Fatal("expected migration to fail on repository error, got nil")
	}
}

// failingListTokenRepo wraps memTokenRepo to make ListAll return an error,
// simulating a repository outage during bootstrap.
type failingListTokenRepo struct {
	*memTokenRepo
}

func (f *failingListTokenRepo) ListAll(context.Context) ([]*entities.APIToken, error) {
	return nil, errors.New("repository unavailable")
}

func TestBootstrapAPITokens_BootstrapErrorPropagates(t *testing.T) {
	auth := NewSimpleAuthService()
	repo := &failingListTokenRepo{memTokenRepo: newMemTokenRepo()}
	err := BootstrapAPITokens(context.Background(), auth, repo)
	if err == nil {
		t.Fatal("expected bootstrap to fail on list error, got nil")
	}
}

// nilTokenListRepo returns a single nil token from ListAll to simulate a
// corrupt/unparseable stored entry; bootstrap must surface this as an error
type nilTokenListRepo struct {
	*memTokenRepo
}

func (nilTokenListRepo) ListAll(context.Context) ([]*entities.APIToken, error) {
	return []*entities.APIToken{nil}, nil
}

func TestBootstrapAPITokens_LoadErrorPropagates(t *testing.T) {
	auth := NewSimpleAuthService()
	repo := &nilTokenListRepo{memTokenRepo: newMemTokenRepo()}
	err := BootstrapAPITokens(context.Background(), auth, repo)
	if err == nil {
		t.Fatal("expected bootstrap to fail on corrupt (nil) token, got nil")
	}
}
