package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

type kvStoreVerifyOptions struct {
	namespace            string
	primaryBackend       string
	primaryDatabaseURL   string
	primaryAuthToken     string
	secondaryBackend     string
	secondaryDatabaseURL string
	secondaryAuthToken   string
	output               string
}

type kvStoreVerificationEntry struct {
	Kind      kvstore.Kind `json:"kind"`
	Namespace string       `json:"namespace"`
	Key       string       `json:"key"`
	Status    string       `json:"status"`
}

type kvStoreVerificationResult struct {
	Matched          int                        `json:"matched"`
	MissingPrimary   int                        `json:"missing_primary"`
	MissingSecondary int                        `json:"missing_secondary"`
	Different        int                        `json:"different"`
	Entries          []kvStoreVerificationEntry `json:"entries"`
}

func (r kvStoreVerificationResult) mismatchCount() int {
	return r.MissingPrimary + r.MissingSecondary + r.Different
}

func newKVStoreVerifyCommand() *cobra.Command {
	o := &kvStoreVerifyOptions{}
	command := &cobra.Command{
		Use:   "verify",
		Short: "Verify that primary and secondary application KV data match",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if o.output != "text" && o.output != "json" {
				return fmt.Errorf("unsupported output %q (must be text or json)", o.output)
			}
			primaryConfig, secondaryConfig, err := o.storeConfigs()
			if err != nil {
				return err
			}
			primary, secondary, err := buildMigrationStores(cmd.Context(), primaryConfig, secondaryConfig)
			if err != nil {
				return err
			}
			defer func() { _ = errors.Join(primary.Close(), secondary.Close()) }()

			result, verifyErr := verifyKVStores(cmd.Context(), primary, secondary, o.namespace)
			if err := writeKVStoreVerificationResult(cmd.OutOrStdout(), result, o.output); err != nil {
				return err
			}
			return verifyErr
		},
	}
	flags := command.Flags()
	flags.StringVarP(&o.namespace, "namespace", "n", resolveKubernetesNamespace(), "namespace containing application KV data")
	flags.StringVar(&o.primaryBackend, "primary-backend", os.Getenv("AGENTAPI_KV_STORE_PRIMARY_BACKEND"), "primary backend (kubernetes or libsql)")
	flags.StringVar(&o.primaryDatabaseURL, "primary-database-url", os.Getenv("AGENTAPI_KV_STORE_PRIMARY_DATABASE_URL"), "primary libSQL database URL")
	flags.StringVar(&o.primaryAuthToken, "primary-auth-token", os.Getenv("AGENTAPI_KV_STORE_PRIMARY_AUTH_TOKEN"), "primary libSQL authentication token")
	flags.StringVar(&o.secondaryBackend, "secondary-backend", os.Getenv("AGENTAPI_KV_STORE_SECONDARY_BACKEND"), "secondary backend (kubernetes or libsql)")
	flags.StringVar(&o.secondaryDatabaseURL, "secondary-database-url", os.Getenv("AGENTAPI_KV_STORE_SECONDARY_DATABASE_URL"), "secondary libSQL database URL")
	flags.StringVar(&o.secondaryAuthToken, "secondary-auth-token", os.Getenv("AGENTAPI_KV_STORE_SECONDARY_AUTH_TOKEN"), "secondary libSQL authentication token")
	flags.StringVarP(&o.output, "output", "o", "text", "output format: text or json")
	return command
}

func (o kvStoreVerifyOptions) storeConfigs() (migrationStoreConfig, migrationStoreConfig, error) {
	if o.primaryBackend == "" || o.secondaryBackend == "" {
		return migrationStoreConfig{}, migrationStoreConfig{}, errors.New("primary and secondary backends are required (configure AGENTAPI_KV_STORE_PRIMARY_BACKEND and AGENTAPI_KV_STORE_SECONDARY_BACKEND)")
	}
	return migrationStoreConfig{backend: o.primaryBackend, databaseURL: o.primaryDatabaseURL, authToken: o.primaryAuthToken}, migrationStoreConfig{backend: o.secondaryBackend, databaseURL: o.secondaryDatabaseURL, authToken: o.secondaryAuthToken}, nil
}

func verifyKVStores(ctx context.Context, primary, secondary kvstore.Store, namespace string) (kvStoreVerificationResult, error) {
	result := kvStoreVerificationResult{Entries: []kvStoreVerificationEntry{}}
	primaryRecords, err := collectApplicationKVStoreRecords(ctx, primary, namespace)
	if err != nil {
		return result, fmt.Errorf("collect primary records: %w", err)
	}
	secondaryRecords, err := collectApplicationKVStoreRecords(ctx, secondary, namespace)
	if err != nil {
		return result, fmt.Errorf("collect secondary records: %w", err)
	}

	primaryByKey := indexKVRecords(primaryRecords)
	secondaryByKey := indexKVRecords(secondaryRecords)
	identities := make(map[string]kvstore.Record, len(primaryRecords)+len(secondaryRecords))
	for identity, record := range primaryByKey {
		identities[identity] = record
	}
	for identity, record := range secondaryByKey {
		identities[identity] = record
	}
	keys := make([]string, 0, len(identities))
	for identity := range identities {
		keys = append(keys, identity)
	}
	sort.Strings(keys)

	for _, identity := range keys {
		record := identities[identity]
		entry := kvStoreVerificationEntry{Kind: record.Kind, Namespace: record.Namespace, Key: record.Key}
		primaryRecord, inPrimary := primaryByKey[identity]
		secondaryRecord, inSecondary := secondaryByKey[identity]
		switch {
		case !inPrimary:
			entry.Status = "missing-primary"
			result.MissingPrimary++
		case !inSecondary:
			entry.Status = "missing-secondary"
			result.MissingSecondary++
		case !bytes.Equal(primaryRecord.Value, secondaryRecord.Value):
			entry.Status = "different"
			result.Different++
		default:
			entry.Status = "matched"
			result.Matched++
		}
		result.Entries = append(result.Entries, entry)
	}
	if mismatches := result.mismatchCount(); mismatches > 0 {
		return result, fmt.Errorf("KV verification found %d mismatched record(s)", mismatches)
	}
	return result, nil
}

func indexKVRecords(records []kvstore.Record) map[string]kvstore.Record {
	result := make(map[string]kvstore.Record, len(records))
	for _, record := range records {
		result[string(record.Kind)+"\x00"+record.Namespace+"\x00"+record.Key] = record
	}
	return result
}

func writeKVStoreVerificationResult(w io.Writer, result kvStoreVerificationResult, output string) error {
	if output == "json" {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	for _, entry := range result.Entries {
		if _, err := fmt.Fprintf(w, "%-18s %s/%s\n", entry.Status, entry.Kind, entry.Key); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "verification: matched=%d missing-primary=%d missing-secondary=%d different=%d\n",
		result.Matched, result.MissingPrimary, result.MissingSecondary, result.Different)
	return err
}
