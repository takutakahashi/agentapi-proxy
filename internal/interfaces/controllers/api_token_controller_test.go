package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	apitokenuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/api_token"
)

// mockAPITokenRepo is an in-memory APITokenRepository for controller tests.
type mockAPITokenRepo struct {
	byID map[string]*entities.APIToken
}

func newMockAPITokenRepo() *mockAPITokenRepo {
	return &mockAPITokenRepo{byID: map[string]*entities.APIToken{}}
}

func (r *mockAPITokenRepo) Create(_ context.Context, t *entities.APIToken) error {
	if _, ok := r.byID[t.ID()]; ok {
		return entities.ErrAPITokenAlreadyExists
	}
	r.byID[t.ID()] = t
	return nil
}
func (r *mockAPITokenRepo) GetByID(_ context.Context, id string) (*entities.APIToken, error) {
	t, ok := r.byID[id]
	if !ok {
		return nil, entities.ErrAPITokenNotFound
	}
	return t, nil
}
func (r *mockAPITokenRepo) GetBySecret(_ context.Context, s string) (*entities.APIToken, error) {
	for _, t := range r.byID {
		if t.Secret() == s {
			return t, nil
		}
	}
	return nil, entities.ErrAPITokenNotFound
}
func (r *mockAPITokenRepo) ListByOwner(_ context.Context, u entities.UserID) ([]*entities.APIToken, error) {
	var out []*entities.APIToken
	for _, t := range r.byID {
		if t.Scope() == entities.APITokenScopeUser && t.UserID() == u {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *mockAPITokenRepo) ListByTeam(_ context.Context, teamID string) ([]*entities.APIToken, error) {
	var out []*entities.APIToken
	for _, t := range r.byID {
		if t.Scope() == entities.APITokenScopeTeam && t.TeamID() == teamID {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *mockAPITokenRepo) ListAll(_ context.Context) ([]*entities.APIToken, error) {
	out := make([]*entities.APIToken, 0, len(r.byID))
	for _, t := range r.byID {
		out = append(out, t)
	}
	return out, nil
}
func (r *mockAPITokenRepo) Delete(_ context.Context, id string) error {
	delete(r.byID, id)
	return nil
}

// mockTokenAuth records loads/revokes.
type mockTokenAuth struct {
	loaded  []*entities.APIToken
	revoked []string
}

func (m *mockTokenAuth) LoadAPIToken(_ context.Context, t *entities.APIToken) error {
	m.loaded = append(m.loaded, t)
	return nil
}
func (m *mockTokenAuth) RevokeAPIToken(secret string) { m.revoked = append(m.revoked, secret) }

func newAPITokenControllerForTest(repo *mockAPITokenRepo, authSvc *mockTokenAuth) *APITokenController {
	createUC := apitokenuc.NewCreateAPITokenUseCase(repo, authSvc)
	listUC := apitokenuc.NewListAPITokenUseCase(repo)
	getUC := apitokenuc.NewGetAPITokenUseCase(repo)
	deleteUC := apitokenuc.NewDeleteAPITokenUseCase(repo, authSvc)
	return NewAPITokenController(createUC, listUC, getUC, deleteUC)
}

func makeAPITokenEchoContext(t *testing.T, method, path string, body interface{}, user *entities.User) (echo.Context, *httptest.ResponseRecorder) {
	return makeAPITokenEchoContextWithParam(t, method, path, "", body, user)
}

func makeAPITokenEchoContextWithParam(t *testing.T, method, path, tokenID string, body interface{}, user *entities.User) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	var reqBody *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader([]byte("{}"))
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if user != nil {
		c.Set("internal_user", user)
	}
	if tokenID != "" {
		c.SetParamNames("tokenId")
		c.SetParamValues(tokenID)
	}
	return c, rec
}

func setupAPITokenController() (*APITokenController, *mockAPITokenRepo, *mockTokenAuth) {
	repo := newMockAPITokenRepo()
	authSvc := &mockTokenAuth{}
	return newAPITokenControllerForTest(repo, authSvc), repo, authSvc
}

// requireMetadataShape asserts the raw JSON for a metadata-only payload (list
// item or GET response) has token_prefix and lacks secret/plaintext_token.
func requireMetadataShape(t *testing.T, raw map[string]json.RawMessage) {
	t.Helper()
	assert.Contains(t, raw, "token_prefix", "metadata must expose token_prefix")
	_, hasDisplay := raw["display_prefix"]
	assert.False(t, hasDisplay, "metadata must NOT expose display_prefix")
	_, hasSecret := raw["secret"]
	assert.False(t, hasSecret, "metadata must NOT expose secret")
	_, hasPlain := raw["plaintext_token"]
	assert.False(t, hasPlain, "metadata must NOT expose plaintext_token")
}

// --- Create ---

func TestAPITokenController_Create_Personal_201_NoStore(t *testing.T) {
	c, rec := makeAPITokenEchoContext(t, http.MethodPost, "/api-tokens",
		CreateAPITokenRequest{
			Name:        "laptop",
			Scope:       "personal",
			Permissions: []string{"session:read"},
		}, newTestAPIKeyUser("user-1"))
	ctrl, repo, authSvc := setupAPITokenController()

	require.NoError(t, ctrl.Create(c))
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	var resp CreateAPITokenResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "personal", resp.Token.Scope, "public API scope must be 'personal'")
	assert.Equal(t, "user-1", resp.Token.UserID)
	assert.Equal(t, "laptop", resp.Token.Name)
	assert.NotEmpty(t, resp.PlaintextToken)
	assert.Contains(t, resp.Token.TokenPrefix, "apt_")

	// The create response must be the exact nested contract: {token, plaintext_token}.
	var createRaw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &createRaw))
	assert.Contains(t, createRaw, "token")
	assert.Contains(t, createRaw, "plaintext_token")
	for _, banned := range []string{"secret", "display_prefix"} {
		_, has := createRaw[banned]
		assert.False(t, has, "create response must not contain %q", banned)
	}
	// The nested token object must itself be metadata-only (no secret leakage).
	var nestedToken map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(createRaw["token"], &nestedToken))
	requireMetadataShape(t, nestedToken)
	var nestedScope string
	require.NoError(t, json.Unmarshal(nestedToken["scope"], &nestedScope))
	assert.Equal(t, "personal", nestedScope)

	// Secret must NOT appear in metadata responses later.
	all, _ := repo.ListAll(context.Background())
	assert.Equal(t, resp.PlaintextToken, all[0].Secret()) // entity still holds secret
	assert.Len(t, authSvc.loaded, 1)
}

