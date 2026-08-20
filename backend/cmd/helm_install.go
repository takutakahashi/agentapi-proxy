package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilversion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/yaml"
)

const (
	defaultCCPlantChart     = "oci://ghcr.io/ccplant/charts/ccplant"
	defaultCookieSecretName = "agentapi-ui-encryption"
	defaultCookieSecretKey  = "cookie-encryption-secret"
	minimumHelmMajorVersion = 3
)

type helmInstallOptions struct {
	release, namespace, chart, version                     string
	hostname, apiHostname, ingressClass                    string
	cookieSecretName, cookieSecretKey                      string
	cookieSecretValue                                      string
	storageClass, persistenceSize, valuesOut               string
	values, sets                                           []string
	tls, persistence, createNamespace, dryRun, printValues bool
	timeout                                                time.Duration
}

const installConfigAPIVersion = "ccplant.io/v1alpha1"

type installConfig struct {
	APIVersion string                `json:"apiVersion"`
	Kind       string                `json:"kind"`
	Metadata   installConfigMetadata `json:"metadata"`
	Encryption installEncryption     `json:"encryption,omitempty"`
	Spec       installConfigSpec     `json:"spec"`
	Target     installTarget         `json:"target"`
}

type installConfigMetadata struct {
	Name string `json:"name"`
}

type installConfigSpec struct {
	Version     string             `json:"version"`
	Frontend    installEndpoint    `json:"frontend"`
	Backend     installEndpoint    `json:"backend"`
	TLS         bool               `json:"tls"`
	Persistence installPersistence `json:"persistence"`
	Secrets     installSecrets     `json:"secrets,omitempty"`
}

type installEncryption struct {
	Provider string `json:"provider,omitempty"`
}

type installSecrets struct {
	CookieEncryptionKey string `json:"cookieEncryptionKey,omitempty"`
}

type installEndpoint struct {
	Hostname string `json:"hostname"`
}

type installPersistence struct {
	Enabled bool   `json:"enabled"`
	Size    string `json:"size"`
}

type installTarget struct {
	Type       string                   `json:"type"`
	Kubernetes *installKubernetesTarget `json:"kubernetes,omitempty"`
	Compose    *installComposeTarget    `json:"compose,omitempty"`
}

type installKubernetesTarget struct {
	Namespace    string `json:"namespace"`
	IngressClass string `json:"ingressClass,omitempty"`
	StorageClass string `json:"storageClass,omitempty"`
	Chart        string `json:"chart,omitempty"`
	Version      string `json:"version,omitempty"`
}

type installComposeTarget struct {
	ProjectName  string `json:"projectName,omitempty"`
	Output       string `json:"output,omitempty"`
	FrontendPort int    `json:"frontendPort,omitempty"`
	BackendPort  int    `json:"backendPort,omitempty"`
}

type installAdapter interface {
	Apply(context.Context, io.Writer, io.Writer, *installConfig, installApplyOptions) error
}

type installApplyOptions struct {
	DryRun bool
	Runner installCommandRunner
	Client kubernetes.Interface
}

type installCommandRunner interface {
	Run(context.Context, io.Writer, io.Writer, string, ...string) error
}

type execInstallCommandRunner struct{}

func (execInstallCommandRunner) Run(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout, command.Stderr = stdout, stderr
	return command.Run()
}

type umbrellaChartMetadata struct {
	APIVersion   string `yaml:"apiVersion"`
	Name         string `yaml:"name"`
	Dependencies []struct {
		Name, Alias, Version, Condition string
	} `yaml:"dependencies"`
}

// ControlPlaneCmd manages the desired state of a ccplant control plane.
// Target-specific mechanisms such as Helm are implementation details.
var ControlPlaneCmd = newControlPlaneCommand()

func newControlPlaneCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "control-plane", Short: "Initialize, generate, and apply a ccplant control plane configuration",
		Args: cobra.NoArgs,
	}
	command.AddCommand(newControlPlaneInitCommand(), newControlPlaneGenerateCommand(), newControlPlaneEncryptCommand(), newControlPlaneEditCommand(), newControlPlaneApplyCommand())
	return command
}

