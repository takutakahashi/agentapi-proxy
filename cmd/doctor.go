package cmd

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client/config"
)

var (
	doctorNamespace = "default"
	doctorReleases  = []string{"agentapi-proxy", "agentapi-ui"}
)

// DoctorCmd validates the active Helm release and every Kubernetes Secret it references.
var DoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Validate a Helm release and its referenced Kubernetes Secrets",
	Long: `Inspect the latest Helm release revision in a namespace, discover Secret
references in its user-supplied values, and verify that every referenced Secret,
key, and value exists. Secret values are never printed.`,
	Args: cobra.NoArgs,
	RunE: runDoctor,
}

type helmStoredRelease struct {
	Name    string          `json:"name"`
	Version int             `json:"version"`
	Info    helmReleaseInfo `json:"info"`
	Config  map[string]any  `json:"config"`
}

type helmReleaseInfo struct {
	Status string `json:"status"`
}

type secretReference struct {
	Path     string
	Name     string
	Keys     []string
	Required bool
}

type doctorFinding struct {
	OK      bool
	Warning bool
	Subject string
	Message string
}

func init() {
	DoctorCmd.Flags().StringVarP(&doctorNamespace, "namespace", "n", "default", "Kubernetes namespace containing the Helm release")
	DoctorCmd.Flags().StringSliceVar(&doctorReleases, "release", []string{"agentapi-proxy", "agentapi-ui"}, "Helm release name (may be specified multiple times)")
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("get Kubernetes config: %w", err)
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}

	failed := false
	if len(doctorReleases) == 0 {
		return fmt.Errorf("at least one Helm release must be specified")
	}
	for _, releaseName := range doctorReleases {
		releaseName = strings.TrimSpace(releaseName)
		if releaseName == "" {
			return fmt.Errorf("helm release name must not be empty")
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "== %s ==\n", releaseName); err != nil {
			return fmt.Errorf("write doctor output: %w", err)
		}
		findings, err := diagnoseHelmRelease(cmd.Context(), client, doctorNamespace, releaseName)
		if err != nil {
			return err
		}
		for _, finding := range findings {
			marker := "PASS"
			if finding.Warning {
				marker = "WARN"
			} else if !finding.OK {
				marker = "FAIL"
				failed = true
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s: %s\n", marker, finding.Subject, finding.Message); err != nil {
				return fmt.Errorf("write doctor output: %w", err)
			}
		}
	}
	if failed {
		return fmt.Errorf("doctor found one or more problems")
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "All checks passed."); err != nil {
		return fmt.Errorf("write doctor output: %w", err)
	}
	return nil
}

func diagnoseHelmRelease(ctx context.Context, client kubernetes.Interface, namespace, releaseName string) ([]doctorFinding, error) {
	stored, storageSecretName, err := loadLatestHelmRelease(ctx, client, namespace, releaseName)
	if err != nil {
		return nil, err
	}

	findings := []doctorFinding{{
		OK:      true,
		Subject: "helm release",
		Message: fmt.Sprintf("%s revision %d (%s) loaded from %s", stored.Name, stored.Version, stored.Info.Status, storageSecretName),
	}}
	if stored.Info.Status != "deployed" {
		findings = append(findings, doctorFinding{OK: false, Subject: "helm status", Message: fmt.Sprintf("expected deployed, got %q", stored.Info.Status)})
	} else {
		findings = append(findings, doctorFinding{OK: true, Subject: "helm status", Message: "deployed"})
	}
	findings = append(findings, inspectSensitiveLiterals(stored.Config)...)
	findings = append(findings, inspectOptionalWorkload(ctx, client, namespace, releaseName)...)

	references := discoverSecretReferences(stored.Config)
	if len(references) == 0 {
		findings = append(findings, doctorFinding{OK: true, Subject: "secret references", Message: "none configured"})
		return findings, nil
	}

	for _, ref := range references {
		secret, getErr := client.CoreV1().Secrets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			findings = append(findings, secretReferenceFailure(ref, fmt.Sprintf("Secret %q does not exist", ref.Name)))
			continue
		}
		if getErr != nil {
			findings = append(findings, secretReferenceFailure(ref, fmt.Sprintf("read Secret %q: %v", ref.Name, getErr)))
			continue
		}
		findings = append(findings, validateSecretReference(ref, secret)...)
	}

	return findings, nil
}

