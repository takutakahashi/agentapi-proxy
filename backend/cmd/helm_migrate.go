package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client/config"
)

var exactSemver = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)

type helmMigratePlanOptions struct {
	namespace       string
	backendRelease  string
	frontendRelease string
	targetRelease   string
	chart           string
	version         string
	valuesOut       string
	output          string
}

type migrationFinding struct {
	Level   string `json:"level" yaml:"level"`
	Subject string `json:"subject" yaml:"subject"`
	Message string `json:"message" yaml:"message"`
}

type migrationInventory struct {
	Sessions       int `json:"sessions" yaml:"sessions"`
	LegacySessions int `json:"legacySessions" yaml:"legacySessions"`
	RuntimeSecrets int `json:"runtimeSecrets" yaml:"runtimeSecrets"`
	SessionPVCs    int `json:"sessionPVCs" yaml:"sessionPVCs"`
}

type migrationPlan struct {
	Ready             bool               `json:"ready" yaml:"ready"`
	Namespace         string             `json:"namespace" yaml:"namespace"`
	BackendRelease    string             `json:"backendRelease" yaml:"backendRelease"`
	FrontendRelease   string             `json:"frontendRelease" yaml:"frontendRelease"`
	TargetRelease     string             `json:"targetRelease" yaml:"targetRelease"`
	Chart             string             `json:"chart" yaml:"chart"`
	Version           string             `json:"version" yaml:"version"`
	ValuesFile        string             `json:"valuesFile" yaml:"valuesFile"`
	Inventory         migrationInventory `json:"inventory" yaml:"inventory"`
	SecretFingerprint string             `json:"secretFingerprint" yaml:"secretFingerprint"`
	Findings          []migrationFinding `json:"findings" yaml:"findings"`
	Commands          []string           `json:"commands" yaml:"commands"`
}

type migrationVerify struct {
	OK            bool                   `json:"ok" yaml:"ok"`
	Phase         string                 `json:"phase" yaml:"phase"`
	Namespace     string                 `json:"namespace" yaml:"namespace"`
	TargetRelease string                 `json:"targetRelease" yaml:"targetRelease"`
	Inventory     migrationInventory     `json:"inventory" yaml:"inventory"`
	Findings      []migrationFinding     `json:"findings" yaml:"findings"`
	LeaderLeases  []migrationLeaderLease `json:"leaderLeases,omitempty" yaml:"leaderLeases,omitempty"`
}

type migrationLeaderLease struct {
	Worker string `json:"worker" yaml:"worker"`
	Name   string `json:"name" yaml:"name"`
	Holder string `json:"holder,omitempty" yaml:"holder,omitempty"`
}

type serviceProbe func(context.Context, string, string) error

func newHelmMigrateCommand() *cobra.Command {
	o := &helmMigratePlanOptions{}
	root := &cobra.Command{
		Use:   "migrate",
		Short: "Preflight migration from split releases to the ccplant chart",
		Long: `Inspect a split agentapi-proxy/agentapi-ui installation before migration.

This command is deliberately read-only with respect to the Kubernetes cluster.
It validates retained resources and generates shadow-install values and commands;
it never installs, patches, switches traffic, or uninstalls a release.`,
	}
	f := root.PersistentFlags()
	f.StringVarP(&o.namespace, "namespace", "n", "default", "Kubernetes namespace")
	f.StringVar(&o.backendRelease, "backend-release", "agentapi-proxy", "existing backend Helm release")
	f.StringVar(&o.frontendRelease, "frontend-release", "agentapi-ui", "existing frontend Helm release")
	f.StringVar(&o.targetRelease, "target-release", "ccplant", "target ccplant Helm release")
	f.StringVar(&o.chart, "chart", "oci://ghcr.io/ccplant/charts/ccplant", "target chart reference")
	f.StringVar(&o.version, "version", "", "exact target chart version")
	f.StringVar(&o.valuesOut, "values-out", "ccplant-shadow-values.yaml", "generated shadow-install values file")
	f.StringVarP(&o.output, "output", "o", "text", "output format: text, json, or yaml")
	root.AddCommand(&cobra.Command{
		Use: "plan", Short: "Run migration preflight and generate shadow values", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateHelmMigratePlanOptions(o); err != nil {
				return err
			}
			config, err := ctrl.GetConfig()
			if err != nil {
				return fmt.Errorf("get Kubernetes config: %w", err)
			}
			client, err := kubernetes.NewForConfig(config)
			if err != nil {
				return fmt.Errorf("create Kubernetes client: %w", err)
			}
			return runHelmMigratePlan(cmd.Context(), cmd.OutOrStdout(), client, o)
		},
	})
	root.AddCommand(&cobra.Command{
		Use: "verify", Short: "Verify a running shadow or cut-over ccplant release", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateHelmMigrateVerifyOptions(o); err != nil {
				return err
			}
			config, err := ctrl.GetConfig()
			if err != nil {
				return fmt.Errorf("get Kubernetes config: %w", err)
			}
			client, err := kubernetes.NewForConfig(config)
			if err != nil {
				return fmt.Errorf("create Kubernetes client: %w", err)
			}
			probe := func(ctx context.Context, service, path string) error {
				_, probeErr := client.CoreV1().Services(o.namespace).ProxyGet("http", service, "http", path, nil).DoRaw(ctx)
				return probeErr
			}
			return runHelmMigrateVerify(cmd.Context(), cmd.OutOrStdout(), client, o, probe)
		},
	})
	return root
}

