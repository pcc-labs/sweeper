package provider

import "github.com/papercomputeco/sweeper/pkg/worker"

// Kind distinguishes CLI harnesses (that have built-in file tools) from
// API-only providers (that need file content in the prompt and return diffs).
type Kind int

const (
	KindCLI Kind = iota // e.g. claude, codex — has built-in file read/write
	KindAPI             // e.g. ollama — text-in, text-out; sweeper handles files
)

// Config holds provider-specific settings passed when constructing an executor.
type Config struct {
	Model     string               // model name (e.g. "qwen2.5-coder:7b")
	APIBase   string               // base URL for API providers (e.g. "http://localhost:11434")
	ExtraArgs []string             // additional CLI arguments
	Capture   worker.CaptureConfig // session-capture gateway (paper, tapes, none)
}

// Provider describes a registered AI backend.
type Provider struct {
	Name    string
	Kind    Kind
	NewExec func(Config) worker.Executor
	// UsesAPIBase reports whether the executor consumes Config.APIBase.
	// CLI harnesses (claude, codex, pi) manage their own endpoints, so an
	// api_base configured for them is ignored and worth a warning.
	UsesAPIBase bool
}
