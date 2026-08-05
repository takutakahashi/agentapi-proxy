package provisioner

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleSandboxPolicyForwardsToNetworkFilter(t *testing.T) {
	var gotMethod, gotContentType, gotBody string
	filter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("policy configured"))
	}))
	defer filter.Close()

	server := &Server{httpClient: filter.Client(), filterURL: filter.URL}
	req := httptest.NewRequest(http.MethodPost, "/sandbox-policy", strings.NewReader(`{"allowed":["slack.com"],"count_mode":false}`))
	resp := httptest.NewRecorder()

	server.handleSandboxPolicy(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	if gotMethod != http.MethodPost || gotContentType != "application/json" {
		t.Fatalf("forwarded request = %s %q", gotMethod, gotContentType)
	}
	if gotBody != `{"allowed":["slack.com"],"count_mode":false}` {
		t.Fatalf("forwarded body = %q", gotBody)
	}
}

func TestHandleSandboxPolicyReturnsServiceUnavailable(t *testing.T) {
	server := &Server{filterURL: "http://127.0.0.1:1", httpClient: &http.Client{}}
	req := httptest.NewRequest(http.MethodPost, "/sandbox-policy", strings.NewReader(`{}`))
	resp := httptest.NewRecorder()

	server.handleSandboxPolicy(resp, req)

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusServiceUnavailable)
	}
}
