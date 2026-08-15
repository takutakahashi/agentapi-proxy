package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	infra "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSessionPoolRunnerClaimLifecycle(t *testing.T) {
	store := infra.NewKubernetesStore(fake.NewSimpleClientset(), "test")
	controller := NewSessionPoolController(store, nil)

	managerResult := callSessionPoolHandler(t, controller.CreateManager, http.MethodPost, "/admin/session-managers",
		map[string]any{"id": "manager-a", "name": "Manager A"}, nil, nil)
	if managerResult.Code != http.StatusCreated {
		t.Fatalf("create manager status=%d body=%s", managerResult.Code, managerResult.Body.String())
	}
	var created struct {
		ConnectionToken string `json:"connection_token"`
	}
	decodeRecorder(t, managerResult, &created)
	if created.ConnectionToken == "" {
		t.Fatal("manager connection token was not returned")
	}
	if _, err := store.GetManager(context.Background(), "manager-a"); err != nil {
		t.Fatalf("created manager not persisted: %v, body=%s", err, managerResult.Body.String())
	}

	poolResult := callSessionPoolHandler(t, controller.CreatePool, http.MethodPost, "/admin/session-managers/manager-a/pools",
		map[string]any{"name": "linux", "min_idle": 1, "max_runners": 2}, map[string]string{"id": "manager-a"}, nil)
	if poolResult.Code != http.StatusCreated {
		t.Fatalf("create pool status=%d body=%s", poolResult.Code, poolResult.Body.String())
	}
	pool, err := store.GetPool(context.Background(), "manager-a", "linux")
	if err != nil || !pool.Enabled {
		t.Fatalf("pool should default enabled: pool=%+v err=%v", pool, err)
	}

	bindingResult := callSessionPoolHandler(t, controller.CreateBinding, http.MethodPost, "/admin/session-pools/linux/bindings",
		map[string]any{"subject_type": "team", "subject_id": "acme/platform"}, map[string]string{"pool": "linux"}, nil)
	if bindingResult.Code != http.StatusCreated {
		t.Fatalf("create binding status=%d body=%s", bindingResult.Code, bindingResult.Body.String())
	}
	bindings, err := store.ListBindings(context.Background(), "linux")
	if err != nil || len(bindings) != 1 || !bindings[0].Enabled {
		t.Fatalf("binding should default enabled: bindings=%+v err=%v", bindings, err)
	}

	registerResult := callSessionPoolHandler(t, controller.RegisterRunner, http.MethodPost, "/internal/session-runners/register",
		map[string]any{"runner_id": "runner-a", "pool": "linux", "pod_name": "pod-a"}, nil,
		map[string]string{"Authorization": "Bearer " + created.ConnectionToken, "X-Session-Manager-ID": "manager-a"})
	if registerResult.Code != http.StatusCreated {
		t.Fatalf("register runner status=%d body=%s", registerResult.Code, registerResult.Body.String())
	}
	var registered struct {
		RunnerToken string `json:"runner_token"`
	}
	decodeRecorder(t, registerResult, &registered)

	if err := store.Enqueue(context.Background(), &core.Allocation{SessionID: "session-a", Pool: "linux", RuntimeToken: "runtime-secret"}); err != nil {
		t.Fatal(err)
	}
	claimResult := callSessionPoolHandler(t, controller.ClaimRunnerAllocation, http.MethodGet, "/internal/session-runners/allocations/next?wait=0s",
		nil, nil, map[string]string{"Authorization": "Bearer " + registered.RunnerToken, "X-Session-Runner-ID": "runner-a"})
	if claimResult.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", claimResult.Code, claimResult.Body.String())
	}
	var claim struct {
		LeaseID      string `json:"lease_id"`
		RuntimeToken string `json:"runtime_token"`
	}
	decodeRecorder(t, claimResult, &claim)
	if claim.LeaseID == "" || claim.RuntimeToken != "runtime-secret" {
		t.Fatalf("invalid claim response: %+v", claim)
	}

	ackResult := callSessionPoolHandler(t, controller.AckRunnerAllocation, http.MethodPost, "/internal/session-runners/allocations/session-a/ack",
		map[string]any{"lease_id": claim.LeaseID}, map[string]string{"sessionId": "session-a"},
		map[string]string{"Authorization": "Bearer " + registered.RunnerToken, "X-Session-Runner-ID": "runner-a"})
	if ackResult.Code != http.StatusOK {
		t.Fatalf("ack status=%d body=%s", ackResult.Code, ackResult.Body.String())
	}
	runner, err := store.GetRunner(context.Background(), "runner-a")
	if err != nil || runner.Status != core.RunnerRunning {
		t.Fatalf("runner should be running: runner=%+v err=%v", runner, err)
	}
}

func callSessionPoolHandler(t *testing.T, handler echo.HandlerFunc, method, target string, body any, params, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(raw))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	ctx := echo.New().NewContext(req, recorder)
	names := make([]string, 0, len(params))
	values := make([]string, 0, len(params))
	for key, value := range params {
		names = append(names, key)
		values = append(values, value)
	}
	ctx.SetParamNames(names...)
	ctx.SetParamValues(values...)
	if err := handler(ctx); err != nil {
		echo.New().HTTPErrorHandler(err, ctx)
	}
	return recorder
}

func decodeRecorder(t *testing.T, recorder *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), out); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
}
