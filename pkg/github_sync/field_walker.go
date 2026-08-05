package githubsync

import (
	"fmt"
	"reflect"

	importexport "github.com/takutakahashi/agentapi-proxy/pkg/import"
)

const gitsyncTag = "gitsync"

// encryptTaggedFields recursively encrypts all fields in v that are tagged with
// gitsync:"encrypt" (string) or gitsync:"encrypt-values" (map[string]string).
// Fields tagged gitsync:"companion" (*EncryptedSecretData metadata) are skipped;
// call clearCompanionFields separately to nil them out after encryption.
func encryptTaggedFields(v interface{}, dek []byte) error {
	return walkGitsyncFields(reflect.ValueOf(v), dek, true)
}

// decryptTaggedFields recursively decrypts all enc:v1: values in fields tagged
// with gitsync:"encrypt" or gitsync:"encrypt-values".
func decryptTaggedFields(v interface{}, dek []byte) error {
	return walkGitsyncFields(reflect.ValueOf(v), dek, false)
}

// clearCompanionFields nils out all *EncryptedSecretData companion fields in r.
// These fields carry import-path metadata and are not written to git YAML.
func clearCompanionFields(r *importexport.TeamResources) {
	for i := range r.Schedules {
		sc := &r.Schedules[i].SessionConfig
		sc.EnvironmentEncrypted = nil
		if sc.Params != nil {
			sc.Params.GitHubTokenEncrypted = nil
		}
	}
	for i := range r.Webhooks {
		w := &r.Webhooks[i]
		w.SecretEncrypted = nil
		if w.SessionConfig != nil {
			w.SessionConfig.EnvironmentEncrypted = nil
			if w.SessionConfig.Params != nil {
				w.SessionConfig.Params.GitHubTokenEncrypted = nil
			}
		}
		for j := range w.Triggers {
			if sc := w.Triggers[j].SessionConfig; sc != nil {
				sc.EnvironmentEncrypted = nil
				if sc.Params != nil {
					sc.Params.GitHubTokenEncrypted = nil
				}
			}
		}
	}
	if s := r.Settings; s != nil {
		s.ClaudeCodeOAuthTokenEncrypted = nil
		s.EnvVarsEncrypted = nil
		if b := s.Bedrock; b != nil {
			b.AccessKeyIDEncrypted = nil
			b.SecretAccessKeyEncrypted = nil
		}
		for _, mcp := range s.MCPServers {
			mcp.EnvEncrypted = nil
			mcp.HeadersEncrypted = nil
		}
	}
	for i := range r.SessionProfiles {
		sp := &r.SessionProfiles[i]
		sp.Config.EnvironmentEncrypted = nil
		if sp.Config.Params != nil {
			sp.Config.Params.GitHubTokenEncrypted = nil
		}
	}
}

// walkGitsyncFields recursively walks v and encrypts or decrypts fields based on
// their gitsync struct tag.
//
// Tag values:
//   - "encrypt"       — string field: encrypt or decrypt the value in-place
//   - "encrypt-values" — map[string]string field: encrypt or decrypt every map value
//   - "companion"      — *EncryptedSecretData metadata field: skip (handled by clearCompanionFields)
//   - ""               — recurse into structs, pointers, slices, and maps of structs
//
// Note: gitsync:"encrypt" on string fields inside map-value structs (e.g. a string
// field of a struct stored in map[string]*T) is not supported; use gitsync:"encrypt-values"
// on map[string]string fields instead.
func walkGitsyncFields(v reflect.Value, dek []byte, encrypting bool) error {
	// Dereference pointers and interfaces.
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			ft := t.Field(i)
			if !ft.IsExported() {
				continue
			}
			fv := v.Field(i)
			switch ft.Tag.Get(gitsyncTag) {
			case "encrypt":
				if fv.Kind() == reflect.String {
					if err := processEncryptString(fv, dek, encrypting, ft.Name); err != nil {
						return err
					}
				}
			case "encrypt-values":
				if fv.Kind() == reflect.Map {
					if err := processEncryptMapValues(fv, dek, encrypting, ft.Name); err != nil {
						return err
					}
				}
			case "companion":
				// Handled by clearCompanionFields; skip here.
			default:
				if err := walkGitsyncFields(fv, dek, encrypting); err != nil {
					return err
				}
			}
		}

	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if err := walkGitsyncFields(v.Index(i), dek, encrypting); err != nil {
				return err
			}
		}

	case reflect.Map:
		// Recurse into map values that are pointers to structs (e.g. map[string]*MCPServerImport).
		// Map values obtained via MapIndex are not addressable in reflect, so gitsync:"encrypt"
		// string fields inside such structs cannot be set. Use gitsync:"encrypt-values" on
		// map[string]string fields within those structs instead.
		for _, key := range v.MapKeys() {
			if err := walkGitsyncFields(v.MapIndex(key), dek, encrypting); err != nil {
				return err
			}
		}
	}

	return nil
}

func processEncryptString(fv reflect.Value, dek []byte, encrypting bool, name string) error {
	s := fv.String()
	if s == "" {
		return nil
	}
	if encrypting {
		if IsEncrypted(s) {
			return nil
		}
		enc, err := EncryptField(dek, s)
		if err != nil {
			return fmt.Errorf("encrypt %s: %w", name, err)
		}
		fv.SetString(enc)
	} else {
		if !IsEncrypted(s) {
			return nil
		}
		plain, err := DecryptField(dek, s)
		if err != nil {
			return fmt.Errorf("decrypt %s: %w", name, err)
		}
		fv.SetString(plain)
	}
	return nil
}

func processEncryptMapValues(fv reflect.Value, dek []byte, encrypting bool, name string) error {
	if fv.IsNil() {
		return nil
	}
	for _, key := range fv.MapKeys() {
		elem := fv.MapIndex(key)
		if elem.Kind() != reflect.String {
			continue
		}
		s := elem.String()
		if encrypting {
			if IsEncrypted(s) {
				continue
			}
			enc, err := EncryptField(dek, s)
			if err != nil {
				return fmt.Errorf("encrypt %s[%s]: %w", name, key.String(), err)
			}
			fv.SetMapIndex(key, reflect.ValueOf(enc))
		} else {
			if !IsEncrypted(s) {
				continue
			}
			plain, err := DecryptField(dek, s)
			if err != nil {
				return fmt.Errorf("decrypt %s[%s]: %w", name, key.String(), err)
			}
			fv.SetMapIndex(key, reflect.ValueOf(plain))
		}
	}
	return nil
}
