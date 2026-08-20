package auth

import "testing"

func TestWorkerControlAPIUsesInternalTokenAuthentication(t *testing.T) {
	if !isInternalTokenEndpoint("/internal/worker/sessions") {
		t.Fatal("worker control API must bypass user authentication middleware")
	}
	if isInternalTokenEndpoint("/internal/workers") {
		t.Fatal("unrelated path must not bypass user authentication middleware")
	}
}