func TestAPITokenController_Create_ScopeDefaultIsPersonal(t *testing.T) {
	c, rec := makeAPITokenEchoContext(t, http.MethodPost, "/api-tokens",
		CreateAPITokenRequest{
			Name:        "default-scope",
			Permissions: []string{"session:read"},
		}, newTestAPIKeyUser("user-1"))
	ctrl, _, _ := setupAPITokenController()
	require.NoError(t, ctrl.Create(c))
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAPITokenResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "personal", resp.Token.Scope)
}

func TestAPITokenController_Create_RejectsInternalUserScope(t *testing.T) {
	// The internal "user" wording must NOT be accepted on the public API.
	c, _ := makeAPITokenEchoContext(t, http.MethodPost, "/api-tokens",
		CreateAPITokenRequest{Name: "x", Scope: "user", Permissions: []string{"session:read"}},
		newTestAPIKeyUser("user-1"))
	ctrl, _, _ := setupAPITokenController()
	assertHTTPError(t, ctrl.Create(c), http.StatusBadRequest)
}

func TestAPITokenController_Create_NameRequired(t *testing.T) {
	c, _ := makeAPITokenEchoContext(t, http.MethodPost, "/api-tokens",
		CreateAPITokenRequest{Name: "", Scope: "personal", Permissions: []string{"session:read"}},
		newTestAPIKeyUser("user-1"))
	ctrl, _, _ := setupAPITokenController()
	assertHTTPError(t, ctrl.Create(c), http.StatusBadRequest)
}

