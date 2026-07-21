package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// NativeSessionManager runs one agent-provisioner process per session on the host.
// Provision settings are kept in memory and pulled by the unmodified provisioner
// through the existing internal provisioner API.
type NativeSessionManager struct {
	mu                sync.RWMutex
	sessions          map[string]*NativeSession
	provisionRequests map[string]*ProvisionRequest
	stateDir          string
	proxyURL          string
	provisionerToken  string
	upstreamAuthToken string
	binaryPath        string
	filesystemSandbox bool
	httpClient        *http.Client
}

type NativeSession struct {
	mu              sync.RWMutex
	id              string
	request         *entities.RunServerRequest
	rootDir         string
	agentPort       int
	provisionerPort int
	cmd             *exec.Cmd
	pid             int
	startedAt       time.Time
	updatedAt       time.Time
	lastMessageAt   time.Time
	status          string
	description     string
	cancel          context.CancelFunc
}

type nativeSessionState struct {
	ID                string                     `json:"id"`
	Request           *entities.RunServerRequest `json:"request"`
	RootDir           string                     `json:"root_dir"`
	AgentPort         int                        `json:"agent_port"`
	ProvisionerPort   int                        `json:"provisioner_port"`
	PID               int                        `json:"pid"`
	StartedAt         time.Time                  `json:"started_at"`
	UpdatedAt         time.Time                  `json:"updated_at"`
	LastMessageAt     time.Time                  `json:"last_message_at"`
	Status            string                     `json:"status"`
	Description       string                     `json:"description,omitempty"`
	FilesystemSandbox bool                       `json:"filesystem_sandbox,omitempty"`
}

