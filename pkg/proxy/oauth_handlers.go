package proxy

// OAuth types are now defined as aliases in proxy.go
// - OAuthSession = app.OAuthSession
// - OAuthLoginRequest = app.OAuthLoginRequest
// - OAuthLoginResponse = app.OAuthLoginResponse
// - OAuthCallbackRequest = app.OAuthCallbackRequest
// - OAuthTokenResponse = app.OAuthTokenResponse
// - OAuthSessionResponse = app.OAuthSessionResponse

// All OAuth handler methods are now implemented in internal/app/auth.go
// and are accessible through the Server (Proxy) type.