func TestAPITokenController_Create_NameTrimmedAndLengthBoundaries(t *testing.T) {
	ctrl, _, _ := setupAPITokenController()

	// 1 character (after trimming surrounding spaces) is valid.
	c, rec := makeAPITokenEchoContext(t, http.MethodPost, "/api-tokens",
		CreateAPITokenRequest{Name: "   a   ", Scope: "personal", Permissions: []string{"session:read"}},
		newTestAPIKeyUser("user-1"))
	require.NoError(t, ctrl.Create(c))
	assert.Equal(t, http.StatusCreated, rec.Code)
	var resp CreateAPITokenResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "a", resp.Token.Name, "name must be trimmed")

	// exactly 64 runes is valid.
	name64 := strings.Repeat("a", 64)
	c2, rec2 := makeAPITokenEchoContext(t, http.MethodPost, "/api-tokens",
		CreateAPITokenRequest{Name: name64, Scope: "personal", Permissions: []string{"session:read"}},
		newTestAPIKeyUser("user-2"))
	require.NoError(t, ctrl.Create(c2))
	assert.Equal(t, http.StatusCreated, rec2.Code)

	// 65 runes is rejected with 400.
	name65 := strings.Repeat("a", 65)
	c3, _ := makeAPITokenEchoContext(t, http.MethodPost, "/api-tokens",
		CreateAPITokenRequest{Name: name65, Scope: "personal", Permissions: []string{"session:read"}},
		newTestAPIKeyUser("user-3"))
	assertHTTPError(t, ctrl.Create(c3), http.StatusBadRequest)

	// only-whitespace name is rejected (empty after trim) with 400.
	c4, _ := makeAPITokenEchoContext(t, http.MethodPost, "/api-tokens",
		CreateAPITokenRequest{Name: "    ", Scope: "personal", Permissions: []string{"session:read"}},
		newTestAPIKeyUser("user-4"))
	assertHTTPError(t, ctrl.Create(c4), http.StatusBadRequest)
}

func TestAPITokenController_Create_DefaultPermissionsBoundedByCaller(t *testing.T) {
	// A read-only caller (only session:read) must NOT silently receive
	// session:delete when permissions are omitted.
	readOnly := entities.NewUser(entities.UserID("ro-1"), entities.UserTypeRegular, "ro")
	readOnly.SetPermissions([]entities.Permission{entities.PermissionSessionRead})

	c, rec := makeAPITokenEchoContext(t, http.MethodPost, "/api-tokens",
		CreateAPITokenRequest{Name: "ro", Scope: "personal"}, readOnly)
	ctrl, _, _ := setupAPITokenController()
	require.NoError(t, ctrl.Create(c))
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAPITokenResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, []string{"session:read"}, resp.Token.Permissions,
		"default permissions must be exactly the caller's session permissions")
}

func TestAPITokenController_Create_DefaultPermissionsEmptyForCallerWithoutSessionPerms(t *testing.T) {
	// A caller with no session permissions at all: default derivation yields
	// an empty set, which the use case rejects with 400 (no silent over-grant).
	none := entities.NewUser(entities.UserID("none-1"), entities.UserTypeRegular, "none")
	none.SetPermissions([]entities.Permission{}) // no session perms

	c, _ := makeAPITokenEchoContext(t, http.MethodPost, "/api-tokens",
		CreateAPITokenRequest{Name: "x", Scope: "personal"}, none)
	ctrl, _, _ := setupAPITokenController()
	assertHTTPError(t, ctrl.Create(c), http.StatusBadRequest)
}

func TestAPITokenController_Create_MissingAuth(t *testing.T) {
	c, _ := makeAPITokenEchoContext(t, http.MethodPost, "/api-tokens",
		CreateAPITokenRequest{Scope: "personal", Permissions: []string{"session:read"}}, nil)
	ctrl, _, _ := setupAPITokenController()
	assertHTTPError(t, ctrl.Create(c), http.StatusUnauthorized)
}