func NewNativeSessionManager(stateDir, proxyURL, provisionerToken, upstreamAuthToken, binaryPath string, filesystemSandbox bool) (*NativeSessionManager, error) {
	if stateDir == "" {
		return nil, errors.New("native state directory is required")
	}
	if proxyURL == "" {
		return nil, errors.New("native provisioner proxy URL is required")
	}
	if provisionerToken == "" {
		return nil, errors.New("native provisioner token is required")
	}
	if binaryPath == "" {
		var err error
		binaryPath, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve executable: %w", err)
		}
	}
	if filesystemSandbox && runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("native filesystem sandbox is only supported on macOS")
	}
	if filesystemSandbox {
		if _, err := os.Stat(nativeSandboxExecPath); err != nil {
			return nil, fmt.Errorf("native filesystem sandbox requires %s: %w", nativeSandboxExecPath, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "sessions"), 0o700); err != nil {
		return nil, fmt.Errorf("create native state directory: %w", err)
	}
	m := &NativeSessionManager{
		sessions:          make(map[string]*NativeSession),
		provisionRequests: make(map[string]*ProvisionRequest),
		stateDir:          stateDir,
		proxyURL:          strings.TrimRight(proxyURL, "/"),
		provisionerToken:  provisionerToken,
		upstreamAuthToken: upstreamAuthToken,
		binaryPath:        binaryPath,
		filesystemSandbox: filesystemSandbox,
		httpClient:        &http.Client{Timeout: 30 * time.Second},
	}
	if err := m.restoreSessions(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *NativeSessionManager) CreateSession(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	return m.CreateSessionDirect(ctx, id, req, webhookPayload)
}

func (m *NativeSessionManager) CreateSessionDirect(_ context.Context, id string, req *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	if req == nil {
		return nil, errors.New("native session requires allocation metadata")
	}
	m.mu.Lock()
	if _, exists := m.sessions[id]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("session %s already exists", id)
	}
	m.mu.Unlock()

	root := filepath.Join(m.stateDir, "sessions", id)
	home := filepath.Join(root, "home")
	workdir := filepath.Join(root, "workdir")
	runtimeDir := filepath.Join(root, "runtime")
	buildDir := filepath.Join(root, "build")
	tmpDir := filepath.Join(root, "tmp")
	for _, dir := range []string{home, workdir, runtimeDir, buildDir, tmpDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create session directory %s: %w", dir, err)
		}
	}
	configureNativeRepositoryCloneDir(req, workdir)
	agentPort, err := reserveTCPPort()
	if err != nil {
		return nil, err
	}
	provisionerPort, err := reserveTCPPort()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	sessionCtx, cancel := context.WithCancel(context.Background())
	s := &NativeSession{
		id: id, request: req, rootDir: root, agentPort: agentPort,
		provisionerPort: provisionerPort, startedAt: now, updatedAt: now,
		lastMessageAt: now, status: "creating", description: req.InitialMessage, cancel: cancel,
	}
	provisionReq := &ProvisionRequest{
		RequestID: id + "-provision-1", SessionID: id, Type: provisionRequestType,
		Settings: req.ProvisionSettings, Status: "pending", UpdatedAt: now,
	}

	logPath := filepath.Join(runtimeDir, "provisioner.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open provisioner log: %w", err)
	}
	cmd, err := m.newProvisionerCommand(sessionCtx, root, runtimeDir, provisionerPort)
	if err != nil {
		_ = logFile.Close()
		cancel()
		return nil, err
	}
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"TMPDIR="+tmpDir,
		"CFFIXED_USER_HOME="+home,
		"AGENTAPI_NATIVE_SESSION_ROOT="+root,
		"AGENTAPI_WORKDIR="+workdir,
		"AGENTAPI_BUILD_DIR="+buildDir,
		"AGENTAPI_REPO_DIR="+filepath.Join(workdir, "repo"),
		"AGENTAPI_SESSION_ID="+id,
		"AGENTAPI_PORT="+strconv.Itoa(agentPort),
		"PROVISIONER_PROXY_URL="+m.proxyURL,
		"PROVISIONER_TOKEN="+m.provisionerToken,
		"PROVISIONER_UPSTREAM_AUTH_TOKEN="+m.upstreamAuthToken,
		"POD_NAME=native-"+id,
		"POD_NAMESPACE=native",
		"PROVISIONER_PRE_SCRIPT=true",
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	m.mu.Lock()
	m.sessions[id] = s
	m.provisionRequests[id] = provisionReq
	m.mu.Unlock()
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		cancel()
		m.mu.Lock()
		delete(m.sessions, id)
		delete(m.provisionRequests, id)
		m.mu.Unlock()
		return nil, fmt.Errorf("start agent-provisioner: %w", err)
	}
	s.cmd = cmd
	s.pid = cmd.Process.Pid
	if err := m.persistSession(s); err != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		return nil, err
	}
	go func() {
		err := cmd.Wait()
		_ = logFile.Close()
		s.mu.Lock()
		if s.status != "stopped" {
			s.status = "error"
			if err != nil {
				s.description = err.Error()
			}
		}
		s.updatedAt = time.Now().UTC()
		s.mu.Unlock()
		_ = m.persistSession(s)
	}()
	return s, nil
}

func configureNativeRepositoryCloneDir(req *entities.RunServerRequest, workdir string) {
	cloneDir := filepath.Join(workdir, "repo")
	if req.RepoInfo != nil {
		req.RepoInfo.CloneDir = cloneDir
	}
	if req.ProvisionSettings != nil && req.ProvisionSettings.Repository != nil {
		req.ProvisionSettings.Repository.CloneDir = cloneDir
	}
}

func reserveTCPPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve TCP port: %w", err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func (m *NativeSessionManager) ValidateProvisionerToken(token string) bool {
	return token != "" && token == m.provisionerToken
}

func (m *NativeSessionManager) UsesRemoteProvisioner() bool { return true }

func (m *NativeSessionManager) ConnectProvisioner(_ context.Context, req ProvisionerConnectRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.provisionRequests[req.SessionID]
	if p == nil {
		return fmt.Errorf("provision request for session %s not found", req.SessionID)
	}
	p.ClaimedBy = req.PodName
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *NativeSessionManager) ClaimProvisionRequest(_ context.Context, sessionID, podName string) (*ProvisionRequest, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.provisionRequests[sessionID]
	if p == nil {
		return nil, false, nil
	}
	if p.Status == "ready" || p.Status == "claimed" || p.Status == "provisioning" {
		return nil, false, nil
	}
	p.Status = "claimed"
	p.ClaimedBy = podName
	p.UpdatedAt = time.Now().UTC()
	copyReq := *p
	return &copyReq, true, nil
}