func newControlPlaneGenerateCommand() *cobra.Command {
	var output, namespace, release, chart string
	command := &cobra.Command{Use: "generate", Aliases: []string{"export"}, Short: "Generate a control plane configuration from an existing Kubernetes release", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		restConfig, err := ctrl.GetConfig()
		if err != nil {
			return fmt.Errorf("get Kubernetes config: %w", err)
		}
		client, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			return fmt.Errorf("create Kubernetes client: %w", err)
		}
		cfg, err := generateKubernetesInstallConfig(cmd.Context(), client, execInstallCommandRunner{}, namespace, release, chart)
		if err != nil {
			return err
		}
		data, err := yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("encode control plane configuration: %w", err)
		}
		if output == "-" {
			_, err = cmd.OutOrStdout().Write(data)
			return err
		}
		if _, err := os.Stat(output); err == nil {
			return fmt.Errorf("configuration %q already exists", output)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check output configuration: %w", err)
		}
		if err := writeNewFileAtomically(output, data, 0o600); err != nil {
			return fmt.Errorf("write control plane configuration: %w", err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Generated %s from Kubernetes release %s/%s.\n", output, namespace, release)
		return err
	}}
	command.Flags().StringVarP(&output, "output", "o", "control-plane.yaml", "configuration output path, or - for stdout")
	command.Flags().StringVarP(&namespace, "namespace", "n", "ccplant", "Kubernetes namespace containing the Helm release")
	command.Flags().StringVar(&release, "release", "ccplant", "Helm release name")
	command.Flags().StringVar(&chart, "chart", defaultCCPlantChart, "chart reference to record in the configuration")
	return command
}

func generateKubernetesInstallConfig(ctx context.Context, client kubernetes.Interface, runner installCommandRunner, namespace, release, chart string) (*installConfig, error) {
	for label, value := range map[string]string{"namespace": namespace, "release": release, "chart": chart} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("--%s must not be empty", label)
		}
	}
	var output strings.Builder
	if err := runner.Run(ctx, &output, io.Discard, "helm", "get", "values", release, "--namespace", namespace, "--all", "--output", "yaml"); err != nil {
		return nil, fmt.Errorf("read Helm values for %s/%s: %w", namespace, release, err)
	}
	var values map[string]any
	if err := yaml.Unmarshal([]byte(output.String()), &values); err != nil {
		return nil, fmt.Errorf("decode Helm values for %s/%s: %w", namespace, release, err)
	}
	stringValue := func(path ...string) string {
		var current any = values
		for _, key := range path {
			object, ok := current.(map[string]any)
			if !ok {
				return ""
			}
			current = object[key]
		}
		value, _ := current.(string)
		return value
	}
	boolValue := func(path ...string) bool {
		var current any = values
		for _, key := range path {
			object, ok := current.(map[string]any)
			if !ok {
				return false
			}
			current = object[key]
		}
		value, _ := current.(bool)
		return value
	}
	hostname, apiHostname := stringValue("global", "hostname"), stringValue("global", "apiHostname")
	if hostname == "" || apiHostname == "" {
		return nil, fmt.Errorf("helm release %s/%s does not contain global.hostname and global.apiHostname", namespace, release)
	}
	secretName, secretKey := stringValue("frontend", "cookieEncryptionSecret", "secretName"), stringValue("frontend", "cookieEncryptionSecret", "secretKey")
	if secretName == "" {
		secretName = defaultCookieSecretName
	}
	if secretKey == "" {
		secretKey = defaultCookieSecretKey
	}
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read cookie Secret %s/%s: %w", namespace, secretName, err)
	}
	cookieKey := string(secret.Data[secretKey])
	if cookieKey == "" {
		return nil, fmt.Errorf("cookie Secret %s/%s does not contain non-empty key %q", namespace, secretName, secretKey)
	}
	version := strings.TrimPrefix(stringValue("backend", "image", "tag"), "v")
	if version == "" {
		version = strings.TrimPrefix(stringValue("frontend", "image", "tag"), "v")
	}
	if version == "" {
		version = "latest"
	}
	persistence := boolValue("backend", "kubernetesSession", "pvc", "enabled")
	size := stringValue("backend", "kubernetesSession", "pvc", "storageSize")
	if size == "" {
		size = "10Gi"
	}
	return &installConfig{
		APIVersion: installConfigAPIVersion, Kind: "ControlPlane", Metadata: installConfigMetadata{Name: release},
		Spec:   installConfigSpec{Version: version, Frontend: installEndpoint{Hostname: hostname}, Backend: installEndpoint{Hostname: apiHostname}, TLS: boolValue("global", "ingress", "tls", "enabled"), Persistence: installPersistence{Enabled: persistence, Size: size}, Secrets: installSecrets{CookieEncryptionKey: cookieKey}},
		Target: installTarget{Type: "kubernetes", Kubernetes: &installKubernetesTarget{Namespace: namespace, IngressClass: stringValue("global", "ingress", "className"), StorageClass: stringValue("backend", "kubernetesSession", "pvc", "storageClass"), Chart: chart}},
	}, nil
}