func loadLatestHelmRelease(ctx context.Context, client kubernetes.Interface, namespace, releaseName string) (*helmStoredRelease, string, error) {
	secrets, err := client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("owner=helm,name=%s", releaseName),
	})
	if err != nil {
		return nil, "", fmt.Errorf("list Helm release Secrets: %w", err)
	}
	if len(secrets.Items) == 0 {
		return nil, "", fmt.Errorf("helm release %q was not found in namespace %q", releaseName, namespace)
	}

	sort.SliceStable(secrets.Items, func(i, j int) bool {
		return helmRevision(secrets.Items[i]) > helmRevision(secrets.Items[j])
	})
	latest := secrets.Items[0]
	stored, err := decodeHelmRelease(latest.Data["release"])
	if err != nil {
		return nil, "", fmt.Errorf("decode Helm release Secret %q: %w", latest.Name, err)
	}
	return stored, latest.Name, nil
}

func helmRevision(secret corev1.Secret) int {
	revision, err := strconv.Atoi(secret.Labels["version"])
	if err != nil {
		return 0
	}
	return revision
}

func decodeHelmRelease(encoded []byte) (*helmStoredRelease, error) {
	if len(encoded) == 0 {
		return nil, fmt.Errorf("release payload is empty")
	}
	compressed := make([]byte, base64.StdEncoding.DecodedLen(len(encoded)))
	n, err := base64.StdEncoding.Decode(compressed, encoded)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed[:n]))
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	payload, err := io.ReadAll(reader)
	if err != nil {
		_ = reader.Close()
		return nil, fmt.Errorf("decompress: %w", err)
	}
	if err := reader.Close(); err != nil {
		return nil, fmt.Errorf("close gzip reader: %w", err)
	}
	var stored helmStoredRelease
	if err := json.Unmarshal(payload, &stored); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if stored.Config == nil {
		stored.Config = map[string]any{}
	}
	return &stored, nil
}

