package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"tokenhub/backend/internal/migration/bundle"
	migrationtokenhub "tokenhub/backend/internal/migration/sink/tokenhub"
	"tokenhub/backend/internal/server"

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

func newStoreSink() *migrationtokenhub.StoreSink {
	return migrationtokenhub.NewStoreSink(server.NewMemoryStore(), secretsResolver())
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
		var report *migrationtokenhub.MigrationReport
		if strings.TrimSpace(baseURL) != "" {
			report, err = newHTTPSink(baseURL, token).Plan(context.Background(), migrationBundle)
		} else {
			report, err = newStoreSink().Plan(migrationBundle)
		}
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
		if dryRun {
			var report *migrationtokenhub.MigrationReport
			if strings.TrimSpace(baseURL) != "" {
				report, err = newHTTPSink(baseURL, token).Plan(context.Background(), migrationBundle)
			} else {
				report, err = newStoreSink().Plan(migrationBundle)
			}
			if err != nil {
				return errExit(ExitSinkRejected, err.Error())
			}
			fmt.Printf("Dry-run plan:\n  Created: %d\n  Updated: %d\n  Skipped: %d\n", report.Created, report.Updated, report.Skipped)
			return nil
		}
		if strings.TrimSpace(baseURL) != "" {
			result, err := newHTTPSink(baseURL, token).Apply(context.Background(), migrationBundle)
			if err != nil {
				return errExit(ExitSinkRejected, err.Error())
			}
			fmt.Printf("Apply complete:\n  Created: %d\n  Updated: %d\n  Skipped: %d\n", result.Report.Created, result.Report.Updated, result.Report.Skipped)
			return nil
		}
		result, err := newStoreSink().Apply(migrationBundle)
		if err != nil {
			return errExit(ExitSinkRejected, err.Error())
		}
		fmt.Printf("Apply complete:\n  Created: %d\n  Updated: %d\n  Skipped: %d\n", result.Report.Created, result.Report.Updated, result.Report.Skipped)
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
		var result *migrationtokenhub.VerifyResult
		if strings.TrimSpace(baseURL) != "" {
			result, err = newHTTPSink(baseURL, token).Verify(context.Background(), migrationBundle)
		} else {
			result, err = newStoreSink().Verify(migrationBundle)
		}
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
		if strings.TrimSpace(baseURL) != "" {
			result, err := newHTTPSink(baseURL, token).Rollback(context.Background(), checkpoint)
			if err != nil {
				return errExit(ExitSinkRejected, err.Error())
			}
			fmt.Printf("Rollback: %d changes reverted\n", len(result.Changes))
			return nil
		}
		result, err := newStoreSink().Rollback(checkpoint)
		if err != nil {
			return errExit(ExitSinkRejected, err.Error())
		}
		fmt.Printf("Rollback: %d changes reverted\n", len(result.Changes))
		return nil
	},
}
