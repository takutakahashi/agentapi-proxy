package controllers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// GoogleOAuthController exposes non-secret scia Google OAuth integration status.
type GoogleOAuthController struct {
	scia       config.SciaConfig
	client     kubernetes.Interface
	namespace  string
	httpClient *http.Client
}

// NewGoogleOAuthController creates a GoogleOAuthController.
func NewGoogleOAuthController(scia config.SciaConfig, client kubernetes.Interface, namespace string) *GoogleOAuthController {
	return &GoogleOAuthController{
		scia:      scia,
		client:    client,
		namespace: namespace,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

// GetName returns the controller name.
func (c *GoogleOAuthController) GetName() string {
	return "GoogleOAuthController"
}

// GoogleOAuthStatusResponse is returned by GET /integrations/google-oauth/status.
type GoogleOAuthStatusResponse struct {
	Enabled          bool   `json:"enabled"`
	HealthOK         bool   `json:"health_ok"`
	HealthStatus     string `json:"health_status,omitempty"`
	ClientConfigured bool   `json:"client_configured"`
	Connected        bool   `json:"connected"`
	Credential       string `json:"credential,omitempty"`
	UserNamespace    string `json:"user_namespace,omitempty"`
	OAuthStartURL    string `json:"oauth_start_url,omitempty"`
	AuthorizationURL string `json:"authorization_url_endpoint,omitempty"`
	ProxyConfigured  bool   `json:"proxy_configured"`
}

// GetStatus returns the user's scia Google OAuth integration status.
func (c *GoogleOAuthController) GetStatus(ctx echo.Context) error {
	return c.getProviderStatus(ctx, "google")
}

// GetNotionStatus returns the user's scia Notion OAuth integration status.
func (c *GoogleOAuthController) GetNotionStatus(ctx echo.Context) error {
	return c.getProviderStatus(ctx, "notion")
}

func (c *GoogleOAuthController) getProviderStatus(ctx echo.Context, provider string) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	userID := string(user.ID())
	userNamespace := c.userNamespace(provider, userID)
	credential := c.credential(provider, userNamespace)

	resp := GoogleOAuthStatusResponse{
		Enabled:          c.scia.Enabled,
		ClientConfigured: c.scia.Enabled && credential != "",
		Credential:       credential,
		UserNamespace:    userNamespace,
		OAuthStartURL:    c.oauthStartURL(provider, userNamespace, credential),
		AuthorizationURL: c.authorizationURLEndpoint(provider, userNamespace),
		ProxyConfigured:  c.scia.Enabled && c.scia.ProxyURL != "",
	}

	if c.scia.Enabled {
		resp.HealthOK, resp.HealthStatus = c.checkHealth(ctx.Request().Context())
		resp.Connected = c.refreshTokenSecretExists(ctx.Request().Context(), userNamespace)
	}

	return ctx.JSON(http.StatusOK, resp)
}

func (c *GoogleOAuthController) checkHealth(ctx context.Context) (bool, string) {
	base := strings.TrimRight(c.scia.PublicBaseURL, "/")
	if base == "" {
		return false, "public_base_url_not_configured"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/_scia/healthz", nil)
	if err != nil {
		return false, "invalid_health_url"
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer func() {
		_ = res.Body.Close()
	}()

	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return true, res.Status
	}
	return false, res.Status
}

func (c *GoogleOAuthController) refreshTokenSecretExists(ctx context.Context, userNamespace string) bool {
	if c.client == nil || c.namespace == "" || userNamespace == "" {
		return false
	}
	secretName := "scia-oauth-" + sanitizeSciaSecretSuffix(userNamespace)
	_, err := c.client.CoreV1().Secrets(c.namespace).Get(ctx, secretName, metav1.GetOptions{})
	return err == nil
}

func (c *GoogleOAuthController) userNamespace(provider, userID string) string {
	if provider == "notion" && c.scia.NotionUserNamespace != "" {
		return c.scia.NotionUserNamespace
	}
	if c.scia.UserNamespace != "" {
		if provider == "notion" {
			return c.scia.UserNamespace + "-notion"
		}
		return c.scia.UserNamespace
	}
	if provider == "notion" {
		return userID + "-notion"
	}
	return userID
}

func (c *GoogleOAuthController) credential(provider, userNamespace string) string {
	if provider == "notion" && c.scia.NotionCredential != "" {
		return c.scia.NotionCredential
	}
	if provider == "google" && c.scia.Credential != "" {
		return c.scia.Credential
	}
	if userNamespace == "" {
		return ""
	}
	return userNamespace + "." + provider
}

func (c *GoogleOAuthController) oauthStartURL(provider, userNamespace, credential string) string {
	if credential == "" || userNamespace == "" {
		return ""
	}
	values := url.Values{}
	values.Set("credential", credential)
	values.Set("user", userNamespace)
	return c.withPublicBase("/oauth/"+provider+"/start") + "?" + values.Encode()
}

func (c *GoogleOAuthController) authorizationURLEndpoint(provider, userNamespace string) string {
	if userNamespace == "" {
		return ""
	}
	return fmt.Sprintf("%s/oauth/%s/%s/authorization-url", c.withPublicBase(""), url.PathEscape(userNamespace), provider)
}

func (c *GoogleOAuthController) withPublicBase(path string) string {
	base := strings.TrimRight(c.scia.PublicBaseURL, "/")
	if base == "" {
		return path
	}
	return base + path
}

var sciaSecretInvalidChars = regexp.MustCompile(`[^a-z0-9.-]+`)

func sanitizeSciaSecretSuffix(value string) string {
	value = strings.ToLower(value)
	value = sciaSecretInvalidChars.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-.")
	if value == "" {
		return "default"
	}
	return value
}
