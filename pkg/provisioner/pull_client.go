package provisioner

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type PullClientConfig struct {
	ProxyURL  string
	Token     string
	SessionID string
	PodName   string
	Namespace string
	CAFile    string
}

type pullProvisionRequest struct {
	RequestID string                           `json:"request_id"`
	Type      string                           `json:"type"`
	Settings  *sessionsettings.SessionSettings `json:"settings"`
}

// RunPullClient connects this session Pod to the proxy and claims provision requests.
func RunPullClient(ctx context.Context, srv *Server, cfg PullClientConfig) error {
	cfg.ProxyURL = strings.TrimRight(cfg.ProxyURL, "/")
	if cfg.PodName == "" {
		cfg.PodName, _ = os.Hostname()
	}
	if cfg.ProxyURL == "" || cfg.Token == "" || cfg.SessionID == "" {
		return fmt.Errorf("pull provisioner requires proxy URL, token, and session ID")
	}
	client, err := newPullHTTPClient(ctx, cfg.CAFile)
	if err != nil {
		return err
	}
	if err := postJSON(ctx, client, cfg, "/internal/session-provisioners/connect", map[string]interface{}{
		"session_id": cfg.SessionID,
		"pod_name":   cfg.PodName,
		"namespace":  cfg.Namespace,
	}); err != nil {
		log.Printf("[PROVISIONER] Initial connect failed: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		provisionReq, ok, err := pollProvisionRequest(ctx, client, cfg)
		if err != nil {
			log.Printf("[PROVISIONER] Failed to poll provision request: %v", err)
			sleepOrDone(ctx, 5*time.Second)
			continue
		}
		if !ok {
			continue
		}
		if provisionReq.Settings == nil {
			_ = reportProvisionRequestStatus(ctx, client, cfg, provisionReq.RequestID, StatusError, "provision request has no settings")
			continue
		}

		srv.SetStatusReporter(func(st Status, msg string) {
			go func() {
				if err := reportProvisionRequestStatus(context.Background(), client, cfg, provisionReq.RequestID, st, msg); err != nil {
					log.Printf("[PROVISIONER] Failed to report status %s for provision request %s: %v", st, provisionReq.RequestID, err)
				}
			}()
		})
		srv.setStatus(StatusProvisioning, "")
		srv.runProvision(ctx, provisionReq.Settings)
		<-ctx.Done()
		return ctx.Err()
	}
}

func newPullHTTPClient(ctx context.Context, caFile string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if caFile == "" {
		return &http.Client{Timeout: 35 * time.Second, Transport: transport}, nil
	}

	for {
		caPEM, err := os.ReadFile(caFile)
		if err == nil {
			roots, rootsErr := x509.SystemCertPool()
			if rootsErr != nil || roots == nil {
				roots = x509.NewCertPool()
			}
			if !roots.AppendCertsFromPEM(caPEM) {
				return nil, fmt.Errorf("pull provisioner CA file %s contains no certificates", caFile)
			}
			transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
			return &http.Client{Timeout: 35 * time.Second, Transport: transport}, nil
		}

		log.Printf("[PROVISIONER] Waiting for SCIA CA %s: %v", caFile, err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func pollProvisionRequest(ctx context.Context, client *http.Client, cfg PullClientConfig) (*pullProvisionRequest, bool, error) {
	u, err := url.Parse(cfg.ProxyURL + "/internal/session-provisioners/" + url.PathEscape(cfg.SessionID) + "/provision-requests")
	if err != nil {
		return nil, false, err
	}
	q := u.Query()
	q.Set("wait", "30s")
	q.Set("pod_name", cfg.PodName)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode == http.StatusNoContent {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("poll provision request returned HTTP %d", resp.StatusCode)
	}
	var provisionReq pullProvisionRequest
	if err := json.NewDecoder(resp.Body).Decode(&provisionReq); err != nil {
		return nil, false, err
	}
	return &provisionReq, true, nil
}

func reportProvisionRequestStatus(ctx context.Context, client *http.Client, cfg PullClientConfig, requestID string, st Status, msg string) error {
	path := "/internal/session-provisioners/" + url.PathEscape(cfg.SessionID) + "/provision-requests/" + url.PathEscape(requestID) + "/status"
	return postJSON(ctx, client, cfg, path, map[string]interface{}{
		"status":   string(st),
		"message":  msg,
		"pod_name": cfg.PodName,
	})
}

func postJSON(ctx context.Context, client *http.Client, cfg PullClientConfig, path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.ProxyURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s returned HTTP %d", path, resp.StatusCode)
	}
	return nil
}

func sleepOrDone(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
