package proxy

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

func TestSanitizeLabelFunctions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		fn       func(string) string
	}{
		{
			name:     "sanitize key - normal",
			input:    "valid-key",
			expected: "valid-key",
			fn:       sanitizeLabelKey,
		},
		{
			name:     "sanitize key - special chars",
			input:    "key/with/slashes",
			expected: "key-with-slashes",
			fn:       sanitizeLabelKey,
		},
		{
			name:     "sanitize key - email",
			input:    "user@example.com",
			expected: "user-example.com",
			fn:       sanitizeLabelKey,
		},
		{
			name:     "sanitize value - normal",
			input:    "valid-value",
			expected: "valid-value",
			fn:       sanitizeLabelValue,
		},
		{
			name:     "sanitize value - special chars",
			input:    "value/with/slashes",
			expected: "value-with-slashes",
			fn:       sanitizeLabelValue,
		},
		{
			name:     "sanitize value - long string",
			input:    "this-is-a-very-long-string-that-exceeds-the-kubernetes-label-value-limit-of-63-characters",
			expected: "this-is-a-very-long-string-that-exceeds-the-kubernetes-label-va",
			fn:       sanitizeLabelValue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.fn(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestKubernetesSession_Methods(t *testing.T) {
	// Test that kubernetesSession implements Session interface
	var _ Session = &kubernetesSession{}

	session := &kubernetesSession{
		id:          "test-session",
		serviceName: "test-svc",
		namespace:   "test-ns",
		servicePort: 9000,
		startedAt:   time.Now(),
		status:      "active",
		request: &RunServerRequest{
			UserID: "test-user",
			Tags:   map[string]string{"key": "value"},
		},
	}

	// Test ID
	if session.ID() != "test-session" {
		t.Errorf("Expected ID 'test-session', got %s", session.ID())
	}

	// Test Addr
	expectedAddr := "test-svc.test-ns.svc.cluster.local:9000"
	if session.Addr() != expectedAddr {
		t.Errorf("Expected Addr %s, got %s", expectedAddr, session.Addr())
	}

	// Test UserID
	if session.UserID() != "test-user" {
		t.Errorf("Expected UserID 'test-user', got %s", session.UserID())
	}

	// Test Tags
	if session.Tags()["key"] != "value" {
		t.Errorf("Expected tag 'key'='value', got %s", session.Tags()["key"])
	}

	// Test Status
	if session.Status() != "active" {
		t.Errorf("Expected status 'active', got %s", session.Status())
	}

	// Test setStatus
	session.setStatus("stopped")
	if session.Status() != "stopped" {
		t.Errorf("Expected status 'stopped', got %s", session.Status())
	}

	// Test ServiceDNS
	expectedDNS := "test-svc.test-ns.svc.cluster.local"
	if session.ServiceDNS() != expectedDNS {
		t.Errorf("Expected ServiceDNS %s, got %s", expectedDNS, session.ServiceDNS())
	}

	// Test ErrorMessage (initially empty)
	if session.ErrorMessage() != "" {
		t.Errorf("Expected empty error message, got %s", session.ErrorMessage())
	}

	// Test setError
	session.setError("Container 'test' failed: CrashLoopBackOff")
	if session.Status() != "failed" {
		t.Errorf("Expected status 'failed' after setError, got %s", session.Status())
	}
	if session.ErrorMessage() != "Container 'test' failed: CrashLoopBackOff" {
		t.Errorf("Expected error message 'Container 'test' failed: CrashLoopBackOff', got %s", session.ErrorMessage())
	}
}

func TestKubernetesSession_ErrorMessageThreadSafety(t *testing.T) {
	session := &kubernetesSession{
		id:     "test-session",
		status: "creating",
		request: &RunServerRequest{
			UserID: "test-user",
		},
	}

	// Test concurrent access to ErrorMessage and setError
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			session.setError("error message")
			session.setStatus("active")
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = session.ErrorMessage()
			_ = session.Status()
		}
		done <- true
	}()

	<-done
	<-done
}

func TestCheckPodErrorStatus(t *testing.T) {
	tests := []struct {
		name             string
		pods             []corev1.Pod
		expectedHasError bool
		expectedContains string
	}{
		{
			name:             "no pods",
			pods:             []corev1.Pod{},
			expectedHasError: false,
			expectedContains: "",
		},
		{
			name: "pod running successfully",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "test-ns",
						Labels: map[string]string{
							"agentapi.proxy/session-id": "test-session",
						},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "agentapi",
								State: corev1.ContainerState{
									Running: &corev1.ContainerStateRunning{},
								},
							},
						},
					},
				},
			},
			expectedHasError: false,
			expectedContains: "",
		},
		{
			name: "container in CrashLoopBackOff",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "test-ns",
						Labels: map[string]string{
							"agentapi.proxy/session-id": "test-session",
						},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "agentapi",
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{
										Reason:  "CrashLoopBackOff",
										Message: "back-off 5m0s restarting failed container",
									},
								},
							},
						},
					},
				},
			},
			expectedHasError: true,
			expectedContains: "CrashLoopBackOff",
		},
		{
			name: "container in ImagePullBackOff",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "test-ns",
						Labels: map[string]string{
							"agentapi.proxy/session-id": "test-session",
						},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "agentapi",
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{
										Reason:  "ImagePullBackOff",
										Message: "Back-off pulling image",
									},
								},
							},
						},
					},
				},
			},
			expectedHasError: true,
			expectedContains: "ImagePullBackOff",
		},
		{
			name: "init container in CrashLoopBackOff",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "test-ns",
						Labels: map[string]string{
							"agentapi.proxy/session-id": "test-session",
						},
					},
					Status: corev1.PodStatus{
						InitContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "clone-repo",
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{
										Reason:  "CrashLoopBackOff",
										Message: "back-off",
									},
								},
							},
						},
					},
				},
			},
			expectedHasError: true,
			expectedContains: "Init container 'clone-repo' failed: CrashLoopBackOff",
		},
		{
			name: "init container terminated with error",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "test-ns",
						Labels: map[string]string{
							"agentapi.proxy/session-id": "test-session",
						},
					},
					Status: corev1.PodStatus{
						InitContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "setup-claude",
								State: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										ExitCode: 1,
										Reason:   "Error",
										Message:  "configuration failed",
									},
								},
							},
						},
					},
				},
			},
			expectedHasError: true,
			expectedContains: "Init container 'setup-claude' failed with exit code 1",
		},
		{
			name: "container terminated with error",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "test-ns",
						Labels: map[string]string{
							"agentapi.proxy/session-id": "test-session",
						},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "agentapi",
								State: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										ExitCode: 127,
										Reason:   "ContainerCannotRun",
										Message:  "command not found",
									},
								},
							},
						},
					},
				},
			},
			expectedHasError: true,
			expectedContains: "terminated with exit code 127",
		},
		{
			name: "container in ErrImagePull",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "test-ns",
						Labels: map[string]string{
							"agentapi.proxy/session-id": "test-session",
						},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "agentapi",
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{
										Reason:  "ErrImagePull",
										Message: "rpc error: code = NotFound",
									},
								},
							},
						},
					},
				},
			},
			expectedHasError: true,
			expectedContains: "ErrImagePull",
		},
		{
			name: "container in CreateContainerConfigError",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "test-ns",
						Labels: map[string]string{
							"agentapi.proxy/session-id": "test-session",
						},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "agentapi",
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{
										Reason:  "CreateContainerConfigError",
										Message: "secret not found",
									},
								},
							},
						},
					},
				},
			},
			expectedHasError: true,
			expectedContains: "CreateContainerConfigError",
		},
		{
			name: "container waiting for PodInitializing (not an error)",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "test-ns",
						Labels: map[string]string{
							"agentapi.proxy/session-id": "test-session",
						},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "agentapi",
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{
										Reason:  "PodInitializing",
										Message: "waiting for init containers",
									},
								},
							},
						},
					},
				},
			},
			expectedHasError: false,
			expectedContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with pods
			fakeClient := fake.NewSimpleClientset()
			for i := range tt.pods {
				_, err := fakeClient.CoreV1().Pods("test-ns").Create(
					context.Background(), &tt.pods[i], metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("Failed to create pod: %v", err)
				}
			}

			// Create manager with fake client
			cfg := &config.Config{
				KubernetesSession: config.KubernetesSessionConfig{
					Enabled:   true,
					Namespace: "test-ns",
				},
			}
			lgr := logger.NewLogger()
			manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, fakeClient)
			if err != nil {
				t.Fatalf("Failed to create manager: %v", err)
			}

			// Call checkPodErrorStatus
			hasError, errorMsg := manager.checkPodErrorStatus("test-session")

			// Verify results
			if hasError != tt.expectedHasError {
				t.Errorf("Expected hasError=%v, got %v", tt.expectedHasError, hasError)
			}

			if tt.expectedContains != "" && !strings.Contains(errorMsg, tt.expectedContains) {
				t.Errorf("Expected error message to contain '%s', got '%s'", tt.expectedContains, errorMsg)
			}
		})
	}
}