func TestAPITokenController_Create_PermissionExceedsCaller(t *testing.T) {
	user := newTestAPIKeyUser("u1") // has session:create, session:read only
	c, _ := makeAPITokenEchoContext(t, http.MethodPost, "/api-tokens",
		CreateAPITokenRequest{Name: "x", Scope: "personal", Permissions: []string{"session:delete"}}, user)
	ctrl, _, _ := setupAPITokenController()
	assertHTTPError(t, ctrl.Create(c), http.StatusForbidden)
}

func TestAPITokenController_Create_TeamNonMember(t *testing.T) {
	user := newTestGitHubUser("u1", "org", "other") // member of org/other only
	c, _ := makeAPITokenEchoContext(t, http.MethodPost, "/api-tokens",
		CreateAPITokenRequest{Name: "x", Scope: "team", TeamID: "org/team", Permissions: []string{"session:create"}}, user)
	ctrl, _, _ := setupAPITokenController()
	assertHTTPError(t, ctrl.Create(c), http.StatusForbidden)
}

// --- List ---

func TestAPITokenController_List_ItemsShape(t *testing.T) {
	ctrl, repo, _ := setupAPITokenController()
	// seed two personal tokens for user-1 and one for user-2
	seed := func(uid string) {
		_ = repo.Create(context.Background(), entities.NewAPIToken(
			"tok_"+uid, "secret_"+uid, "secret_", "n",
			entities.APITokenScopeUser, entities.UserID(uid), "",
			[]entities.Permission{entities.PermissionSessionRead}, nil, entities.UserID(uid)))
	}
	seed("user-1")
	seed("user-1b")
	seed("user-2")

	c, rec := makeAPITokenEchoContext(t, http.MethodGet, "/api-tokens?scope=personal", nil, newTestAPIKeyUser("user-1"))
	require.NoError(t, ctrl.List(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp APITokenListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	// user-1 owns tok_user-1 only (the other seeded for "user-1b" belongs to a different owner)
	assert.Len(t, resp.Items, 1)
	assert.Equal(t, "user-1", resp.Items[0].UserID)
	assert.Equal(t, "personal", resp.Items[0].Scope, "list item scope must be 'personal'")

	// list response must not contain secret/plaintext_token and must use token_prefix
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	var items []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["items"], &items))
	for _, item := range items {
		requireMetadataShape(t, item)
		assert.NotEmpty(t, item["token_prefix"])
	}
}

func TestAPITokenController_List_RejectsInternalUserScope(t *testing.T) {
	c, _ := makeAPITokenEchoContext(t, http.MethodGet, "/api-tokens?scope=user", nil, newTestAPIKeyUser("user-1"))
	ctrl, _, _ := setupAPITokenController()
	assertHTTPError(t, ctrl.List(c), http.StatusBadRequest)
}

// --- Get ---

