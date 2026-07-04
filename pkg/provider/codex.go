package provider

import "github.com/papercomputeco/sweeper/pkg/worker"

func init() {
	Register(Provider{
		Name: "codex",
		Kind: KindCLI,
		NewExec: func(cfg Config) worker.Executor {
			return worker.NewCodexExecutor(worker.CodexConfig{
				Model:     cfg.Model,
				ExtraArgs: cfg.ExtraArgs,
			})
		},
	})
}
