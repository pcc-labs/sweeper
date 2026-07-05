package cmd

import (
	"fmt"

	"github.com/papercomputeco/sweeper/pkg/config"
	"github.com/papercomputeco/sweeper/pkg/observer"
	"github.com/spf13/cobra"
)

func newObserveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "observe",
		Short: "Analyze past runs and show learned patterns",
		RunE: func(cmd *cobra.Command, args []string) error {
			tc, err := config.LoadTOML(".", configPath)
			if err != nil {
				tc = config.NewDefaultTOMLConfig()
			}
			telDir := tc.Telemetry.Dir

			obs := observer.New(telDir)
			insights, err := obs.Analyze()
			if err != nil {
				return err
			}
			if len(insights) == 0 {
				fmt.Println("No past runs found. Run `sweeper run` first.")
				return nil
			}

			fmt.Println("Fix success rates by linter:")
			for _, i := range insights {
				line := fmt.Sprintf("  %-20s %d/%d (%.0f%%)", i.Linter, i.Successes, i.Attempts, i.SuccessRate*100)
				if i.TotalTokens > 0 {
					line += fmt.Sprintf("  [%d tokens]", i.TotalTokens)
				}
				fmt.Println(line)
			}

			models, err := obs.AnalyzeModels()
			if err == nil && len(models) > 0 {
				fmt.Println("\nModel tiers:")
				for _, m := range models {
					line := fmt.Sprintf("  %-28s %d/%d (%.0f%%)", m.Model, m.Successes, m.Attempts, m.SuccessRate*100)
					if m.TotalTokens > 0 {
						line += fmt.Sprintf("  [%d tokens]", m.TotalTokens)
					}
					fmt.Println(line)
				}
			}

			hist, err := obs.AnalyzeHistory()
			if err == nil && hist.TotalRuns > 0 {
				fmt.Printf("\nHistorical trends (%d runs):\n", hist.TotalRuns)
				if len(hist.RoundEffectiveness) > 0 {
					fmt.Println("  Round effectiveness:")
					for round, rate := range hist.RoundEffectiveness {
						fmt.Printf("    Round %d: %.0f%% of fixes\n", round, rate*100)
					}
				}
				if len(hist.StrategyEffectiveness) > 0 {
					fmt.Println("  Strategy effectiveness:")
					for strategy, rate := range hist.StrategyEffectiveness {
						fmt.Printf("    %-15s %.0f%% success\n", strategy, rate*100)
					}
				}
			}

			return nil
		},
	}
}