func validateHelmMigratePlanOptions(o *helmMigratePlanOptions) error {
	if strings.TrimSpace(o.namespace) == "" {
		return errors.New("namespace must not be empty")
	}
	if o.backendRelease == "" || o.frontendRelease == "" || o.targetRelease == "" {
		return errors.New("release names must not be empty")
	}
	if o.targetRelease == o.backendRelease || o.targetRelease == o.frontendRelease {
		return errors.New("target release must differ from both source releases")
	}
	if !exactSemver.MatchString(o.version) {
		return errors.New("--version must be an exact semantic version, for example 0.3.2")
	}
	if o.output != "text" && o.output != "json" && o.output != "yaml" {
		return fmt.Errorf("unsupported output format %q", o.output)
	}
	return nil
}

func validateHelmMigrateVerifyOptions(o *helmMigratePlanOptions) error {
	if strings.TrimSpace(o.namespace) == "" {
		return errors.New("namespace must not be empty")
	}
	if o.backendRelease == "" || o.targetRelease == "" {
		return errors.New("backend and target release names must not be empty")
	}
	if o.backendRelease == o.targetRelease {
		return errors.New("target release must differ from the source backend release")
	}
	if o.output != "text" && o.output != "json" && o.output != "yaml" {
		return fmt.Errorf("unsupported output format %q", o.output)
	}
	return nil
}

func runHelmMigratePlan(ctx context.Context, out io.Writer, client kubernetes.Interface, o *helmMigratePlanOptions) error {
	backend, _, err := loadLatestHelmRelease(ctx, client, o.namespace, o.backendRelease)
	if err != nil {
		return err
	}
	frontend, _, err := loadLatestHelmRelease(ctx, client, o.namespace, o.frontendRelease)
	if err != nil {
		return err
	}

	values := buildMigrationShadowValues(backend.Config, frontend.Config)
	valuesData, err := yaml.Marshal(values)
	if err != nil {
		return fmt.Errorf("encode shadow values: %w", err)
	}
	if err := os.WriteFile(o.valuesOut, valuesData, 0o600); err != nil {
		return fmt.Errorf("write shadow values: %w", err)
	}

	plan := migrationPlan{
		Namespace: o.namespace, BackendRelease: o.backendRelease, FrontendRelease: o.frontendRelease,
		TargetRelease: o.targetRelease, Chart: o.chart, Version: o.version, ValuesFile: o.valuesOut,
	}
	addFinding(&plan, "pass", "backend release", fmt.Sprintf("revision %d is %s", backend.Version, backend.Info.Status))
	addFinding(&plan, "pass", "frontend release", fmt.Sprintf("revision %d is %s", frontend.Version, frontend.Info.Status))
	if backend.Info.Status != "deployed" {
		addFinding(&plan, "block", "backend release", fmt.Sprintf("status is %q, expected deployed", backend.Info.Status))
	}
	if frontend.Info.Status != "deployed" {
		addFinding(&plan, "block", "frontend release", fmt.Sprintf("status is %q, expected deployed", frontend.Info.Status))
	}

	if _, _, targetErr := loadLatestHelmRelease(ctx, client, o.namespace, o.targetRelease); targetErr == nil {
		addFinding(&plan, "block", "target release", fmt.Sprintf("%s already exists", o.targetRelease))
	} else if !strings.Contains(targetErr.Error(), "was not found") {
		return targetErr
	} else {
		addFinding(&plan, "pass", "target release", fmt.Sprintf("%s is available", o.targetRelease))
	}

	inspectControlService(ctx, client, o, &plan)
	inspectSharedRBAC(ctx, client, o, &plan)
	if err := inspectSecretReferences(ctx, client, o.namespace, backend.Config, "backend", &plan); err != nil {
		return err
	}
	if err := inspectSecretReferences(ctx, client, o.namespace, frontend.Config, "frontend", &plan); err != nil {
		return err
	}
	if err := inspectRuntimeResources(ctx, client, o, &plan); err != nil {
		return err
	}

	plan.SecretFingerprint = referencedSecretsFingerprint(ctx, client, o.namespace, append(discoverSecretReferences(backend.Config), discoverSecretReferences(frontend.Config)...))
	plan.Ready = !hasBlockingFinding(plan.Findings)
	plan.Commands = migrationCommands(o, plan.Inventory.LegacySessions)
	if err := writeMigrationPlan(out, o.output, plan); err != nil {
		return err
	}
	if !plan.Ready {
		return errors.New("migration preflight found blocking problems")
	}
	return nil
}

