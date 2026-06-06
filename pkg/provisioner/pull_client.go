package provisioner

import (
	"bytes"
	"context"
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
}

type pullJob struct {
	JobID    string                           `json:"job_id"`
	Type     string                           `json:"type"`
	Settings *sessionsettings.SessionSettings `json:"settings"`
}

// RunPullClient connects this session Pod to the proxy and claims provisioning jobs.
func RunPullClient(ctx context.Context, srv *Server, cfg PullClientConfig) error {
	cfg.ProxyURL = strings.TrimRight(cfg.ProxyURL, "/")
	if cfg.PodName == "" {
		cfg.PodName, _ = os.Hostname()
	}
	if cfg.ProxyURL == "" || cfg.Token == "" || cfg.SessionID == "" {
		return fmt.Errorf("pull provisioner requires proxy URL, token, and session ID")
	}
	client := &http.Client{Timeout: 35 * time.Second}
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

		job, ok, err := pollJob(ctx, client, cfg)
		if err != nil {
			log.Printf("[PROVISIONER] Failed to poll provision job: %v", err)
			sleepOrDone(ctx, 5*time.Second)
			continue
		}
		if !ok {
			continue
		}
		if job.Settings == nil {
			_ = reportJobStatus(ctx, client, cfg, job.JobID, StatusError, "job has no settings")
			continue
		}

		srv.SetStatusReporter(func(st Status, msg string) {
			go func() {
				if err := reportJobStatus(context.Background(), client, cfg, job.JobID, st, msg); err != nil {
					log.Printf("[PROVISIONER] Failed to report status %s for job %s: %v", st, job.JobID, err)
				}
			}()
		})
		srv.setStatus(StatusProvisioning, "")
		srv.runProvision(ctx, job.Settings)
		<-ctx.Done()
		return ctx.Err()
	}
}

func pollJob(ctx context.Context, client *http.Client, cfg PullClientConfig) (*pullJob, bool, error) {
	u, err := url.Parse(cfg.ProxyURL + "/internal/session-provisioners/" + url.PathEscape(cfg.SessionID) + "/jobs")
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
		return nil, false, fmt.Errorf("poll job returned HTTP %d", resp.StatusCode)
	}
	var job pullJob
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, false, err
	}
	return &job, true, nil
}

func reportJobStatus(ctx context.Context, client *http.Client, cfg PullClientConfig, jobID string, st Status, msg string) error {
	path := "/internal/session-provisioners/" + url.PathEscape(cfg.SessionID) + "/jobs/" + url.PathEscape(jobID) + "/status"
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
