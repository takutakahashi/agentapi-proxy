package cmd

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func migrateRoleSecrets(ctx context.Context, client kubernetes.Interface, values map[string]any, o *helmMigrateValuesOptions, stderr io.Writer) error {
	legacyEncryptionKey := stringValue(nestedValue(values, "config", "encryption", "key"))
	existingEncryptionKey, err := existingSecretKey(ctx, client, o.namespace, o.encryptionSecret, "encryption-key")
	if err != nil {
		return fmt.Errorf("read application encryption Secret: %w", err)
	}
	if len(existingEncryptionKey) > 0 && legacyEncryptionKey != "" && string(existingEncryptionKey) != legacyEncryptionKey {
		return fmt.Errorf("application encryption Secret %s/%s key %q conflicts with legacy config.encryption.key", o.namespace, o.encryptionSecret, "encryption-key")
	}
	if len(existingEncryptionKey) == 0 && legacyEncryptionKey == "" {
		legacyEncryptionKey, err = randomBase64(32)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(stderr, "Legacy encryption key was empty; generated a new encryption key."); err != nil {
			return fmt.Errorf("write encryption migration status: %w", err)
		}
	}
	if len(existingEncryptionKey) == 0 {
		existingEncryptionKey = []byte(legacyEncryptionKey)
	}
	legacyProvisionerToken, err := existingSecretKey(ctx, client, o.namespace, o.provisionerSecret, "token")
	if err != nil {
		return fmt.Errorf("read legacy provisioner Secret: %w", err)
	}
	existingProvisionerToken, err := existingSecretKey(ctx, client, o.namespace, o.provisionerSecret, "provisioner-token")
	if err != nil {
		return fmt.Errorf("read provisioner Secret: %w", err)
	}
	if len(legacyProvisionerToken) > 0 && len(existingProvisionerToken) > 0 && string(legacyProvisionerToken) != string(existingProvisionerToken) {
		return fmt.Errorf("provisioner Secret %s/%s keys %q and %q conflict", o.namespace, o.provisionerSecret, "token", "provisioner-token")
	}

	// Do not modify the cluster until all legacy/current consistency checks pass.
	token, err := randomHex(32)
	if err != nil {
		return err
	}
	if err := ensureSecretKey(ctx, client, o.namespace, o.workerControlSecret, "token", []byte(token)); err != nil {
		return fmt.Errorf("migrate worker control Secret: %w", err)
	}
	token, err = randomHex(32)
	if err != nil {
		return err
	}
	if err := ensureSecretKey(ctx, client, o.namespace, o.managerInternalSecret, "token", []byte(token)); err != nil {
		return fmt.Errorf("migrate session manager internal Secret: %w", err)
	}
	if err := ensureSecretKey(ctx, client, o.namespace, o.encryptionSecret, "encryption-key", existingEncryptionKey); err != nil {
		return fmt.Errorf("migrate application encryption Secret: %w", err)
	}

	provisionerToken := existingProvisionerToken
	if len(provisionerToken) == 0 {
		provisionerToken = legacyProvisionerToken
	}
	if len(provisionerToken) == 0 {
		generated, generateErr := randomHex(32)
		if generateErr != nil {
			return generateErr
		}
		provisionerToken = []byte(generated)
		if _, err := fmt.Fprintln(stderr, "Legacy provisioner token was absent; generated a new provisioner token."); err != nil {
			return fmt.Errorf("write provisioner migration status: %w", err)
		}
	}
	if err := ensureSecretKey(ctx, client, o.namespace, o.provisionerSecret, "provisioner-token", provisionerToken); err != nil {
		return fmt.Errorf("migrate provisioner Secret: %w", err)
	}
	return nil
}

func ensureSecretKey(ctx context.Context, client kubernetes.Interface, namespace, name, key string, value []byte) error {
	secrets := client.CoreV1().Secrets(namespace)
	secret, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = secrets.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{"app.kubernetes.io/managed-by": "ccplant-helm-migrate-values"}},
			Type:       corev1.SecretTypeOpaque,
			Data:       map[string][]byte{key: value},
		}, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if len(secret.Data[key]) > 0 {
		return nil
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data[key] = value
	_, err = secrets.Update(ctx, secret, metav1.UpdateOptions{})
	return err
}

func existingSecretKey(ctx context.Context, client kubernetes.Interface, namespace, name, key string) ([]byte, error) {
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return secret.Data[key], nil
}

func randomHex(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func randomBase64(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate encryption key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(value), nil
}