func runHelmMigrateVerify(ctx context.Context, out io.Writer, client kubernetes.Interface, o *helmMigratePlanOptions, probe serviceProbe) error {
	target, _, err := loadLatestHelmRelease(ctx, client, o.namespace, o.targetRelease)
	if err != nil {
		return err
	}
	verify := migrationVerify{Namespace: o.namespace, TargetRelease: o.targetRelease, Phase: "shadow"}
	if target.Info.Status != "deployed" {
		addVerifyFinding(&verify, "block", "target release", fmt.Sprintf("status is %q, expected deployed", target.Info.Status))
	} else {
		addVerifyFinding(&verify, "pass", "target release", fmt.Sprintf("revision %d is deployed", target.Version))
	}

	backendService := o.targetRelease + "-backend"
	frontendService := o.targetRelease + "-frontend"
	inspectShadowWorkload(ctx, client, o.namespace, backendService, "backend", &verify)
	inspectShadowWorkload(ctx, client, o.namespace, frontendService, "frontend", &verify)
	if err := probe(ctx, backendService, "/health"); err != nil {
		addVerifyFinding(&verify, "block", "backend API probe", err.Error())
	} else {
		addVerifyFinding(&verify, "pass", "backend API probe", backendService+"/health returned successfully through the Kubernetes API proxy")
	}

	control, controlErr := client.CoreV1().Services(o.namespace).Get(ctx, "control", metav1.GetOptions{})
	oldSelector := map[string]string{"app.kubernetes.io/name": "agentapi-proxy", "app.kubernetes.io/instance": o.backendRelease, "app.kubernetes.io/component": "proxy"}
	targetSelector := map[string]string{"app.kubernetes.io/name": "backend", "app.kubernetes.io/instance": o.targetRelease, "app.kubernetes.io/component": "proxy"}
	if controlErr != nil {
		addVerifyFinding(&verify, "block", "control Service", controlErr.Error())
	} else if selectorContains(control.Spec.Selector, targetSelector) {
		verify.Phase = "cutover"
		addVerifyFinding(&verify, "pass", "control routing", "points to the target backend")
	} else if selectorContains(control.Spec.Selector, oldSelector) {
		addVerifyFinding(&verify, "pass", "control routing", "still points to the source backend (shadow phase)")
	} else {
		addVerifyFinding(&verify, "block", "control routing", "selector points to neither the source nor target backend: "+formatStringMap(control.Spec.Selector))
	}

	inspectWorkerLeaderElection(ctx, client, o, target.Config, &verify)
	if err := inspectVerifyRuntime(ctx, client, o, &verify); err != nil {
		return err
	}
	verify.OK = !hasBlockingFinding(verify.Findings)
	if err := writeMigrationVerify(out, o.output, verify); err != nil {
		return err
	}
	if !verify.OK {
		return errors.New("shadow migration verification failed")
	}
	return nil
}

