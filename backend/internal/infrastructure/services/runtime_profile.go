package services

import (
	"context"
	"fmt"
	"log"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const runtimeProfileVersion = 1

// ExternalRuntimeProfile returns the parent-owned runtime configuration that
// every Kubernetes ESM must inherit for externally allocated sessions.
func (m *KubernetesSessionManager) ExternalRuntimeProfile() *sessionsettings.RuntimeProfile {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	k8s := m.k8sConfig
	scia := m.config.Scia
	return &sessionsettings.RuntimeProfile{
		Version: runtimeProfileVersion,
		Kubernetes: sessionsettings.KubernetesRuntimeProfile{
			ServiceAccount:                 k8s.ServiceAccount,
			NetworkFilterImage:             k8s.NetworkFilterImage,
			NetworkFilterCPURequest:        k8s.NetworkFilterCPURequest,
			NetworkFilterCPULimit:          k8s.NetworkFilterCPULimit,
			NetworkFilterMemoryRequest:     k8s.NetworkFilterMemoryRequest,
			NetworkFilterMemoryLimit:       k8s.NetworkFilterMemoryLimit,
			NetworkFilterInitCPURequest:    k8s.NetworkFilterInitCPURequest,
			NetworkFilterInitCPULimit:      k8s.NetworkFilterInitCPULimit,
			NetworkFilterInitMemoryRequest: k8s.NetworkFilterInitMemoryRequest,
			NetworkFilterInitMemoryLimit:   k8s.NetworkFilterInitMemoryLimit,
		},
		Scia: sessionsettings.SciaRuntimeProfile{
			Enabled:                   scia.Enabled,
			SessionSidecarEnabled:     scia.SessionSidecarEnabled,
			SessionSidecarImage:       scia.SessionSidecarImage,
			SessionSidecarConfigImage: scia.SessionSidecarConfigImage,
			SessionSidecarPort:        scia.SessionSidecarPort,
			Credential:                scia.Credential,
			UserNamespace:             scia.UserNamespace,
			NoProxy:                   scia.NoProxy,
			GoogleHosts:               append([]string(nil), scia.GoogleHosts...),
			GooglePaths:               append([]string(nil), scia.GooglePaths...),
			TodoistCredential:         scia.TodoistCredential,
			TodoistHosts:              append([]string(nil), scia.TodoistHosts...),
			TodoistPaths:              append([]string(nil), scia.TodoistPaths...),
		},
	}
}

// ApplyRuntimeProfile replaces ESM-local runtime settings with the profile
// supplied by the parent and ensures the inherited session identity exists.
func (m *KubernetesSessionManager) ApplyRuntimeProfile(ctx context.Context, profile *sessionsettings.RuntimeProfile) error {
	if profile == nil {
		return nil
	}
	if profile.Version != runtimeProfileVersion {
		return fmt.Errorf("unsupported runtime profile version %d", profile.Version)
	}

	profileCopy := *profile
	profileCopy.Scia.GoogleHosts = append([]string(nil), profile.Scia.GoogleHosts...)
	profileCopy.Scia.GooglePaths = append([]string(nil), profile.Scia.GooglePaths...)
	profileCopy.Scia.TodoistHosts = append([]string(nil), profile.Scia.TodoistHosts...)
	profileCopy.Scia.TodoistPaths = append([]string(nil), profile.Scia.TodoistPaths...)

	m.mutex.Lock()
	m.inheritedRuntimeProfile = &profileCopy
	m.applyRuntimeProfileLocked(&profileCopy)
	m.mutex.Unlock()

	if err := m.ensureRuntimeProfileServiceAccount(ctx, profile.Kubernetes.ServiceAccount); err != nil {
		return err
	}
	log.Printf("[SESSION_MANAGER_ALLOCATOR] Applied parent runtime profile version %d", profile.Version)
	return nil
}

func (m *KubernetesSessionManager) applyRuntimeProfileLocked(profile *sessionsettings.RuntimeProfile) {
	k8s := profile.Kubernetes
	m.k8sConfig.ServiceAccount = k8s.ServiceAccount
	m.k8sConfig.NetworkFilterImage = k8s.NetworkFilterImage
	m.k8sConfig.NetworkFilterCPURequest = k8s.NetworkFilterCPURequest
	m.k8sConfig.NetworkFilterCPULimit = k8s.NetworkFilterCPULimit
	m.k8sConfig.NetworkFilterMemoryRequest = k8s.NetworkFilterMemoryRequest
	m.k8sConfig.NetworkFilterMemoryLimit = k8s.NetworkFilterMemoryLimit
	m.k8sConfig.NetworkFilterInitCPURequest = k8s.NetworkFilterInitCPURequest
	m.k8sConfig.NetworkFilterInitCPULimit = k8s.NetworkFilterInitCPULimit
	m.k8sConfig.NetworkFilterInitMemoryRequest = k8s.NetworkFilterInitMemoryRequest
	m.k8sConfig.NetworkFilterInitMemoryLimit = k8s.NetworkFilterInitMemoryLimit

	scia := profile.Scia
	m.config.Scia.Enabled = scia.Enabled
	m.config.Scia.SessionSidecarEnabled = scia.SessionSidecarEnabled
	m.config.Scia.SessionSidecarImage = scia.SessionSidecarImage
	m.config.Scia.SessionSidecarConfigImage = scia.SessionSidecarConfigImage
	m.config.Scia.SessionSidecarPort = scia.SessionSidecarPort
	m.config.Scia.Credential = scia.Credential
	m.config.Scia.UserNamespace = scia.UserNamespace
	m.config.Scia.NoProxy = scia.NoProxy
	m.config.Scia.GoogleHosts = append([]string(nil), scia.GoogleHosts...)
	m.config.Scia.GooglePaths = append([]string(nil), scia.GooglePaths...)
	m.config.Scia.TodoistCredential = scia.TodoistCredential
	m.config.Scia.TodoistHosts = append([]string(nil), scia.TodoistHosts...)
	m.config.Scia.TodoistPaths = append([]string(nil), scia.TodoistPaths...)
}

func (m *KubernetesSessionManager) ensureRuntimeProfileServiceAccount(ctx context.Context, name string) error {
	if name == "" || name == "default" {
		return nil
	}
	serviceAccounts := m.client.CoreV1().ServiceAccounts(m.namespace)
	if _, err := serviceAccounts.Get(ctx, name, metav1.GetOptions{}); apierrors.IsNotFound(err) {
		if _, err = serviceAccounts.Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name}}, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create inherited session service account: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("get inherited session service account: %w", err)
	}

	rules := []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"pods", "pods/log", "configmaps"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get", "list", "create", "update", "patch"}},
	}
	roles := m.client.RbacV1().Roles(m.namespace)
	role, err := roles.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = roles.Create(ctx, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name}, Rules: rules}, metav1.CreateOptions{})
	} else if err == nil {
		role.Rules = rules
		_, err = roles.Update(ctx, role, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("ensure inherited session role: %w", err)
	}

	bindings := m.client.RbacV1().RoleBindings(m.namespace)
	binding, err := bindings.Get(ctx, name, metav1.GetOptions{})
	desiredSubjects := []rbacv1.Subject{{Kind: "ServiceAccount", Name: name, Namespace: m.namespace}}
	desiredRoleRef := rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name}
	if apierrors.IsNotFound(err) {
		_, err = bindings.Create(ctx, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name}, Subjects: desiredSubjects, RoleRef: desiredRoleRef}, metav1.CreateOptions{})
	} else if err == nil {
		binding.Subjects = desiredSubjects
		binding.RoleRef = desiredRoleRef
		_, err = bindings.Update(ctx, binding, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("ensure inherited session role binding: %w", err)
	}
	return nil
}