func newControlPlaneInitCommand() *cobra.Command {
	var output, target, version, encryption, ageRecipient string
	command := &cobra.Command{Use: "init", Short: "Write an editable control plane configuration", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := defaultInstallConfig(target)
		if err != nil {
			return err
		}
		cfg.Spec.Version = version
		data, err := yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("encode control plane configuration: %w", err)
		}
		if encryption == "sops" {
			if strings.TrimSpace(ageRecipient) == "" {
				return fmt.Errorf("--age-recipient is required with --encryption=sops")
			}
			cfg.Encryption.Provider = "sops"
			if cfg.Spec.Secrets.CookieEncryptionKey == "" {
				cfg.Spec.Secrets.CookieEncryptionKey, err = generateCookieEncryptionKey()
				if err != nil {
					return err
				}
			}
			data, err = yaml.Marshal(cfg)
			if err != nil {
				return fmt.Errorf("encode control plane configuration: %w", err)
			}
			data, err = encryptControlPlaneWithSOPS(cmd.Context(), data, ageRecipient)
			if err != nil {
				return err
			}
		} else if encryption != "none" {
			return fmt.Errorf("unsupported encryption %q (supported: none, sops)", encryption)
		}
		if output == "-" {
			_, err = cmd.OutOrStdout().Write(data)
			return err
		}
		if _, err := os.Stat(output); err == nil {
			return fmt.Errorf("configuration %q already exists", output)
		}
		if err := os.WriteFile(output, data, 0o600); err != nil {
			return fmt.Errorf("write control plane configuration: %w", err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s. Edit it, then run: agentapi-proxy control-plane apply --file %s\n", output, output)
		return err
	}}
	command.Flags().StringVarP(&output, "output", "o", "control-plane.yaml", "configuration output path, or - for stdout")
	command.Flags().StringVar(&target, "target", "kubernetes", "deployment target: kubernetes or compose")
	command.Flags().StringVar(&version, "version", "latest", "ccplant release version shared by all components")
	command.Flags().StringVar(&encryption, "encryption", "none", "configuration encryption: none or sops")
	command.Flags().StringVar(&ageRecipient, "age-recipient", "", "age recipient used by SOPS encryption")
	return command
}

func newControlPlaneEditCommand() *cobra.Command {
	var file, ageKeyFile string
	command := &cobra.Command{Use: "edit", Short: "Edit a SOPS-encrypted control plane configuration", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if _, err := os.Stat(file); err != nil {
			return fmt.Errorf("read control plane configuration: %w", err)
		}
		process := exec.CommandContext(cmd.Context(), "sops", file)
		process.Stdin, process.Stdout, process.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
		process.Env = sopsEnvironment(ageKeyFile)
		if err := process.Run(); err != nil {
			return fmt.Errorf("edit SOPS configuration: %w", err)
		}
		return nil
	}}
	command.Flags().StringVarP(&file, "file", "f", "control-plane.yaml", "control plane configuration")
	command.Flags().StringVar(&ageKeyFile, "sops-age-key-file", "", "age identity file for SOPS (otherwise SOPS_AGE_KEY_FILE or the SOPS default is used)")
	return command
}

func newControlPlaneEncryptCommand() *cobra.Command {
	var file, output, ageRecipient string
	command := &cobra.Command{Use: "encrypt", Short: "Encrypt an existing control plane configuration with SOPS", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if strings.TrimSpace(ageRecipient) == "" {
			return fmt.Errorf("--age-recipient is required")
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read control plane configuration: %w", err)
		}
		if hasSOPSMetadata(data) {
			return fmt.Errorf("configuration %q is already SOPS-encrypted", file)
		}
		var cfg installConfig
		if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
			return fmt.Errorf("decode control plane configuration: %w", err)
		}
		if err := validateInstallConfig(&cfg); err != nil {
			return err
		}
		cfg.Encryption.Provider = "sops"
		if cfg.Spec.Secrets.CookieEncryptionKey == "" {
			cfg.Spec.Secrets.CookieEncryptionKey, err = generateCookieEncryptionKey()
			if err != nil {
				return err
			}
		}
		data, err = yaml.Marshal(&cfg)
		if err != nil {
			return fmt.Errorf("encode control plane configuration: %w", err)
		}
		encrypted, err := encryptControlPlaneWithSOPS(cmd.Context(), data, ageRecipient)
		if err != nil {
			return err
		}
		if output == "" {
			output = encryptedControlPlanePath(file)
		}
		if samePath(file, output) {
			return fmt.Errorf("encrypted output must differ from input %q", file)
		}
		if _, err := os.Stat(output); err == nil {
			return fmt.Errorf("encrypted output %q already exists", output)
		}
		if err := writeNewFileAtomically(output, encrypted, 0o600); err != nil {
			return fmt.Errorf("write encrypted control plane configuration: %w", err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Encrypted %s with SOPS.\n", output)
		return err
	}}
	command.Flags().StringVarP(&file, "file", "f", "control-plane.yaml", "plain control plane configuration")
	command.Flags().StringVarP(&output, "output", "o", "", "encrypted output path (default: <input>.sops.yaml)")
	command.Flags().StringVar(&ageRecipient, "age-recipient", "", "age recipient used by SOPS encryption")
	return command
}