func TestAPITokenController_Get_NoSecret(t *testing.T) {
	ctrl, repo, _ := setupAPITokenController()
	_ = repo.Create(context.Background(), entities.NewAPIToken(
		"tok_get", "secret_get", "secret_g", "n",
		entities.APITokenScopeUser, entities.UserID("user-1"), "",
		[]entities.Permission{entities.PermissionSessionRead}, nil, entities.UserID("user-1")))

	c, rec := makeAPITokenEchoContextWithParam(t, http.MethodGet, "/api-tokens/tok_get", "tok_get", nil, newTestAPIKeyUser("user-1"))
	require.NoError(t, ctrl.Get(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp APITokenMetadata
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "tok_get", resp.ID)
	assert.Equal(t, "personal", resp.Scope, "get scope must be 'personal'")
	// secret field must be absent on metadata-only responses
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	requireMetadataShape(t, raw)
}

func TestAPITokenController_Get_OtherUserNotFound(t *testing.T) {
	ctrl, repo, _ := setupAPITokenController()
	_ = repo.Create(context.Background(), entities.NewAPIToken(
		"tok_owner", "secret_o", "secret_o", "n",
		entities.APITokenScopeUser, entities.UserID("owner"), "",
		[]entities.Permission{entities.PermissionSessionRead}, nil, entities.UserID("owner")))

	c, _ := makeAPITokenEchoContextWithParam(t, http.MethodGet, "/api-tokens/tok_owner", "tok_owner", nil, newTestAPIKeyUser("intruder"))
	assertHTTPError(t, ctrl.Get(c), http.StatusNotFound) // 404, no cross-scope leak
}

func TestAPITokenController_Get_MissingNotFound(t *testing.T) {
	ctrl, _, _ := setupAPITokenController()
	c, _ := makeAPITokenEchoContextWithParam(t, http.MethodGet, "/api-tokens/ghost", "ghost", nil, newTestAPIKeyUser("user-1"))
	assertHTTPError(t, ctrl.Get(c), http.StatusNotFound)
}

// --- Delete ---

func TestAPITokenController_Delete_Owner_204(t *testing.T) {
	ctrl, repo, authSvc := setupAPITokenController()
	_ = repo.Create(context.Background(), entities.NewAPIToken(
		"tok_del", "secret_del", "secret_d", "n",
		entities.APITokenScopeUser, entities.UserID("user-1"), "",
		[]entities.Permission{entities.PermissionSessionRead}, nil, entities.UserID("user-1")))

	c, rec := makeAPITokenEchoContextWithParam(t, http.MethodDelete, "/api-tokens/tok_del", "tok_del", nil, newTestAPIKeyUser("user-1"))
	require.NoError(t, ctrl.Delete(c))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Len(t, authSvc.revoked, 1, "immediate local revocation expected")
	_, err := repo.GetByID(context.Background(), "tok_del")
	assert.True(t, errors.Is(err, entities.ErrAPITokenNotFound))
}

func TestAPITokenController_Delete_IdempotentNonexistent_204(t *testing.T) {
	ctrl, _, authSvc := setupAPITokenController()
	c, rec := makeAPITokenEchoContextWithParam(t, http.MethodDelete, "/api-tokens/ghost", "ghost", nil, newTestAPIKeyUser("user-1"))
	require.NoError(t, ctrl.Delete(c))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, authSvc.revoked)
}

func TestAPITokenController_Delete_NonOwner_NoLeak_204(t *testing.T) {
	ctrl, repo, authSvc := setupAPITokenController()
	_ = repo.Create(context.Background(), entities.NewAPIToken(
		"tok_other", "secret_o", "secret_o", "n",
		entities.APITokenScopeUser, entities.UserID("owner"), "",
		[]entities.Permission{entities.PermissionSessionRead}, nil, entities.UserID("owner")))

	c, rec := makeAPITokenEchoContextWithParam(t, http.MethodDelete, "/api-tokens/tok_other", "tok_other", nil, newTestAPIKeyUser("intruder"))
	require.NoError(t, ctrl.Delete(c))
	assert.Equal(t, http.StatusNoContent, rec.Code) // no leak, no delete
	assert.Empty(t, authSvc.revoked)
	if _, err := repo.GetByID(context.Background(), "tok_other"); err != nil {
		t.Errorf("non-owner delete removed the token: %v", err)
	}
}

func TestAPITokenController_Delete_TeamCreatorOrAdmin(t *testing.T) {
	ctrl, repo, authSvc := setupAPITokenController()
	creator := newTestGitHubUser("creator", "org", "team")
	out, err := apitokenuc.NewCreateAPITokenUseCase(repo, authSvc).Execute(context.Background(), &apitokenuc.CreateAPITokenInput{
		Caller:      creator,
		Scope:       "team",
		TeamID:      "org/team",
		Name:        "team",
		Permissions: []entities.Permission{entities.PermissionSessionCreate},
	})
	require.NoError(t, err)

	// admin can delete
	c, rec := makeAPITokenEchoContextWithParam(t, http.MethodDelete, "/api-tokens/"+out.Token.ID(), out.Token.ID(), nil, newTestAdminUser("admin"))
	require.NoError(t, ctrl.Delete(c))
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// --- Backward compatibility ---

func TestAPITokenController_PersonalAPIKeyEndpointStillWorks(t *testing.T) {
	// The legacy /users/me/api-key controller must continue to exist and
	// behave unchanged. It is wired separately in the router; here we just
	// assert the controller type is still instantiable to guard against
	// accidental removal.
	_ = NewPersonalAPIKeyController(nil, nil)
}