func inspectShadowWorkload(ctx context.Context, client kubernetes.Interface, namespace, name, component string, verify *migrationVerify) {
	deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		addVerifyFinding(verify, "block", component+" Deployment", err.Error())
	} else {
		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		if desired == 0 || deployment.Status.ReadyReplicas < desired {
			addVerifyFinding(verify, "block", component+" Deployment", fmt.Sprintf("%d/%d replicas are Ready", deployment.Status.ReadyReplicas, desired))
		} else {
			addVerifyFinding(verify, "pass", component+" Deployment", fmt.Sprintf("%d/%d replicas are Ready", deployment.Status.ReadyReplicas, desired))
		}
	}
	endpointSlices, err := client.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{LabelSelector: "kubernetes.io/service-name=" + name})
	if err != nil {
		addVerifyFinding(verify, "block", component+" Service endpoints", err.Error())
		return
	}
	ready := 0
	for _, endpointSlice := range endpointSlices.Items {
		for _, endpoint := range endpointSlice.Endpoints {
			if endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready {
				ready += len(endpoint.Addresses)
			}
		}
	}
	if ready == 0 {
		addVerifyFinding(verify, "block", component+" Service endpoints", "no ready addresses")
	} else {
		addVerifyFinding(verify, "pass", component+" Service endpoints", fmt.Sprintf("%d ready address(es)", ready))
	}
}

var migrationWorkerLeases = []struct {
	valuePath string
	worker    string
	lease     string
	always    bool
}{
	{"scheduleWorker", "schedule", schedule.ScheduleWorkerLeaseName, false},
	{"slackbotCleanupWorker", "slackbot cleanup", schedule.SlackbotCleanupWorkerLeaseName, false},
	{"stockInventoryWorker", "stock inventory", schedule.StockInventoryWorkerLeaseName, false},
	{"", "session allocator", schedule.SessionAllocatorLeaseName, true},
}

func inspectWorkerLeaderElection(ctx context.Context, client kubernetes.Interface, o *helmMigratePlanOptions, targetValues map[string]any, verify *migrationVerify) {
	backendValues, _ := targetValues["backend"].(map[string]any)
	if backendValues == nil {
		backendValues = targetValues
	}
	for _, worker := range migrationWorkerLeases {
		config, _ := backendValues[worker.valuePath].(map[string]any)
		enabled := worker.always
		if configured, ok := config["enabled"].(bool); ok {
			enabled = configured
		}
		if !enabled {
			continue
		}
		lease, err := client.CoordinationV1().Leases(o.namespace).Get(ctx, worker.lease, metav1.GetOptions{})
		if err != nil {
			addVerifyFinding(verify, "block", worker.worker+" leader election", fmt.Sprintf("shared Lease/%s is missing: %v", worker.lease, err))
			continue
		}
		holder := ""
		if lease.Spec.HolderIdentity != nil {
			holder = *lease.Spec.HolderIdentity
		}
		verify.LeaderLeases = append(verify.LeaderLeases, migrationLeaderLease{Worker: worker.worker, Name: worker.lease, Holder: holder})
		if holder == "" {
			addVerifyFinding(verify, "warn", worker.worker+" leader election", fmt.Sprintf("Lease/%s exists but has no holder", worker.lease))
		} else {
			addVerifyFinding(verify, "pass", worker.worker+" leader election", fmt.Sprintf("uses shared Lease/%s; current holder is %s", worker.lease, holder))
		}
	}
	addVerifyFinding(verify, "pass", "leader-election contract", "worker Lease names are release-independent, so source and target contend for the same locks in this namespace")
}