func encryptedControlPlanePath(path string) string {
	extension := filepath.Ext(path)
	if extension == ".yaml" || extension == ".yml" {
		return strings.TrimSuffix(path, extension) + ".sops" + extension
	}
	return path + ".sops.yaml"
}

func samePath(left, right string) bool {
	leftAbsolute, leftErr := filepath.Abs(left)
	rightAbsolute, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && leftAbsolute == rightAbsolute
}

func writeNewFileAtomically(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".control-plane-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	// Publish without overwriting an output that appeared concurrently.
	if err := os.Link(temporaryPath, path); err != nil {
		return err
	}
	return os.Remove(temporaryPath)
}

func defaultInstallConfig(target string) (*installConfig, error) {
	cookieEncryptionKey, err := generateCookieEncryptionKey()
	if err != nil {
		return nil, err
	}
	cfg := &installConfig{APIVersion: installConfigAPIVersion, Kind: "ControlPlane", Metadata: installConfigMetadata{Name: "ccplant"}, Spec: installConfigSpec{Version: "latest", Frontend: installEndpoint{Hostname: "agentapi.local"}, Backend: installEndpoint{Hostname: "api.agentapi.local"}, Persistence: installPersistence{Enabled: true, Size: "10Gi"}, Secrets: installSecrets{CookieEncryptionKey: cookieEncryptionKey}}, Target: installTarget{Type: target}}
	switch target {
	case "kubernetes":
		cfg.Target.Kubernetes = &installKubernetesTarget{Namespace: "ccplant", Chart: defaultCCPlantChart}
	case "compose":
		cfg.Spec.Frontend.Hostname, cfg.Spec.Backend.Hostname = "localhost", "localhost"
		cfg.Target.Compose = &installComposeTarget{ProjectName: "ccplant", Output: "compose.generated.yaml", FrontendPort: 3000, BackendPort: 8080}
	default:
		return nil, fmt.Errorf("unsupported target %q (supported: kubernetes, compose)", target)
	}
	return cfg, nil
}

func newControlPlaneApplyCommand() *cobra.Command {
	var file, ageKeyFile string
	var dryRun bool
	command := &cobra.Command{Use: "apply", Short: "Validate and apply a control plane configuration", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadInstallConfig(cmd.Context(), file, ageKeyFile)
		if err != nil {
			return err
		}
		adapter, err := adapterForInstallTarget(cfg.Target.Type)
		if err != nil {
			return err
		}
		options := installApplyOptions{DryRun: dryRun, Runner: execInstallCommandRunner{}}
		if cfg.Target.Type == "kubernetes" {
			restConfig, configErr := ctrl.GetConfig()
			if configErr != nil {
				return fmt.Errorf("get Kubernetes config: %w", configErr)
			}
			options.Client, configErr = kubernetes.NewForConfig(restConfig)
			if configErr != nil {
				return fmt.Errorf("create Kubernetes client: %w", configErr)
			}
		}
		return adapter.Apply(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), cfg, options)
	}}
	command.Flags().StringVarP(&file, "file", "f", "control-plane.yaml", "control plane configuration")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "validate and render without applying")
	command.Flags().StringVar(&ageKeyFile, "sops-age-key-file", "", "age identity file for SOPS (otherwise SOPS_AGE_KEY_FILE or the SOPS default is used)")
	return command
}

func loadInstallConfig(ctx context.Context, path, ageKeyFile string) (*installConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read control plane configuration: %w", err)
	}
	if hasSOPSMetadata(data) {
		data, err = decryptControlPlaneWithSOPS(ctx, path, ageKeyFile)
		if err != nil {
			return nil, err
		}
	}
	var cfg installConfig
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode control plane configuration: %w", err)
	}
	if err := validateInstallConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func hasSOPSMetadata(data []byte) bool {
	var document map[string]any
	if yaml.Unmarshal(data, &document) != nil {
		return false
	}
	_, ok := document["sops"]
	return ok
}

