package provider

import "github.com/papercomputeco/sweeper/pkg/worker"

func init() {
	Register(Provider{
		Name: "pi",
		Kind: KindCLI,
		NewExec: func(cfg Config) worker.Executor {
			return worker.NewPiExecutor(worker.PiConfig{
				Model:     cfg.Model,
				ExtraArgs: cfg.ExtraArgs,
				Capture:   cfg.Capture,
			})
		},
	})
}
