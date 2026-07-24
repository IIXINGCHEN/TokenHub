package cli

import (
	"github.com/spf13/cobra"
)

// Exit codes as defined in docs/migration/PLAN.md §6.
const (
	ExitOK               = 0
	ExitPartial          = 2
	ExitVerifyMismatch   = 3
	ExitSourceUnreadable = 4
	ExitSinkRejected     = 5
	ExitSchemaMismatch   = 6
)

func Execute() error {
	return rootCmd.Execute()
}

var rootCmd = &cobra.Command{
	Use:   "tokenhub-migrate",
	Short: "Migrate competing AI gateways into TokenHub",
	Long: `tokenhub-migrate provides a repeatable, idempotent workflow
for migrating competitor AI gateway configurations into TokenHub.

Supported sources are listed by the "sources" command.`,
	SilenceUsage: true,
}

func init() {
	rootCmd.PersistentFlags().String("secret-source", "env", "Secret resolution source: env|file|prompt")
	rootCmd.PersistentFlags().String("id-strategy", "prefixed", "ID generation strategy: stable|prefixed|source")
	rootCmd.PersistentFlags().String("report", "", "Write structured report to JSON file")
	rootCmd.PersistentFlags().String("log-level", "info", "Log level: debug|info|warn|error")
}