func encryptControlPlaneWithSOPS(ctx context.Context, data []byte, recipient string) ([]byte, error) {
	command := exec.CommandContext(ctx, "sops", "encrypt", "--input-type", "yaml", "--output-type", "yaml", "--encrypted-regex", "^secrets$", "--age", recipient, "/dev/stdin")
	command.Stdin = strings.NewReader(string(data))
	var stdout, stderr strings.Builder
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("encrypt control plane configuration with SOPS: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return []byte(stdout.String()), nil
}

func decryptControlPlaneWithSOPS(ctx context.Context, path, ageKeyFile string) ([]byte, error) {
	command := exec.CommandContext(ctx, "sops", "decrypt", "--output-type", "yaml", path)
	command.Env = sopsEnvironment(ageKeyFile)
	var stdout, stderr strings.Builder
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("decrypt control plane configuration with SOPS: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return []byte(stdout.String()), nil
}

func sopsEnvironment(ageKeyFile string) []string {
	environment := os.Environ()
	if ageKeyFile != "" {
		environment = append(environment, "SOPS_AGE_KEY_FILE="+ageKeyFile)
	}
	return environment
}

func generateCookieEncryptionKey() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate cookie encryption key: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func validateInstallConfig(cfg *installConfig) error {
	if cfg.APIVersion != installConfigAPIVersion || cfg.Kind != "ControlPlane" {
		return fmt.Errorf("expected apiVersion %q and kind ControlPlane", installConfigAPIVersion)
	}
	if cfg.Metadata.Name == "" || cfg.Spec.Frontend.Hostname == "" || cfg.Spec.Backend.Hostname == "" {
		return fmt.Errorf("metadata.name and spec frontend/backend hostnames are required")
	}
	if strings.TrimSpace(cfg.Spec.Version) == "" {
		return fmt.Errorf("spec.version is required")
	}
	if cfg.Spec.Persistence.Enabled && cfg.Spec.Persistence.Size == "" {
		return fmt.Errorf("spec.persistence.size is required when persistence is enabled")
	}
	_, err := adapterForInstallTarget(cfg.Target.Type)
	return err
}

func adapterForInstallTarget(target string) (installAdapter, error) {
	switch target {
	case "kubernetes":
		return kubernetesInstallAdapter{}, nil
	case "compose":
		return composeInstallAdapter{}, nil
	default:
		return nil, fmt.Errorf("unsupported target %q", target)
	}
}

type kubernetesInstallAdapter struct{}

func (kubernetesInstallAdapter) Apply(ctx context.Context, out, errOut io.Writer, cfg *installConfig, apply installApplyOptions) error {
	target := cfg.Target.Kubernetes
	if target == nil {
		return fmt.Errorf("target.kubernetes is required for kubernetes")
	}
	o := &helmInstallOptions{
		release: cfg.Metadata.Name, namespace: target.Namespace, chart: target.Chart, version: chartVersion(cfg.Spec.Version, target.Version),
		hostname: cfg.Spec.Frontend.Hostname, apiHostname: cfg.Spec.Backend.Hostname,
		ingressClass: target.IngressClass, storageClass: target.StorageClass,
		persistenceSize:  cfg.Spec.Persistence.Size,
		cookieSecretName: defaultCookieSecretName, cookieSecretKey: defaultCookieSecretKey,
		cookieSecretValue: cfg.Spec.Secrets.CookieEncryptionKey,
		tls:               cfg.Spec.TLS, persistence: cfg.Spec.Persistence.Enabled, createNamespace: true,
		dryRun: apply.DryRun, timeout: 10 * time.Minute,
	}
	if o.namespace == "" {
		o.namespace = "ccplant"
	}
	if o.chart == "" {
		o.chart = defaultCCPlantChart
	}
	return runHelmInstall(ctx, out, errOut, apply.Client, apply.Runner, o)
}

type composeInstallAdapter struct{}

func (composeInstallAdapter) Apply(ctx context.Context, out, errOut io.Writer, cfg *installConfig, apply installApplyOptions) error {
	target := cfg.Target.Compose
	if target == nil {
		return fmt.Errorf("target.compose is required for compose")
	}
	if target.ProjectName == "" {
		target.ProjectName = cfg.Metadata.Name
	}
	if target.Output == "" {
		target.Output = "compose.generated.yaml"
	}
	if target.FrontendPort == 0 {
		target.FrontendPort = 3000
	}
	if target.BackendPort == 0 {
		target.BackendPort = 8080
	}
	manifest, err := generateComposeManifest(cfg, target)
	if err != nil {
		return err
	}
	path, err := filepath.Abs(target.Output)
	if err != nil {
		return fmt.Errorf("resolve compose output: %w", err)
	}
	if err := os.WriteFile(path, manifest, 0o600); err != nil {
		return fmt.Errorf("write compose manifest: %w", err)
	}
	if _, err := fmt.Fprintf(out, "Rendered Compose manifest: %s\n", path); err != nil {
		return err
	}
	args := []string{"compose", "--project-name", target.ProjectName, "--file", path, "config", "--quiet"}
	if err := apply.Runner.Run(ctx, io.Discard, errOut, "docker", args...); err != nil {
		return fmt.Errorf("validate Compose manifest: %w", err)
	}
	if apply.DryRun {
		return nil
	}
	args[len(args)-2], args[len(args)-1] = "up", "--detach"
	if err := apply.Runner.Run(ctx, out, errOut, "docker", args...); err != nil {
		return fmt.Errorf("apply Compose installation: %w", err)
	}
	return nil
}

func generateComposeManifest(cfg *installConfig, target *installComposeTarget) ([]byte, error) {
	scheme := "http"
	if cfg.Spec.TLS {
		return nil, fmt.Errorf("compose target does not provide TLS termination; set spec.tls=false or use an external proxy")
	}
	backendURL := fmt.Sprintf("%s://backend:8080", scheme)
	publicURL := fmt.Sprintf("%s://%s:%d", scheme, cfg.Spec.Frontend.Hostname, target.FrontendPort)
	imageTag := imageVersion(cfg.Spec.Version)
	services := map[string]any{
		"backend": map[string]any{
			"image":       "ghcr.io/ccplant/ccplant-backend:" + imageTag,
			"environment": map[string]string{"AGENTAPI_AUTH_STATIC_ENABLED": "false", "AGENTAPI_AUTH_GITHUB_ENABLED": "false"},
			"ports":       []string{fmt.Sprintf("%d:8080", target.BackendPort)},
		},
		"frontend": map[string]any{
			"image":       "ghcr.io/ccplant/ccplant-frontend:" + imageTag,
			"environment": map[string]string{"AGENTAPI_PROXY_URL": backendURL, "NEXT_PUBLIC_BASE_URL": publicURL, "COOKIE_ENCRYPTION_SECRET": cfg.Spec.Secrets.CookieEncryptionKey},
			"depends_on":  []string{"backend"}, "ports": []string{fmt.Sprintf("%d:3000", target.FrontendPort)},
		},
	}
	manifest := map[string]any{"services": services}
	if cfg.Spec.Persistence.Enabled {
		services["backend"].(map[string]any)["volumes"] = []string{"ccplant-data:/data"}
		manifest["volumes"] = map[string]any{"ccplant-data": map[string]any{}}
	}
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("generate Compose manifest: %w", err)
	}
	return data, nil
}

func imageVersion(version string) string {
	if version == "latest" || strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

func chartVersion(version, targetOverride string) string {
	if targetOverride != "" {
		return strings.TrimPrefix(targetOverride, "v")
	}
	if version == "latest" {
		return ""
	}
	return strings.TrimPrefix(version, "v")
}

func runHelmInstall(ctx context.Context, out, errOut io.Writer, client kubernetes.Interface, runner installCommandRunner, o *helmInstallOptions) error {
	if err := validateHelmInstallOptions(o); err != nil {
		return err
	}
	kubeVersion, err := client.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("kubernetes compatibility check failed: %w", err)
	}
	semanticVersion, err := utilversion.ParseSemantic(kubeVersion.GitVersion)
	if err != nil {
		return fmt.Errorf("kubernetes compatibility check failed: parse server version %q: %w", kubeVersion.GitVersion, err)
	}
	if semanticVersion.LessThan(utilversion.MustParseSemantic("v1.19.0")) {
		return fmt.Errorf("kubernetes compatibility check failed: Kubernetes >=1.19 is required, got %s", kubeVersion.GitVersion)
	}
	if err := checkHelmVersion(ctx, runner); err != nil {
		return err
	}
	metadata, err := loadUmbrellaChartMetadata(ctx, runner, o)
	if err != nil {
		return err
	}
	if err := validateUmbrellaChart(metadata); err != nil {
		return fmt.Errorf("chart compatibility check failed: %w", err)
	}
	if err := resolveClusterDefaults(ctx, client, o); err != nil {
		return err
	}

	upgrade, err := helmReleaseExists(ctx, client, o.namespace, o.release)
	if err != nil {
		return err
	}
	verb := "install"
	if upgrade {
		verb = "upgrade"
	}
	if _, err := fmt.Fprintf(out, "Preflight passed: Helm v%d, chart %s with backend/frontend, operation=%s\n", minimumHelmMajorVersion, metadata.Name, verb); err != nil {
		return err
	}

	generatedValues, err := generateInstallValues(o)
	if err != nil {
		return err
	}
	if o.printValues {
		if _, err := fmt.Fprintf(out, "--- # generated installer values\n%s", generatedValues); err != nil {
			return fmt.Errorf("print generated values: %w", err)
		}
	}
	generatedPath, cleanup, err := materializeInstallValues(generatedValues, o.valuesOut)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := validateGeneratedRelease(ctx, runner, o, generatedPath); err != nil {
		return err
	}

	if !o.dryRun {
		if err := ensureInstallNamespace(ctx, client, o.namespace, o.createNamespace); err != nil {
			return err
		}
		if err := ensureCookieSecret(ctx, client, o.namespace, o.cookieSecretName, o.cookieSecretKey, o.cookieSecretValue); err != nil {
			return err
		}
	}
	args := helmInstallArgs(o, upgrade, generatedPath)
	if err := runner.Run(ctx, out, errOut, "helm", args...); err != nil {
		return fmt.Errorf("helm %s failed: %w", verb, err)
	}
	_, err = fmt.Fprintf(out, "ccplant %s completed for %s/%s.\n", verb, o.namespace, o.release)
	return err
}

func validateGeneratedRelease(ctx context.Context, runner installCommandRunner, o *helmInstallOptions, generatedValuesPath string) error {
	args := []string{"template", o.release, o.chart, "--namespace", o.namespace, "--values", generatedValuesPath}
	if o.version != "" {
		args = append(args, "--version", o.version)
	}
	for _, file := range o.values {
		args = append(args, "--values", file)
	}
	for _, value := range o.sets {
		args = append(args, "--set", value)
	}
	if err := runner.Run(ctx, io.Discard, io.Discard, "helm", args...); err != nil {
		return fmt.Errorf("generated values compatibility check failed: %w", err)
	}
	return nil
}

func validateHelmInstallOptions(o *helmInstallOptions) error {
	for label, value := range map[string]string{"release": o.release, "namespace": o.namespace, "chart": o.chart, "hostname": o.hostname, "api-hostname": o.apiHostname, "cookie-secret-name": o.cookieSecretName, "cookie-secret-key": o.cookieSecretKey} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("--%s must not be empty", label)
		}
	}
	if o.timeout <= 0 {
		return fmt.Errorf("--timeout must be positive")
	}
	return nil
}

