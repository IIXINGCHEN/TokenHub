package cli

import (
	"fmt"
	"os"

	"tokenhub/backend/internal/migration/bundle"
	"tokenhub/backend/internal/migration/sink/tokenhub"
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
	_ = planCmd.MarkFlagRequired("bundle")

	verifyCmd.Flags().String("bundle", "", "Path to bundle JSON file")
	_ = verifyCmd.MarkFlagRequired("bundle")

	rollbackCmd.Flags().String("checkpoint", "", "Path to checkpoint JSON file")
	_ = rollbackCmd.MarkFlagRequired("checkpoint")
}

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Dry-run: show what apply would do",
	RunE: func(cmd *cobra.Command, args []string) error {
		bundlePath, _ := cmd.Flags().GetString("bundle")
		data, err := os.ReadFile(bundlePath)
		if err != nil {
			return fmt.Errorf("read bundle: %w", err)
		}
		b, err := bundle.Unmarshal(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return fmt.Errorf("%w: %v", errExit(ExitSchemaMismatch), err)
		}

		store := server.NewMemoryStore()
		sink := tokenhub.NewStoreSink(store, bundle.StaticResolver{})
		report, err := sink.Plan(b)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return fmt.Errorf("%w: %v", errExit(ExitSinkRejected), err)
		}

		fmt.Printf("Plan:\n")
		fmt.Printf("  Created: %d\n", report.Created)
		fmt.Printf("  Updated: %d\n", report.Updated)
		fmt.Printf("  Skipped: %d\n", report.Skipped)
		return nil
	},
}

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply a bundle to TokenHub",
	RunE: func(cmd *cobra.Command, args []string) error {
		bundlePath, _ := cmd.Flags().GetString("bundle")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		data, err := os.ReadFile(bundlePath)
		if err != nil {
			return fmt.Errorf("read bundle: %w", err)
		}
		b, err := bundle.Unmarshal(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return fmt.Errorf("%w: %v", errExit(ExitSchemaMismatch), err)
		}

		store := server.NewMemoryStore()
		sink := tokenhub.NewStoreSink(store, bundle.StaticResolver{})

		if dryRun {
			report, err := sink.Plan(b)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return fmt.Errorf("%w: %v", errExit(ExitSinkRejected), err)
			}
			fmt.Printf("Dry-run plan:\n")
			fmt.Printf("  Created: %d\n", report.Created)
			fmt.Printf("  Updated: %d\n", report.Updated)
			fmt.Printf("  Skipped: %d\n", report.Skipped)
			return nil
		}

		result, err := sink.Apply(b)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return fmt.Errorf("%w: %v", errExit(ExitSinkRejected), err)
		}

		fmt.Printf("Apply complete:\n")
		fmt.Printf("  Created: %d\n", result.Report.Created)
		fmt.Printf("  Updated: %d\n", result.Report.Updated)
		fmt.Printf("  Skipped: %d\n", result.Report.Skipped)
		for ref, key := range result.NewKeys {
			fmt.Printf("  New key for %s: %s\n", ref, key)
		}

		return nil
	},
}

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify applied state matches bundle",
	RunE: func(cmd *cobra.Command, args []string) error {
		bundlePath, _ := cmd.Flags().GetString("bundle")
		data, err := os.ReadFile(bundlePath)
		if err != nil {
			return fmt.Errorf("read bundle: %w", err)
		}
		b, err := bundle.Unmarshal(data)
		if err != nil {
			return fmt.Errorf("%w: %v", errExit(ExitSchemaMismatch), err)
		}

		store := server.NewMemoryStore()
		sink := tokenhub.NewStoreSink(store, bundle.StaticResolver{})
		result, err := sink.Verify(b)
		if err != nil {
			return fmt.Errorf("%w: %v", errExit(ExitSinkRejected), err)
		}

		if result.OK {
			fmt.Println("Verify: PASS")
			return nil
		}

		fmt.Fprintf(os.Stderr, "Verify: FAIL (%d issues)\n", len(result.Issues))
		for _, issue := range result.Issues {
			fmt.Fprintf(os.Stderr, "  [%s] %s: %s\n", issue.Resource, issue.Ref, issue.Message)
		}
		return errExit(ExitVerifyMismatch)
	},
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Rollback to pre-apply state",
	RunE: func(cmd *cobra.Command, args []string) error {
		checkpointPath, _ := cmd.Flags().GetString("checkpoint")
		data, err := os.ReadFile(checkpointPath)
		if err != nil {
			return fmt.Errorf("read checkpoint: %w", err)
		}

		var checkpoint tokenhub.Checkpoint
		if err := bundle.UnmarshalCheckpoint(data, &checkpoint); err != nil {
			return fmt.Errorf("parse checkpoint: %w", err)
		}

		store := server.NewMemoryStore()
		sink := tokenhub.NewStoreSink(store, bundle.StaticResolver{})
		result, err := sink.Rollback(checkpoint)
		if err != nil {
			return fmt.Errorf("%w: %v", errExit(ExitSinkRejected), err)
		}

		fmt.Printf("Rollback: %d changes reverted\n", len(result.Changes))
		return nil
	},
}