func (m *NativeSessionManager) UpdateProvisionRequestStatus(_ context.Context, sessionID, requestID string, update ProvisionRequestStatusUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.provisionRequests[sessionID]
	if p == nil {
		return fmt.Errorf("provision request for session %s not found", sessionID)
	}
	if p.RequestID != requestID {
		return fmt.Errorf("provision request id mismatch")
	}
	p.Status, p.Message, p.UpdatedAt = update.Status, update.Message, time.Now().UTC()
	if update.PodName != "" {
		p.ClaimedBy = update.PodName
	}
	if s := m.sessions[sessionID]; s != nil {
		s.mu.Lock()
		s.status = update.Status
		if update.Status == "ready" {
			s.status = "running"
		}
		s.updatedAt = time.Now().UTC()
		s.mu.Unlock()
		_ = m.persistSession(s)
	}
	return nil
}

func (m *NativeSessionManager) GetSession(id string) entities.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

func (m *NativeSessionManager) ListSessions(filter entities.SessionFilter) []entities.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]entities.Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		if filter.UserID != "" && s.UserID() != filter.UserID {
			continue
		}
		if filter.Scope != "" && s.Scope() != filter.Scope {
			continue
		}
		if filter.TeamID != "" && s.TeamID() != filter.TeamID {
			continue
		}
		if filter.Status != "" && s.Status() != filter.Status {
			continue
		}
		result = append(result, s)
	}
	return result
}

func (m *NativeSessionManager) DeleteSession(id string) error {
	m.mu.Lock()
	s := m.sessions[id]
	if s == nil {
		m.mu.Unlock()
		return fmt.Errorf("session %s not found", id)
	}
	delete(m.sessions, id)
	delete(m.provisionRequests, id)
	m.mu.Unlock()
	s.mu.Lock()
	s.status = "stopped"
	s.updatedAt = time.Now().UTC()
	pid := s.pid
	cancel := s.cancel
	s.mu.Unlock()
	if pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
	}
	if cancel != nil {
		cancel()
	}
	go terminateNativeProcessGroup(pid, s.rootDir)
	return nil
}

func terminateNativeProcessGroup(pid int, root string) {
	if pid > 0 {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if syscall.Kill(pid, 0) != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if syscall.Kill(pid, 0) == nil {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}
	_ = os.RemoveAll(root)
}

func (m *NativeSessionManager) SendMessage(ctx context.Context, id, message string) error {
	s := m.nativeSession(id)
	if s == nil {
		return fmt.Errorf("session %s not found", id)
	}
	path := "/message"
	payload, _ := json.Marshal(map[string]string{"content": message, "type": "user"})
	if s.request != nil && strings.HasSuffix(s.request.AgentType, "-acp") {
		acpSessionID, err := m.getACPSessionID(ctx, s)
		if err != nil {
			return err
		}
		path = "/rpc"
		payload, _ = json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      time.Now().UnixNano(),
			"method":  "session/prompt",
			"params": map[string]interface{}{
				"sessionId": acpSessionID,
				"prompt":    []map[string]string{{"type": "text", "text": message}},
			},
		})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+s.Addr()+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("message returned %d: %s", resp.StatusCode, body)
	}
	s.mu.Lock()
	s.lastMessageAt, s.updatedAt = time.Now().UTC(), time.Now().UTC()
	s.mu.Unlock()
	_ = m.persistSession(s)
	return nil
}

func (m *NativeSessionManager) getACPSessionID(ctx context.Context, s *NativeSession) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+s.Addr()+"/session", nil)
	if err != nil {
		return "", err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("ACP session returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.SessionID == "" {
		return "", errors.New("ACP session ID is empty")
	}
	return result.SessionID, nil
}

