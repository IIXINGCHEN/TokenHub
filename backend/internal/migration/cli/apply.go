package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"tokenhub/backend/internal/migration/bundle"
	migrationtokenhub "tokenhub/backend/internal/migration/sink/tokenhub"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(applyCmd)
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(verifyCmd)
	rootCmd.AddCommand(rollbackCmd)

	applyCmd.Flags().String("bundle", "", "Path to bundle JSON file")
	applyCmd.Flags().String("to", "", "TokenHub admin API base URL")
	applyCmd.Flags().String("token", "", "Admin API token")
	applyCmd.Flags().Bool("dry-run", false, "Perform a dry-run instead of writing")
	applyCmd.Flags().String("checkpoint-out", "", "Write the rollback checkpoint JSON to this path (default: <bundle>.checkpoint.json)")
	applyCmd.Flags().String("new-keys-out", "", "Write newly generated API key secrets JSON to this path (default: <bundle>.new-keys.json)")
	_ = applyCmd.MarkFlagRequired("bundle")

	planCmd.Flags().String("bundle", "", "Path to bundle JSON file")
	planCmd.Flags().String("to", "", "TokenHub admin API base URL")
	planCmd.Flags().String("token", "", "Admin API token")
	_ = planCmd.MarkFlagRequired("bundle")

	verifyCmd.Flags().String("bundle", "", "Path to bundle JSON file")
	verifyCmd.Flags().String("to", "", "TokenHub admin API base URL")
	verifyCmd.Flags().String("token", "", "Admin API token")
	_ = verifyCmd.MarkFlagRequired("bundle")

	rollbackCmd.Flags().String("checkpoint", "", "Path to checkpoint JSON file")
	rollbackCmd.Flags().String("to", "", "TokenHub admin API base URL")
	rollbackCmd.Flags().String("token", "", "Admin API token")
	_ = rollbackCmd.MarkFlagRequired("checkpoint")
}

func loadBundle(path string) (*bundle.CanonicalMigrationBundle, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return bundle.Unmarshal(payload)
}

func secretsResolver() bundle.SecretResolver {
	return bundle.EnvResolver{}
}

func resolveTarget(cmd *cobra.Command) (string, string) {
	baseURL, _ := cmd.Flags().GetString("to")
	token, _ := cmd.Flags().GetString("token")
	if strings.TrimSpace(baseURL) == "" {
		baseURL = strings.TrimSpace(os.Getenv("TOKENHUB_API"))
	}
	if strings.TrimSpace(token) == "" {
		token = strings.TrimSpace(os.Getenv("TOKENHUB_ADMIN_TOKEN"))
	}
	return baseURL, token
}

// requireRemoteTarget guards mutating commands: applying or rolling back
// against the implicit in-memory store would report success while writing
// nothing durable.
func requireRemoteTarget(action string, baseURL string) error {
	if strings.TrimSpace(baseURL) != "" {
		return nil
	}
	return errExit(ExitSinkRejected, fmt.Sprintf("%s requires a TokenHub target: pass --to or set TOKENHUB_API (refusing to %s against a transient in-memory store)", action, action))
}

