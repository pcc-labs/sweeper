package provider

import "github.com/papercomputeco/sweeper/pkg/worker"

func init() {
	Register(Provider{
		Name: "claude",
		Kind: KindCLI,
		NewExec: func(cfg Config) worker.Executor {
			return worker.NewClaudeExecutor(worker.ClaudeConfig{
				Model:     cfg.Model,
				ExtraArgs: cfg.ExtraArgs,
			})
		},
	})
}