func (m *NativeSessionManager) StopAgent(ctx context.Context, id string) error {
	s := m.nativeSession(id)
	if s == nil {
		return fmt.Errorf("session %s not found", id)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+s.Addr()+"/stop", nil)
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("stop returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (m *NativeSessionManager) GetMessages(ctx context.Context, id string) ([]portrepos.Message, error) {
	s := m.nativeSession(id)
	if s == nil {
		return nil, fmt.Errorf("session %s not found", id)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+s.Addr()+"/messages", nil)
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("messages returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Messages []portrepos.Message `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Messages, nil
}

func (m *NativeSessionManager) Shutdown(_ time.Duration) error {
	m.mu.RLock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	for _, id := range ids {
		_ = m.DeleteSession(id)
	}
	return nil
}

func (m *NativeSessionManager) nativeSession(id string) *NativeSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

func (m *NativeSessionManager) persistSession(s *NativeSession) error {
	s.mu.RLock()
	state := nativeSessionState{ID: s.id, Request: s.request, RootDir: s.rootDir, AgentPort: s.agentPort,
		ProvisionerPort: s.provisionerPort, PID: s.pid, StartedAt: s.startedAt, UpdatedAt: s.updatedAt,
		LastMessageAt: s.lastMessageAt, Status: s.status, Description: s.description,
		FilesystemSandbox: m.filesystemSandbox}
	s.mu.RUnlock()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal native session state: %w", err)
	}
	path := filepath.Join(s.rootDir, "runtime", "state.json")
	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (m *NativeSessionManager) restoreSessions() error {
	entries, err := os.ReadDir(filepath.Join(m.stateDir, "sessions"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(m.stateDir, "sessions", entry.Name(), "runtime", "state.json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var state nativeSessionState
		if json.Unmarshal(data, &state) != nil || state.ID == "" || state.PID <= 0 {
			continue
		}
		if state.FilesystemSandbox != m.filesystemSandbox {
			continue
		}
		if err := syscall.Kill(state.PID, 0); err != nil || !nativeProcessMatchesSession(state.PID, state.RootDir) {
			continue
		}
		if state.Status == "running" && !nativeAgentHealthy(state.AgentPort) {
			continue
		}
		_, cancel := context.WithCancel(context.Background())
		m.sessions[state.ID] = &NativeSession{id: state.ID, request: state.Request, rootDir: state.RootDir,
			agentPort: state.AgentPort, provisionerPort: state.ProvisionerPort, pid: state.PID,
			startedAt: state.StartedAt, updatedAt: state.UpdatedAt, lastMessageAt: state.LastMessageAt,
			status: state.Status, description: state.Description, cancel: cancel}
	}
	return nil
}

func nativeProcessMatchesSession(pid int, root string) bool {
	needle := "AGENTAPI_NATIVE_SESSION_ROOT=" + root
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "environ"))
		return err == nil && bytes.Contains(data, []byte(needle))
	}
	if runtime.GOOS == "darwin" {
		data, err := exec.Command("ps", "eww", "-p", strconv.Itoa(pid), "-o", "command=").Output()
		return err == nil && bytes.Contains(data, []byte(needle))
	}
	return false
}

func nativeAgentHealthy(port int) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + "/status")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func (s *NativeSession) ID() string { return s.id }
func (s *NativeSession) Addr() string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(s.agentPort))
}
func (s *NativeSession) UserID() string { return s.request.UserID }
func (s *NativeSession) Scope() entities.ResourceScope {
	if s.request.Scope == "" {
		return entities.ScopeUser
	}
	return s.request.Scope
}
func (s *NativeSession) TeamID() string          { return s.request.TeamID }
func (s *NativeSession) Tags() map[string]string { return s.request.Tags }
func (s *NativeSession) Status() string          { s.mu.RLock(); defer s.mu.RUnlock(); return s.status }
func (s *NativeSession) StartedAt() time.Time    { return s.startedAt }
func (s *NativeSession) UpdatedAt() time.Time    { s.mu.RLock(); defer s.mu.RUnlock(); return s.updatedAt }
func (s *NativeSession) LastMessageAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastMessageAt
}
func (s *NativeSession) Description() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.description
}
func (s *NativeSession) Cancel() {
	if s.cancel != nil {
		s.cancel()
	}
}