// writeSecretFile persists payload with owner-only permissions, tightening
// the mode even when the file already exists.
func writeSecretFile(path string, payload []byte) error {
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// writeApplyArtifacts persists the rollback checkpoint and any one-time API
// key secrets returned by apply so they are not silently discarded.
func writeApplyArtifacts(cmd *cobra.Command, bundlePath string, result *migrationtokenhub.ApplyResult) error {
	checkpointPath, _ := cmd.Flags().GetString("checkpoint-out")
	if strings.TrimSpace(checkpointPath) == "" {
		checkpointPath = bundlePath + ".checkpoint.json"
	}
	checkpointPayload, err := json.MarshalIndent(result.Checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	if err := writeSecretFile(checkpointPath, checkpointPayload); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}
	fmt.Printf("  Checkpoint: %s\n", checkpointPath)
	if len(result.NewKeys) == 0 {
		return nil
	}
	newKeysPath, _ := cmd.Flags().GetString("new-keys-out")
	if strings.TrimSpace(newKeysPath) == "" {
		newKeysPath = bundlePath + ".new-keys.json"
	}
	newKeysPayload, err := json.MarshalIndent(map[string]any{"new_keys": result.NewKeys}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal new keys: %w", err)
	}
	if err := writeSecretFile(newKeysPath, newKeysPayload); err != nil {
		return fmt.Errorf("write new keys: %w", err)
	}
	fmt.Printf("  New API keys (%d, plaintext visible once): %s — distribute securely, then delete the file\n", len(result.NewKeys), newKeysPath)
	return nil
}

func newHTTPSink(baseURL string, token string) *migrationtokenhub.HTTPSink {
	client := migrationtokenhub.NewAdminAPIClient(baseURL, token, http.DefaultClient)
	return migrationtokenhub.NewHTTPSink(client, secretsResolver())
}

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Dry-run: show what apply would do",
	RunE: func(cmd *cobra.Command, args []string) error {
		bundlePath, _ := cmd.Flags().GetString("bundle")
		migrationBundle, err := loadBundle(bundlePath)
		if err != nil {
			return errExit(ExitSchemaMismatch, err.Error())
		}
		baseURL, token := resolveTarget(cmd)
		if err := requireRemoteTarget("plan", baseURL); err != nil {
			return err
		}
		report, err := newHTTPSink(baseURL, token).Plan(context.Background(), migrationBundle)
		if err != nil {
			return errExit(ExitSinkRejected, err.Error())
		}
		fmt.Printf("Plan:\n  Created: %d\n  Updated: %d\n  Skipped: %d\n", report.Created, report.Updated, report.Skipped)
		return nil
	},
}

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply a bundle to TokenHub",
	RunE: func(cmd *cobra.Command, args []string) error {
		bundlePath, _ := cmd.Flags().GetString("bundle")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		migrationBundle, err := loadBundle(bundlePath)
		if err != nil {
			return errExit(ExitSchemaMismatch, err.Error())
		}
		baseURL, token := resolveTarget(cmd)
		if err := requireRemoteTarget("apply", baseURL); err != nil {
			return err
		}
		if dryRun {
			report, err := newHTTPSink(baseURL, token).Plan(context.Background(), migrationBundle)
			if err != nil {
				return errExit(ExitSinkRejected, err.Error())
			}
			fmt.Printf("Dry-run plan:\n  Created: %d\n  Updated: %d\n  Skipped: %d\n", report.Created, report.Updated, report.Skipped)
			return nil
		}
		result, err := newHTTPSink(baseURL, token).Apply(context.Background(), migrationBundle)
		if err != nil {
			return errExit(ExitSinkRejected, err.Error())
		}
		fmt.Printf("Apply complete:\n  Created: %d\n  Updated: %d\n  Skipped: %d\n", result.Report.Created, result.Report.Updated, result.Report.Skipped)
		if err := writeApplyArtifacts(cmd, bundlePath, result); err != nil {
			return errExit(ExitSinkRejected, err.Error())
		}
		return nil
	},
}

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify bundle consistency",
	RunE: func(cmd *cobra.Command, args []string) error {
		bundlePath, _ := cmd.Flags().GetString("bundle")
		migrationBundle, err := loadBundle(bundlePath)
		if err != nil {
			return errExit(ExitSchemaMismatch, err.Error())
		}
		baseURL, token := resolveTarget(cmd)
		if err := requireRemoteTarget("verify", baseURL); err != nil {
			return err
		}
		result, err := newHTTPSink(baseURL, token).Verify(context.Background(), migrationBundle)
		if err != nil {
			return errExit(ExitSinkRejected, err.Error())
		}
		if result.OK {
			fmt.Println("Verify: PASS")
			return nil
		}
		fmt.Fprintf(os.Stderr, "Verify: FAIL (%d issues)\n", len(result.Issues))
		for _, issue := range result.Issues {
			fmt.Fprintf(os.Stderr, "  [%s] %s: %s\n", issue.Resource, issue.Ref, issue.Message)
		}
		return errExit(ExitVerifyMismatch, "verification mismatch")
	},
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Rollback from a checkpoint file",
	RunE: func(cmd *cobra.Command, args []string) error {
		checkpointPath, _ := cmd.Flags().GetString("checkpoint")
		payload, err := os.ReadFile(checkpointPath)
		if err != nil {
			return errExit(ExitSinkRejected, err.Error())
		}
		var checkpoint migrationtokenhub.Checkpoint
		if err := bundle.UnmarshalCheckpoint(payload, &checkpoint); err != nil {
			return errExit(ExitSchemaMismatch, err.Error())
		}
		baseURL, token := resolveTarget(cmd)
		if err := requireRemoteTarget("rollback", baseURL); err != nil {
			return err
		}
		result, err := newHTTPSink(baseURL, token).Rollback(context.Background(), checkpoint)
		if err != nil {
			return errExit(ExitSinkRejected, err.Error())
		}
		fmt.Printf("Rollback: %d changes reverted\n", len(result.Changes))
		return nil
	},
}
