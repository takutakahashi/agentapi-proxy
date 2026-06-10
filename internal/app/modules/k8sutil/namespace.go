package k8sutil

import (
	"os"
	"strings"
)

// ResolveNamespace returns the first non-empty configured namespace, falling
// back to the in-cluster service account namespace and then "default".
func ResolveNamespace(candidates ...string) string {
	for _, candidate := range candidates {
		if namespace := strings.TrimSpace(candidate); namespace != "" {
			return namespace
		}
	}
	if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if namespace := strings.TrimSpace(string(data)); namespace != "" {
			return namespace
		}
	}
	return "default"
}