func resolveClusterDefaults(ctx context.Context, client kubernetes.Interface, o *helmInstallOptions) error {
	if o.ingressClass == "" {
		classes, err := client.NetworkingV1().IngressClasses().List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("detect IngressClass: %w", err)
		}
		for _, class := range classes.Items {
			if class.Annotations["ingressclass.kubernetes.io/is-default-class"] == "true" {
				o.ingressClass = class.Name
				break
			}
		}
		if o.ingressClass == "" && len(classes.Items) == 1 {
			o.ingressClass = classes.Items[0].Name
		}
		if o.ingressClass == "" {
			return fmt.Errorf("no default IngressClass was detected; specify --ingress-class")
		}
	}
	if o.persistence && o.storageClass == "" {
		classes, err := client.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("detect StorageClass: %w", err)
		}
		for _, class := range classes.Items {
			if class.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" || class.Annotations["storageclass.beta.kubernetes.io/is-default-class"] == "true" {
				o.storageClass = class.Name
				break
			}
		}
		if o.storageClass == "" && len(classes.Items) == 1 {
			o.storageClass = classes.Items[0].Name
		}
		if o.storageClass == "" {
			return fmt.Errorf("persistence is enabled but no default StorageClass was detected; specify --storage-class or --persistence=false")
		}
	}
	return nil
}

