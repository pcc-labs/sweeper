package cmd

import (
	"github.com/spf13/cobra"
)

import "time"

var (
	targetDir   string
	concurrency int
	rateLimit   time.Duration
	noPaper     bool
	noTapes     bool
	configPath  string
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "sweeper",
		Short: "AI-powered code sweeper",
		Long:  "Runs linters, dispatches Claude Code sub-agents to fix issues in parallel, and learns from outcomes.",
	}
	root.PersistentFlags().StringVarP(&targetDir, "target", "t", ".", "target directory to maintain")
	root.PersistentFlags().IntVarP(&concurrency, "concurrency", "c", 2, "max parallel sub-agents")
	root.PersistentFlags().DurationVar(&rateLimit, "rate-limit", 2*time.Second, "minimum delay between agent dispatches (e.g. 2s, 500ms)")
	root.PersistentFlags().BoolVar(&noPaper, "no-paper", false, "disable the paper capture detect+warn")
	root.PersistentFlags().BoolVar(&noTapes, "no-tapes", false, "disable tapes integration")
	_ = root.PersistentFlags().MarkDeprecated("no-tapes", "use --no-paper")
	_ = root.PersistentFlags().MarkHidden("no-tapes")
	root.PersistentFlags().StringVar(&configPath, "config", "", "path to config.toml (default: .sweeper/config.toml)")
	root.AddCommand(newVersionCmd())
	root.AddCommand(newRunCmd())
	root.AddCommand(newObserveCmd())
	return root
}

func Execute() error {
	return NewRootCmd().Execute()
}
