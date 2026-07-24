package cli

import (
	"context"
	"fmt"
	"os"

	"tokenhub/backend/internal/migration/bundle"
	"tokenhub/backend/internal/migration/source"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(extractCmd)
	extractCmd.Flags().String("from", "", "Path to source gateway config")
	extractCmd.Flags().String("out", "", "Output bundle JSON file (default: stdout)")
	_ = extractCmd.MarkFlagRequired("from")
}

var extractCmd = &cobra.Command{
	Use:   "extract [source]",
	Short: "Extract a canonical migration bundle from a source gateway",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sourceName := args[0]
		extractor, err := source.Get(sourceName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return fmt.Errorf("%w: %v", errExit(ExitSourceUnreadable), err)
		}

		fromPath, _ := cmd.Flags().GetString("from")
		outPath, _ := cmd.Flags().GetString("out")

		b, err := extractor.Extract(context.Background(), source.ExtractOptions{
			InputPath: fromPath,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return fmt.Errorf("%w: %v", errExit(ExitSourceUnreadable), err)
		}

		data, err := bundle.Marshal(b)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return fmt.Errorf("%w: %v", errExit(ExitSchemaMismatch), err)
		}

		if outPath != "" {
			if err := os.WriteFile(outPath, data, 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return err
			}
			fmt.Printf("Bundle written to %s\n", outPath)
		} else {
			fmt.Println(string(data))
		}

		if len(b.Warnings) > 0 {
			fmt.Fprintf(os.Stderr, "\nWarnings (%d):\n", len(b.Warnings))
			for _, w := range b.Warnings {
				fmt.Fprintf(os.Stderr, "  [%s] %s: %s\n", w.Severity, w.Code, w.Message)
			}
		}

		return nil
	},
}

type exitError struct {
	code int
}

func (e exitError) Error() string {
	return fmt.Sprintf("exit code %d", e.code)
}

func errExit(code int) error {
	return exitError{code: code}
}
