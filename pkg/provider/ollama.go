package provider

import "github.com/papercomputeco/sweeper/pkg/worker"

func init() {
	Register(Provider{
		Name:        "ollama",
		Kind:        KindAPI,
		UsesAPIBase: true,
		NewExec: func(cfg Config) worker.Executor {
			return worker.NewOllamaExecutor(worker.OllamaConfig{
				Model:   cfg.Model,
				APIBase: cfg.APIBase,
			})
		},
	})
}