func generateInstallValues(o *helmInstallOptions) ([]byte, error) {
	scheme := "http"
	if o.tls {
		scheme = "https"
	}
	values := map[string]any{
		"global": map[string]any{
			"hostname": o.hostname, "apiHostname": o.apiHostname,
			"ingress": map[string]any{"className": o.ingressClass, "tls": map[string]any{"enabled": o.tls}},
		},
		"backend": map[string]any{
			"enabled":           true,
			"ingress":           map[string]any{"enabled": true, "className": o.ingressClass},
			"kubernetesSession": map[string]any{"enabled": true, "pvc": map[string]any{"enabled": o.persistence, "storageClass": o.storageClass, "storageSize": o.persistenceSize}},
		},
		"frontend": map[string]any{
			"enabled":                true,
			"ingress":                map[string]any{"enabled": true, "className": o.ingressClass},
			"cookieEncryptionSecret": map[string]any{"enabled": true, "secretName": o.cookieSecretName, "secretKey": o.cookieSecretKey},
			"config":                 map[string]any{"publicUrl": scheme + "://" + o.hostname},
		},
	}
	if o.version != "" {
		tag := imageVersion(o.version)
		values["backend"].(map[string]any)["image"] = map[string]any{"tag": tag}
		values["frontend"].(map[string]any)["image"] = map[string]any{"tag": tag}
	}
	if o.tls {
		values["backend"].(map[string]any)["ingress"].(map[string]any)["tls"] = []any{map[string]any{"secretName": o.release + "-backend-tls", "hosts": []string{o.apiHostname}}}
		values["frontend"].(map[string]any)["ingress"].(map[string]any)["tls"] = []any{map[string]any{"secretName": o.release + "-frontend-tls", "hosts": []string{o.hostname}}}
	}
	data, err := yaml.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("generate installer values: %w", err)
	}
	return data, nil
}

func materializeInstallValues(values []byte, outputPath string) (string, func(), error) {
	if outputPath != "" {
		path, err := filepath.Abs(outputPath)
		if err != nil {
			return "", func() {}, fmt.Errorf("resolve --values-out: %w", err)
		}
		if err := os.WriteFile(path, values, 0o600); err != nil {
			return "", func() {}, fmt.Errorf("write generated values: %w", err)
		}
		return path, func() {}, nil
	}
	file, err := os.CreateTemp("", "ccplant-install-values-*.yaml")
	if err != nil {
		return "", func() {}, fmt.Errorf("create generated values file: %w", err)
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("protect generated values file: %w", err)
	}
	if _, err := file.Write(values); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("write generated values file: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("close generated values file: %w", err)
	}
	return path, cleanup, nil
}