func discoverSecretReferences(values map[string]any) []secretReference {
	refs := map[string]secretReference{}
	var walk func(any, string)
	walk = func(value any, path string) {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				childPath := joinPath(path, key)
				switch {
				case key == "secretName" || key == "existingSecret":
					if name, ok := nonEmptyString(child); ok {
						addSecretReference(refs, secretReference{
							Path:     childPath,
							Name:     name,
							Keys:     siblingSecretKeys(typed),
							Required: requiredSecretReference(childPath, typed),
						})
					}
				case isSecretRefKey(key):
					if refMap, ok := child.(map[string]any); ok {
						if name, ok := nonEmptyString(refMap["name"]); ok {
							addSecretReference(refs, secretReference{Path: childPath, Name: name, Keys: siblingSecretKeys(refMap)})
						}
					}
				case strings.HasSuffix(strings.ToLower(key), "secretnames"):
					if names, ok := child.([]any); ok {
						for index, item := range names {
							if name, ok := nonEmptyString(item); ok {
								addSecretReference(refs, secretReference{Path: fmt.Sprintf("%s[%d]", childPath, index), Name: name})
							}
						}
					}
				}
				walk(child, childPath)
			}
		case []any:
			for index, child := range typed {
				walk(child, fmt.Sprintf("%s[%d]", path, index))
			}
		}
	}
	walk(values, "values")

	result := make([]secretReference, 0, len(refs))
	for _, ref := range refs {
		result = append(result, ref)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

func isSecretRefKey(key string) bool {
	lower := strings.ToLower(key)
	return lower == "secretref" || lower == "clientsecretref" || lower == "tokenref" || lower == "privatekeyref" || lower == "passwordref"
}

func siblingSecretKeys(values map[string]any) []string {
	keys := map[string]struct{}{}
	for key, value := range values {
		lower := strings.ToLower(key)
		if lower == "key" || lower == "secretkey" || strings.HasSuffix(lower, "secretkey") || strings.HasSuffix(lower, "passwordkey") {
			if secretKey, ok := nonEmptyString(value); ok {
				keys[secretKey] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func addSecretReference(refs map[string]secretReference, ref secretReference) {
	identity := ref.Path + "\x00" + ref.Name + "\x00" + strings.Join(ref.Keys, "\x00") + "\x00" + strconv.FormatBool(ref.Required)
	refs[identity] = ref
}

func requiredSecretReference(path string, parent map[string]any) bool {
	if !strings.HasSuffix(path, "cookieEncryptionSecret.secretName") {
		return false
	}
	enabled, _ := parent["enabled"].(bool)
	return enabled
}

func validateSecretReference(ref secretReference, secret *corev1.Secret) []doctorFinding {
	if len(ref.Keys) == 0 {
		for _, value := range secret.Data {
			if len(value) > 0 {
				return []doctorFinding{{OK: true, Subject: ref.Path, Message: fmt.Sprintf("Secret %q exists and contains data", ref.Name)}}
			}
		}
		return []doctorFinding{secretReferenceFailure(ref, fmt.Sprintf("Secret %q exists but contains no non-empty data", ref.Name))}
	}

	findings := make([]doctorFinding, 0, len(ref.Keys))
	for _, key := range ref.Keys {
		value, exists := secret.Data[key]
		switch {
		case !exists:
			findings = append(findings, secretReferenceFailure(ref, fmt.Sprintf("Secret %q is missing key %q", ref.Name, key)))
		case len(value) == 0:
			findings = append(findings, secretReferenceFailure(ref, fmt.Sprintf("Secret %q key %q is empty", ref.Name, key)))
		default:
			findings = append(findings, doctorFinding{OK: true, Subject: ref.Path, Message: fmt.Sprintf("Secret %q key %q exists and is non-empty", ref.Name, key)})
		}
	}
	return findings
}

func secretReferenceFailure(ref secretReference, message string) doctorFinding {
	return doctorFinding{OK: false, Warning: !ref.Required, Subject: ref.Path, Message: message}
}

func inspectSensitiveLiterals(values map[string]any) []doctorFinding {
	paths := make([]string, 0)
	var walk func(any, string)
	walk = func(value any, path string) {
		switch typed := value.(type) {
		case map[string]any:
			if name, ok := typed["name"].(string); ok && sensitiveEnvironmentName(name) {
				if literal, ok := nonEmptyString(typed["value"]); ok && literal != "" {
					paths = append(paths, joinPath(path, "value"))
				}
			}
			for key, child := range typed {
				childPath := joinPath(path, key)
				if isSensitiveLiteralKey(key) {
					if literal, ok := nonEmptyString(child); ok && literal != "" {
						paths = append(paths, childPath)
					}
				}
				walk(child, childPath)
			}
		case []any:
			for index, child := range typed {
				walk(child, fmt.Sprintf("%s[%d]", path, index))
			}
		}
	}
	walk(values, "values")
	sort.Strings(paths)
	if len(paths) == 0 {
		return []doctorFinding{{OK: true, Subject: "optional plaintext secrets", Message: "none detected"}}
	}
	findings := make([]doctorFinding, 0, len(paths))
	for _, path := range paths {
		findings = append(findings, doctorFinding{
			Warning: true,
			Subject: "optional plaintext secrets",
			Message: fmt.Sprintf("%s contains a sensitive-looking literal; prefer a Secret reference", path),
		})
	}
	return findings
}

func isSensitiveLiteralKey(key string) bool {
	switch strings.ToLower(key) {
	case "clientsecret", "token", "password", "privatekey", "encryptionkey", "apikey":
		return true
	default:
		return false
	}
}

func sensitiveEnvironmentName(name string) bool {
	upper := strings.ToUpper(name)
	return strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "PRIVATE_KEY")
}

func inspectOptionalWorkload(ctx context.Context, client kubernetes.Interface, namespace, releaseName string) []doctorFinding {
	findings := make([]doctorFinding, 0, 2)
	deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, releaseName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		findings = append(findings, doctorFinding{OK: false, Subject: "deployment", Message: fmt.Sprintf("Deployment %q was not found", releaseName)})
	case err != nil:
		findings = append(findings, doctorFinding{OK: false, Subject: "deployment", Message: fmt.Sprintf("read Deployment %q: %v", releaseName, err)})
	default:
		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		if deployment.Status.ReadyReplicas != desired {
			findings = append(findings, doctorFinding{OK: false, Subject: "deployment", Message: fmt.Sprintf("Deployment %q has %d/%d ready replicas", releaseName, deployment.Status.ReadyReplicas, desired)})
		} else {
			findings = append(findings, doctorFinding{OK: true, Subject: "deployment", Message: fmt.Sprintf("Deployment %q has %d/%d ready replicas", releaseName, deployment.Status.ReadyReplicas, desired)})
		}
	}

	endpoints, err := client.CoreV1().Endpoints(namespace).Get(ctx, releaseName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		findings = append(findings, doctorFinding{Warning: true, Subject: "optional service endpoints", Message: fmt.Sprintf("Endpoints %q were not found", releaseName)})
	case err != nil:
		findings = append(findings, doctorFinding{Warning: true, Subject: "optional service endpoints", Message: fmt.Sprintf("read Endpoints %q: %v", releaseName, err)})
	default:
		ready := 0
		for _, subset := range endpoints.Subsets {
			ready += len(subset.Addresses)
		}
		if ready == 0 {
			findings = append(findings, doctorFinding{Warning: true, Subject: "optional service endpoints", Message: fmt.Sprintf("Endpoints %q have no ready addresses", releaseName)})
		} else {
			findings = append(findings, doctorFinding{OK: true, Subject: "optional service endpoints", Message: fmt.Sprintf("Endpoints %q have %d ready addresses", releaseName, ready)})
		}
	}
	return findings
}

func nonEmptyString(value any) (string, bool) {
	text, ok := value.(string)
	return text, ok && text != ""
}

func joinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}
