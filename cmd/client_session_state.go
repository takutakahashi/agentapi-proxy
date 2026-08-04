package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionstate"
)

var backupSessionStateCmd = &cobra.Command{Use: "backup-session-state", Short: "Back up ACP state through the proxy", Args: cobra.NoArgs, RunE: runBackupSessionState}

func runBackupSessionState(_ *cobra.Command, _ []string) error {
	proxy := strings.TrimRight(os.Getenv("PROVISIONER_PROXY_URL"), "/")
	token := os.Getenv("PROVISIONER_TOKEN")
	id := os.Getenv("AGENTAPI_SESSION_ID")
	agentType := os.Getenv("AGENTAPI_AGENT_TYPE")
	if proxy == "" || token == "" || id == "" {
		return fmt.Errorf("PROVISIONER_PROXY_URL, PROVISIONER_TOKEN and AGENTAPI_SESSION_ID are required")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	home := os.Getenv("HOME")
	if home == "" {
		home = "/home/agentapi"
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	direct, err := beginDirectUpload(client, proxy, token, id)
	if err == nil && direct != nil {
		if err := uploadSessionStateDirect(context.Background(), client, proxy, token, id, agentType, readACPSessionID(cwd), home, cwd, *direct); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "session state backup skipped: direct persistence transfer failed: %v\n", err)
		}
		return nil
	}
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "session state backup skipped: persistence backend is unavailable: %v\n", err)
		return nil
	}
	bodyReader, bodyWriter := io.Pipe()
	go func() {
		bodyWriter.CloseWithError(sessionstate.Pack(bodyWriter, agentType, readACPSessionID(cwd), home, cwd))
	}()
	req, err := http.NewRequest(http.MethodPut, proxy+"/internal/session-state/"+id, bodyReader)
	if err != nil {
		_ = bodyReader.Close()
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/zstd")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		if resp.StatusCode == http.StatusServiceUnavailable {
			_, _ = fmt.Fprintln(os.Stderr, "session state backup skipped: persistence backend is unavailable")
			return nil
		}
		return fmt.Errorf("session state backup failed: HTTP %d", resp.StatusCode)
	}
	return nil
}

type directUpload struct {
	UploadID string `json:"upload_id"`
	PartSize int64  `json:"part_size"`
}
type uploadedPart struct {
	Number int32  `json:"number"`
	ETag   string `json:"etag"`
}

func internalRequest(method, url, token string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, err
}

func beginDirectUpload(client *http.Client, proxy, token, id string) (*directUpload, error) {
	req, err := internalRequest(http.MethodPost, proxy+"/internal/session-state/"+id+"/uploads", token, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotImplemented {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("begin upload HTTP %d", resp.StatusCode)
	}
	var result directUpload
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.UploadID == "" || result.PartSize < 5<<20 {
		return nil, fmt.Errorf("invalid multipart response")
	}
	return &result, nil
}

func uploadSessionStateDirect(ctx context.Context, client *http.Client, proxy, token, id, agentType, acpID, home, cwd string, upload directUpload) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	go func() { writer.CloseWithError(sessionstate.Pack(writer, agentType, acpID, home, cwd)) }()
	type job struct {
		number int32
		data   []byte
	}
	jobs := make(chan job, 2)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var parts []uploadedPart
	var firstErr error
	worker := func() {
		defer wg.Done()
		for job := range jobs {
			if ctx.Err() != nil {
				continue
			}
			etag, err := uploadDirectPart(ctx, client, proxy, token, id, upload.UploadID, job.number, job.data)
			mu.Lock()
			if err != nil && firstErr == nil {
				firstErr = err
				cancel()
			} else if err == nil {
				parts = append(parts, uploadedPart{Number: job.number, ETag: etag})
			}
			mu.Unlock()
		}
	}
	for range 2 {
		wg.Add(1)
		go worker()
	}
	var number int32 = 1
	for {
		buf := make([]byte, upload.PartSize)
		n, readErr := io.ReadFull(reader, buf)
		if n > 0 {
			select {
			case jobs <- job{number: number, data: buf[:n]}:
				number++
			case <-ctx.Done():
			}
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			mu.Lock()
			if firstErr == nil {
				firstErr = readErr
			}
			mu.Unlock()
			cancel()
			break
		}
	}
	close(jobs)
	wg.Wait()
	mu.Lock()
	err := firstErr
	mu.Unlock()
	if err != nil {
		abortDirectUpload(client, proxy, token, id, upload.UploadID)
		return err
	}
	if len(parts) == 0 {
		abortDirectUpload(client, proxy, token, id, upload.UploadID)
		return fmt.Errorf("empty snapshot")
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Number < parts[j].Number })
	payload, _ := json.Marshal(map[string]any{"parts": parts})
	req, err := internalRequest(http.MethodPost, proxy+"/internal/session-state/"+id+"/uploads/"+upload.UploadID+"/complete", token, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("complete upload HTTP %d", resp.StatusCode)
	}
	return nil
}

func uploadDirectPart(ctx context.Context, client *http.Client, proxy, token, id, uploadID string, number int32, data []byte) (string, error) {
	req, err := internalRequest(http.MethodGet, proxy+"/internal/session-state/"+id+"/uploads/"+uploadID+"/parts/"+strconv.Itoa(int(number)), token, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	var signed struct {
		URL string `json:"url"`
	}
	if resp.StatusCode == http.StatusOK {
		err = json.NewDecoder(resp.Body).Decode(&signed)
	} else {
		err = fmt.Errorf("presign part HTTP %d", resp.StatusCode)
	}
	resp.Body.Close()
	if err != nil {
		return "", err
	}
	put, err := http.NewRequestWithContext(ctx, http.MethodPut, signed.URL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	put.ContentLength = int64(len(data))
	result, err := objectStorageClient(signed.URL, client.Timeout).Do(put)
	if err != nil {
		return "", err
	}
	defer result.Body.Close()
	if result.StatusCode/100 != 2 {
		return "", fmt.Errorf("upload part HTTP %d", result.StatusCode)
	}
	return result.Header.Get("ETag"), nil
}

func objectStorageClient(rawURL string, timeout time.Duration) *http.Client {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return &http.Client{Timeout: timeout}
	}
	hostname := parsed.Hostname()
	if strings.Contains(hostname, ".") && !strings.HasSuffix(hostname, ".svc") && !strings.HasSuffix(hostname, ".cluster.local") {
		return &http.Client{Timeout: timeout}
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{Timeout: timeout}
	}
	direct := transport.Clone()
	direct.Proxy = nil
	return &http.Client{Timeout: timeout, Transport: direct}
}

func abortDirectUpload(client *http.Client, proxy, token, id, uploadID string) {
	req, err := internalRequest(http.MethodDelete, proxy+"/internal/session-state/"+id+"/uploads/"+uploadID, token, nil)
	if err == nil {
		resp, callErr := client.Do(req)
		if callErr == nil {
			resp.Body.Close()
		}
	}
}

func readACPSessionID(cwd string) string {
	data, _ := os.ReadFile(filepath.Join(cwd, ".acp-session-id"))
	return strings.TrimSpace(string(data))
}