func inspectVerifyRuntime(ctx context.Context, client kubernetes.Interface, o *helmMigratePlanOptions, verify *migrationVerify) error {
	pods, err := client.CoreV1().Pods(o.namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=agentapi-session"})
	if err != nil {
		return err
	}
	verify.Inventory.Sessions = len(pods.Items)
	for _, pod := range pods.Items {
		legacy := false
		for _, container := range pod.Spec.Containers {
			for _, env := range container.Env {
				if strings.Contains(env.Value, "http://"+o.backendRelease+".") {
					legacy = true
				}
			}
		}
		if legacy {
			verify.Inventory.LegacySessions++
		}
	}
	secrets, err := client.CoreV1().Secrets(o.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, secret := range secrets.Items {
		if isMigrationRuntimeSecret(secret) {
			verify.Inventory.RuntimeSecrets++
		}
	}
	pvcs, err := client.CoreV1().PersistentVolumeClaims(o.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, pvc := range pvcs.Items {
		if strings.HasPrefix(pvc.Name, "agentapi-session-") {
			verify.Inventory.SessionPVCs++
		}
	}
	if verify.Inventory.LegacySessions > 0 {
		addVerifyFinding(verify, "warn", "legacy sessions", fmt.Sprintf("%d session(s) still require Service/%s", verify.Inventory.LegacySessions, o.backendRelease))
	} else {
		addVerifyFinding(verify, "pass", "session callbacks", "all discovered session Pods use a release-independent callback")
	}
	return nil
}

func selectorContains(actual, want map[string]string) bool {
	for key, value := range want {
		if actual[key] != value {
			return false
		}
	}
	return true
}

func addVerifyFinding(verify *migrationVerify, level, subject, message string) {
	verify.Findings = append(verify.Findings, migrationFinding{Level: level, Subject: subject, Message: message})
}

func buildMigrationShadowValues(backend, frontend map[string]any) map[string]any {
	b := cloneMigrationMap(backend)
	f := cloneMigrationMap(frontend)
	delete(b, "fullnameOverride")
	delete(f, "fullnameOverride")
	setMigrationValue(b, false, "ingress", "enabled")
	setMigrationValue(b, false, "controlPlaneService", "create")
	setMigrationValue(b, "agentapi-proxy-session", "kubernetesSession", "serviceAccountName")
	setMigrationValue(b, false, "kubernetesSession", "rbac", "create")
	setMigrationValue(f, false, "ingress", "enabled")
	return map[string]any{"backend": b, "frontend": f}
}

func cloneMigrationMap(in map[string]any) map[string]any {
	out := map[string]any{}
	data, _ := json.Marshal(in)
	_ = json.Unmarshal(data, &out)
	return out
}

func setMigrationValue(root map[string]any, value any, path ...string) {
	current := root
	for _, key := range path[:len(path)-1] {
		next, ok := current[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[key] = next
		}
		current = next
	}
	current[path[len(path)-1]] = value
}

func inspectControlService(ctx context.Context, client kubernetes.Interface, o *helmMigratePlanOptions, plan *migrationPlan) {
	service, err := client.CoreV1().Services(o.namespace).Get(ctx, "control", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		addFinding(plan, "block", "control Service", "missing; upgrade the old backend chart before migration")
		return
	}
	if err != nil {
		addFinding(plan, "block", "control Service", err.Error())
		return
	}
	if service.Annotations["helm.sh/resource-policy"] != "keep" {
		addFinding(plan, "block", "control Service", "helm.sh/resource-policy=keep is missing")
	} else {
		addFinding(plan, "pass", "control Service", "retained on Helm uninstall")
	}
	if len(service.Spec.Selector) == 0 {
		addFinding(plan, "block", "control Service", "selector is empty")
	} else {
		addFinding(plan, "pass", "control selector", formatStringMap(service.Spec.Selector))
	}
}

func inspectSharedRBAC(ctx context.Context, client kubernetes.Interface, o *helmMigratePlanOptions, plan *migrationPlan) {
	if _, err := client.CoreV1().ServiceAccounts(o.namespace).Get(ctx, "agentapi-proxy-session", metav1.GetOptions{}); err != nil {
		addFinding(plan, "block", "shared session RBAC", "ServiceAccount/agentapi-proxy-session is missing")
		return
	}
	if _, err := client.RbacV1().Roles(o.namespace).Get(ctx, "agentapi-proxy-session", metav1.GetOptions{}); err != nil {
		addFinding(plan, "block", "shared session RBAC", "Role/agentapi-proxy-session is missing")
		return
	}
	if _, err := client.RbacV1().RoleBindings(o.namespace).Get(ctx, "agentapi-proxy-session", metav1.GetOptions{}); err != nil {
		addFinding(plan, "block", "shared session RBAC", "RoleBinding/agentapi-proxy-session is missing")
		return
	}
	addFinding(plan, "pass", "shared session RBAC", "fixed-name ServiceAccount, Role, and RoleBinding exist; shadow values reuse them")
	addFinding(plan, "warn", "backend uninstall", "the old release owns shared session RBAC; transfer or externalize it before uninstalling the backend")
}

func inspectSecretReferences(ctx context.Context, client kubernetes.Interface, namespace string, values map[string]any, prefix string, plan *migrationPlan) error {
	refs := discoverSecretReferences(values)
	if len(refs) == 0 {
		addFinding(plan, "pass", prefix+" Secret references", "none configured")
		return nil
	}
	failed := 0
	for _, ref := range refs {
		secret, err := client.CoreV1().Secrets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			failed++
			continue
		}
		for _, key := range ref.Keys {
			if len(secret.Data[key]) == 0 {
				failed++
			}
		}
	}
	if failed > 0 {
		addFinding(plan, "block", prefix+" Secret references", fmt.Sprintf("%d referenced Secret or key checks failed; run agentapi-proxy doctor for details", failed))
	} else {
		addFinding(plan, "pass", prefix+" Secret references", fmt.Sprintf("%d references resolve without exposing values", len(refs)))
	}
	return nil
}

func inspectRuntimeResources(ctx context.Context, client kubernetes.Interface, o *helmMigratePlanOptions, plan *migrationPlan) error {
	pods, err := client.CoreV1().Pods(o.namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=agentapi-session"})
	if err != nil {
		return err
	}
	plan.Inventory.Sessions = len(pods.Items)
	for _, pod := range pods.Items {
		legacy := false
		for _, container := range pod.Spec.Containers {
			for _, env := range container.Env {
				if strings.Contains(env.Value, "http://"+o.backendRelease+".") {
					legacy = true
				}
			}
		}
		if legacy {
			plan.Inventory.LegacySessions++
		}
	}
	if plan.Inventory.LegacySessions > 0 {
		addFinding(plan, "warn", "legacy sessions", fmt.Sprintf("%d of %d session Pods reference %s directly; drain them or retain a compatibility Service", plan.Inventory.LegacySessions, plan.Inventory.Sessions, o.backendRelease))
	} else {
		addFinding(plan, "pass", "session callbacks", fmt.Sprintf("no legacy callback found across %d session Pods", plan.Inventory.Sessions))
	}
	secrets, err := client.CoreV1().Secrets(o.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, s := range secrets.Items {
		if isMigrationRuntimeSecret(s) {
			plan.Inventory.RuntimeSecrets++
			inspectHelmOwnership(s.ObjectMeta, "Secret", &plan.Findings)
		}
	}
	pvcs, err := client.CoreV1().PersistentVolumeClaims(o.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, pvc := range pvcs.Items {
		if strings.HasPrefix(pvc.Name, "agentapi-session-") {
			plan.Inventory.SessionPVCs++
			inspectHelmOwnership(pvc.ObjectMeta, "PersistentVolumeClaim", &plan.Findings)
		}
	}
	addFinding(plan, "pass", "runtime inventory", fmt.Sprintf("%d runtime Secrets and %d session PVCs recorded", plan.Inventory.RuntimeSecrets, plan.Inventory.SessionPVCs))
	return nil
}

func inspectHelmOwnership(meta metav1.ObjectMeta, kind string, findings *[]migrationFinding) {
	if release := meta.Annotations["meta.helm.sh/release-name"]; release != "" {
		*findings = append(*findings, migrationFinding{Level: "block", Subject: kind + "/" + meta.Name, Message: "runtime resource is owned by Helm release " + release})
	}
}

func isMigrationRuntimeSecret(s corev1.Secret) bool {
	return strings.HasPrefix(s.Name, "agentapi-session-") || strings.HasPrefix(s.Name, "agentapi-settings-") || strings.HasPrefix(s.Name, "agentapi-agent-files-") || strings.HasPrefix(s.Name, "agentapi-user-files-") || s.Labels["agentapi.proxy/settings"] == "true"
}

func referencedSecretsFingerprint(ctx context.Context, client kubernetes.Interface, namespace string, refs []secretReference) string {
	unique := map[string]struct{}{}
	for _, ref := range refs {
		unique[ref.Name] = struct{}{}
	}
	names := make([]string, 0, len(unique))
	for name := range unique {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		secret, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			continue
		}
		_, _ = h.Write([]byte(name))
		keys := make([]string, 0, len(secret.Data))
		for key := range secret.Data {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			_, _ = h.Write([]byte{0})
			_, _ = h.Write([]byte(key))
			_, _ = h.Write([]byte{0})
			_, _ = h.Write(secret.Data[key])
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func migrationCommands(o *helmMigratePlanOptions, legacy int) []string {
	commands := []string{fmt.Sprintf("helm upgrade --install %s %s --namespace %s --version %s --values %s --wait", o.targetRelease, o.chart, o.namespace, o.version, o.valuesOut), fmt.Sprintf("agentapi-proxy helm migrate verify --namespace %s --backend-release %s --target-release %s", o.namespace, o.backendRelease, o.targetRelease), fmt.Sprintf("kubectl -n %s patch service control --type merge -p '{\"spec\":{\"selector\":{\"app.kubernetes.io/name\":\"backend\",\"app.kubernetes.io/instance\":\"%s\",\"app.kubernetes.io/component\":\"proxy\"}}}'", o.namespace, o.targetRelease)}
	if legacy > 0 {
		commands = append(commands, fmt.Sprintf("# Keep Service/%s until %d legacy session(s) are drained", o.backendRelease, legacy))
	}
	commands = append(commands, fmt.Sprintf("# Verify traffic, transfer shared session RBAC, then uninstall %s and %s", o.frontendRelease, o.backendRelease))
	return commands
}

func addFinding(plan *migrationPlan, level, subject, message string) {
	plan.Findings = append(plan.Findings, migrationFinding{Level: level, Subject: subject, Message: message})
}

func hasBlockingFinding(findings []migrationFinding) bool {
	for _, finding := range findings {
		if finding.Level == "block" {
			return true
		}
	}
	return false
}

func formatStringMap(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return strings.Join(parts, ",")
}

func writeMigrationPlan(w io.Writer, format string, plan migrationPlan) error {
	switch format {
	case "json":
		data, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	case "yaml":
		data, err := yaml.Marshal(plan)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	status := "READY"
	if !plan.Ready {
		status = "BLOCKED"
	}
	if _, err := fmt.Fprintf(w, "Migration preflight: %s\nNamespace: %s\nTarget: %s %s (%s)\nShadow values: %s\n", status, plan.Namespace, plan.Chart, plan.Version, plan.TargetRelease, plan.ValuesFile); err != nil {
		return err
	}
	for _, finding := range plan.Findings {
		if _, err := fmt.Fprintf(w, "[%s] %s: %s\n", strings.ToUpper(finding.Level), finding.Subject, finding.Message); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Inventory: sessions=%d legacy=%d runtime-secrets=%d session-pvcs=%d\n", plan.Inventory.Sessions, plan.Inventory.LegacySessions, plan.Inventory.RuntimeSecrets, plan.Inventory.SessionPVCs); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Suggested commands (not executed):"); err != nil {
		return err
	}
	for _, command := range plan.Commands {
		if _, err := fmt.Fprintln(w, "  "+command); err != nil {
			return err
		}
	}
	return nil
}

func writeMigrationVerify(w io.Writer, format string, verify migrationVerify) error {
	if format == "json" {
		data, err := json.MarshalIndent(verify, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	}
	if format == "yaml" {
		data, err := yaml.Marshal(verify)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	status := "PASS"
	if !verify.OK {
		status = "FAIL"
	}
	if _, err := fmt.Fprintf(w, "Shadow verification: %s\nNamespace: %s\nTarget: %s\nPhase: %s\n", status, verify.Namespace, verify.TargetRelease, verify.Phase); err != nil {
		return err
	}
	for _, finding := range verify.Findings {
		if _, err := fmt.Fprintf(w, "[%s] %s: %s\n", strings.ToUpper(finding.Level), finding.Subject, finding.Message); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Inventory: sessions=%d legacy=%d runtime-secrets=%d session-pvcs=%d\n", verify.Inventory.Sessions, verify.Inventory.LegacySessions, verify.Inventory.RuntimeSecrets, verify.Inventory.SessionPVCs); err != nil {
		return err
	}
	return nil
}