func checkHelmVersion(ctx context.Context, runner installCommandRunner) error {
	var output strings.Builder
	if err := runner.Run(ctx, &output, io.Discard, "helm", "version", "--template", "{{.Version}}"); err != nil {
		return fmt.Errorf("helm compatibility check failed: %w", err)
	}
	version := strings.TrimSpace(strings.TrimPrefix(output.String(), "v"))
	var major int
	if _, err := fmt.Sscanf(version, "%d.", &major); err != nil || major < minimumHelmMajorVersion {
		return fmt.Errorf("helm compatibility check failed: Helm >=%d is required, got %q", minimumHelmMajorVersion, output.String())
	}
	return nil
}

func loadUmbrellaChartMetadata(ctx context.Context, runner installCommandRunner, o *helmInstallOptions) (*umbrellaChartMetadata, error) {
	args := []string{"show", "chart", o.chart}
	if o.version != "" {
		args = append(args, "--version", o.version)
	}
	var output strings.Builder
	if err := runner.Run(ctx, &output, io.Discard, "helm", args...); err != nil {
		return nil, fmt.Errorf("load chart metadata: %w", err)
	}
	var metadata umbrellaChartMetadata
	if err := yaml.Unmarshal([]byte(output.String()), &metadata); err != nil {
		return nil, fmt.Errorf("decode chart metadata: %w", err)
	}
	return &metadata, nil
}

func validateUmbrellaChart(metadata *umbrellaChartMetadata) error {
	if metadata.APIVersion != "v2" || metadata.Name != "ccplant" {
		return fmt.Errorf("expected Helm v2 application chart named ccplant, got apiVersion=%q name=%q", metadata.APIVersion, metadata.Name)
	}
	found := map[string]bool{}
	for _, dependency := range metadata.Dependencies {
		alias := dependency.Alias
		if alias == "" {
			alias = dependency.Name
		}
		if alias == "backend" || alias == "frontend" {
			if dependency.Version == "" || dependency.Condition != alias+".enabled" {
				return fmt.Errorf("%s dependency has incompatible version or condition", alias)
			}
			found[alias] = true
		}
	}
	if !found["backend"] || !found["frontend"] {
		return fmt.Errorf("chart must contain enabled backend and frontend dependencies")
	}
	return nil
}

func helmReleaseExists(ctx context.Context, client kubernetes.Interface, namespace, release string) (bool, error) {
	secrets, err := client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{LabelSelector: fmt.Sprintf("owner=helm,name=%s", release)})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("detect existing Helm release: %w", err)
	}
	return len(secrets.Items) > 0, nil
}

func ensureCookieSecret(ctx context.Context, client kubernetes.Interface, namespace, name, key, configuredValue string) error {
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if len(secret.Data[key]) == 0 {
			return fmt.Errorf("existing cookie Secret %s/%s does not contain non-empty key %q", namespace, name, key)
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("read cookie Secret %s/%s: %w", namespace, name, err)
	}
	value := configuredValue
	if value == "" {
		value, err = generateCookieEncryptionKey()
		if err != nil {
			return err
		}
	}
	_, err = client.CoreV1().Secrets(namespace).Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}, Type: corev1.SecretTypeOpaque, StringData: map[string]string{key: value}}, metav1.CreateOptions{})
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("namespace %q does not exist; create it first or let Helm create it before rerunning", namespace)
	}
	if err != nil {
		return fmt.Errorf("create cookie Secret %s/%s: %w", namespace, name, err)
	}
	return nil
}

func ensureInstallNamespace(ctx context.Context, client kubernetes.Interface, namespace string, create bool) error {
	_, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("read namespace %q: %w", namespace, err)
	}
	if !create {
		return fmt.Errorf("namespace %q does not exist (use --create-namespace)", namespace)
	}
	if _, err := client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %q: %w", namespace, err)
	}
	return nil
}

func helmInstallArgs(o *helmInstallOptions, upgrade bool, generatedValuesPath string) []string {
	verb := "install"
	if upgrade {
		verb = "upgrade"
	}
	args := []string{verb, o.release, o.chart, "--namespace", o.namespace, "--atomic", "--wait", "--timeout", o.timeout.String()}
	if o.version != "" {
		args = append(args, "--version", o.version)
	}
	if !upgrade && o.createNamespace {
		args = append(args, "--create-namespace")
	}
	args = append(args, "--values", generatedValuesPath)
	for _, file := range o.values {
		args = append(args, "--values", file)
	}
	for _, value := range o.sets {
		args = append(args, "--set", value)
	}
	if o.dryRun {
		args = append(args, "--dry-run")
	}
	return args
}
