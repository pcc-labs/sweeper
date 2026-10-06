package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/papercomputeco/sweeper/pkg/agent"
	"github.com/papercomputeco/sweeper/pkg/config"
	"github.com/papercomputeco/sweeper/pkg/linter"
	"github.com/papercomputeco/sweeper/pkg/provider"
	"github.com/papercomputeco/sweeper/pkg/telemetry"
	"github.com/papercomputeco/sweeper/pkg/telemetry/confluent"
	"github.com/papercomputeco/sweeper/pkg/vm"
	"github.com/papercomputeco/sweeper/pkg/worker"
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	var dryRun bool
	var maxRounds int
	var staleThreshold int
	var useVM bool
	var vmName string
	var vmJcard string
	var vmBackend string
	var vmImage string
	var providerName string
	var providerModel string
	var providerAPI string
	var captureMode string
	var captureTapesProxy string
	var advisorProvider string
	var advisorModel string
	cmd := &cobra.Command{
		Use:   "run [-- command ...]",
		Short: "Run sweeper against target directory",
		Long: `Run sweeper to lint and fix issues.

Examples:
  sweeper run                              # default: golangci-lint
  sweeper run --max-rounds 3               # retry up to 3 rounds
  sweeper run -- npm run lint              # arbitrary command
  npm run lint | sweeper run               # piped stdin`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			// Load TOML config (defaults -> home -> project -> env).
			tc, err := config.LoadTOML(targetDir, configPath)
			if err != nil {
				fmt.Printf("Warning: loading config: %v\n", err)
				tc = config.NewDefaultTOMLConfig()
			}

			// CLI flags override TOML config.
			rootPF := cmd.Root().PersistentFlags()
			if rootPF.Changed("concurrency") {
				tc.Run.Concurrency = concurrency
			}
			if rootPF.Changed("rate-limit") {
				tc.Run.RateLimit = rateLimit.String()
			}
			if cmd.Flags().Changed("max-rounds") {
				tc.Run.MaxRounds = maxRounds
			}
			if cmd.Flags().Changed("stale-threshold") {
				tc.Run.StaleThreshold = staleThreshold
			}
			if cmd.Flags().Changed("dry-run") {
				tc.Run.DryRun = dryRun
			}
			if cmd.Flags().Changed("provider") {
				tc.Provider.Name = providerName
				tc.Worker.Name = providerName
			}
			if cmd.Flags().Changed("model") {
				tc.Provider.Model = providerModel
				tc.Worker.Model = providerModel
			}
			if cmd.Flags().Changed("api-base") {
				tc.Provider.APIBase = providerAPI
				tc.Worker.APIBase = providerAPI
			}
			if cmd.Flags().Changed("vm-backend") {
				tc.VM.Backend = vmBackend
			}
			if cmd.Flags().Changed("vm-image") {
				tc.VM.Image = vmImage
			}
			if cmd.Flags().Changed("capture") {
				tc.Capture.Mode = captureMode
			}
			if cmd.Flags().Changed("capture-tapes-proxy") {
				tc.Capture.TapesProxy = captureTapesProxy
			}
			if cmd.Flags().Changed("advisor-provider") {
				tc.Advisor.Name = advisorProvider
			}
			if cmd.Flags().Changed("advisor-model") {
				tc.Advisor.Model = advisorModel
			}

			// Build runtime config from TOML.
			cfg := config.FromTOML(tc)
			cfg.TargetDir = targetDir

			clamped := config.ClampConcurrency(cfg.Concurrency)
			if clamped != cfg.Concurrency {
				fmt.Printf("Concurrency clamped to %d (max %d)\n", clamped, config.MaxConcurrency)
				cfg.Concurrency = clamped
			}

			// Build telemetry publisher from config.
			pub, telemetryPath := buildPublisher(tc)

			// Validate provider exists before proceeding.
			if _, err := provider.Get(cfg.Provider); err != nil {
				return err
			}

			// Validate capture mode syntax here; the semantic checks (VM,
			// provider support, paperctl presence) run after --vm resolves.
			if !worker.ValidCaptureMode(cfg.CaptureMode) {
				return fmt.Errorf("invalid capture mode %q (valid: auto, paper, tapes, none)", cfg.CaptureMode)
			}

			piped := isPiped()
			dashArgs := argsAfterDash(cmd, args)

			if piped && len(dashArgs) > 0 {
				return fmt.Errorf("cannot use both piped input and -- command; choose one")
			}

			var opts []agent.Option

			if piped {
				cfg.LinterName = "custom"
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("reading stdin: %w", err)
				}
				raw := string(data)
				opts = append(opts, agent.WithLinterFunc(
					func(ctx context.Context, dir string) (linter.ParseResult, error) {
						return linter.ParseOutput(raw), nil
					},
				))
			} else if len(dashArgs) > 0 {
				cfg.LintCommand = dashArgs
				cfg.LinterName = filepath.Base(dashArgs[0])
				opts = append(opts, agent.WithLinterFunc(
					func(ctx context.Context, dir string) (linter.ParseResult, error) {
						return linter.RunCommand(ctx, dir, dashArgs)
					},
				))
			}

			if vmName != "" || vmJcard != "" || cmd.Flags().Changed("vm-backend") || cmd.Flags().Changed("vm-image") {
				useVM = true
			}
			cfg.VM = useVM
			cfg.VMName = vmName
			cfg.VMJcard = vmJcard

			// Validate: --vm is only compatible with CLI providers.
			if useVM {
				p, err := provider.Get(cfg.Provider)
				if err != nil {
					return fmt.Errorf("provider %q: %w", cfg.Provider, err)
				}
				if p.Kind != provider.KindCLI {
					return fmt.Errorf("--vm is only compatible with CLI providers (got %q)", cfg.Provider)
				}
			}

			// Fail fast when the worker's CLI binary is absent, instead of
			// recording one exec failure per task. CLI providers exec the
			// binary named after the provider (pi, claude, codex). Skipped
			// under --vm (agents run inside the VM) and --dry-run (nothing
			// dispatches).
			if !useVM && !cfg.DryRun {
				if p, err := provider.Get(cfg.Provider); err == nil && p.Kind == provider.KindCLI {
					if _, lerr := exec.LookPath(p.Name); lerr != nil {
						return fmt.Errorf("provider %q requires the %q CLI on PATH; install it or pick another with --provider (available: %v)", cfg.Provider, p.Name, provider.Available())
					}
				}
			}

			// An explicitly requested capture gateway must actually apply:
			// reject combinations where it would be silently dropped, and
			// only then require paperctl. Auto and none pass through — auto
			// means "capture when the environment provides it".
			if cfg.CaptureMode == worker.CaptureModePaper || cfg.CaptureMode == worker.CaptureModeTapes {
				if useVM {
					return fmt.Errorf("--capture %s is not supported with --vm: VM sub-agents run claude directly inside the VM and are not captured", cfg.CaptureMode)
				}
				if p, err := provider.Get(cfg.Provider); err == nil && !p.SupportsCapture {
					return fmt.Errorf("provider %q does not support session capture (--capture %s); capture-aware providers: claude, pi", cfg.Provider, cfg.CaptureMode)
				}
				if cfg.CaptureMode == worker.CaptureModePaper {
					if _, ok := worker.PaperBinary(); !ok {
						return fmt.Errorf("capture mode %q requires the paperctl CLI on PATH", cfg.CaptureMode)
					}
				}
			}

			if useVM {
				absTarget, _ := filepath.Abs(cfg.TargetDir)
				backend, err := vm.ParseBackend(cfg.VMBackend)
				if err != nil {
					return err
				}
				if backend == vm.BackendSmol && cfg.VMJcard != "" {
					return fmt.Errorf("--vm-jcard applies to the stereos backend only; the smol backend boots --vm-image")
				}
				if _, lerr := exec.LookPath(backend.Binary()); lerr != nil {
					return fmt.Errorf("VM backend %q requires the %q CLI on PATH", backend, backend.Binary())
				}
				jcardDir := filepath.Join(absTarget, ".sweeper", "vm")
				if cfg.VMJcard != "" {
					jcardDir = filepath.Dir(cfg.VMJcard)
				}
				vmHandle, managed, err := vm.New(vm.Options{
					Backend:  backend,
					Name:     cfg.VMName,
					HostDir:  absTarget,
					JcardDir: jcardDir,
					Image:    cfg.VMImage,
				})
				if err != nil {
					return fmt.Errorf("booting %s VM: %w", backend, err)
				}
				if managed {
					fmt.Printf("VM: booted %s on %s (managed, will teardown on exit)\n", vm.Name(vmHandle), backend)
				} else {
					fmt.Printf("VM: using existing %s VM %s\n", backend, vm.Name(vmHandle))
				}
				opts = append(opts, agent.WithVM(vmHandle))
				opts = append(opts, agent.WithVMExecutorFactory(func(model string) worker.Executor {
					return worker.NewVMExecutor(vmHandle, worker.VMExecConfig{Model: model})
				}))
			}

			opts = append(opts, agent.WithPublisher(pub))

			a := agent.New(cfg, opts...)
			summary, err := a.Run(ctx)
			if err != nil {
				return err
			}
			fmt.Printf("\n%s\n", agent.FormatSummary(summary, telemetryPath))
			if summary.Failed > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be fixed without making changes")
	cmd.Flags().IntVar(&maxRounds, "max-rounds", 1, "maximum retry rounds (1 = single pass)")
	cmd.Flags().IntVar(&staleThreshold, "stale-threshold", 2, "consecutive non-improving rounds before exploration mode")
	cmd.Flags().BoolVar(&useVM, "vm", false, "boot an ephemeral VM per run, teardown on exit")
	cmd.Flags().StringVar(&vmBackend, "vm-backend", "", "VM backend: stereos (default) or smol (implies --vm)")
	cmd.Flags().StringVar(&vmImage, "vm-image", "", "OCI image for the smol backend (default "+vm.DefaultSmolImage+", implies --vm)")
	cmd.Flags().StringVar(&vmName, "vm-name", "", "use existing VM by name (no managed lifecycle, implies --vm)")
	cmd.Flags().StringVar(&vmJcard, "vm-jcard", "", "custom jcard.toml path, stereos backend only (implies --vm)")
	cmd.Flags().StringVar(&providerName, "provider", "pi", "AI provider (pi, claude, codex, ollama)")
	cmd.Flags().StringVar(&providerModel, "model", "", "model name for the provider (e.g. a model registered with pi, or qwen2.5-coder:7b for ollama)")
	cmd.Flags().StringVar(&providerAPI, "api-base", "", "API base URL for API providers (e.g. http://localhost:11434)")
	cmd.Flags().StringVar(&advisorProvider, "advisor-provider", "", "provider for the sweep-planning advisor (claude, codex; enables the advisor phase)")
	cmd.Flags().StringVar(&advisorModel, "advisor-model", "", "model for the sweep-planning advisor (e.g. claude-opus-4-8)")
	cmd.Flags().StringVar(&captureMode, "capture", "auto", "session-capture gateway: auto (paperctl when installed), paper, tapes, none")
	cmd.Flags().StringVar(&captureTapesProxy, "capture-tapes-proxy", "", "tapes proxy URL for --capture tapes (default http://localhost:8080)")
	return cmd
}

func isPiped() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice == 0
}

func argsAfterDash(cmd *cobra.Command, args []string) []string {
	idx := cmd.ArgsLenAtDash()
	if idx < 0 {
		return nil
	}
	return args[idx:]
}

// buildPublisher returns the telemetry publisher and the JSONL log path,
// surfaced in the end-of-run summary so users can find per-file details.
func buildPublisher(tc config.TOMLConfig) (telemetry.Publisher, string) {
	jsonl := telemetry.NewJSONLPublisher(tc.Telemetry.Dir)

	if tc.Telemetry.Backend != "confluent" {
		return jsonl, jsonl.Path()
	}

	cc := tc.Telemetry.Confluent
	if len(cc.Brokers) == 0 || cc.Topic == "" {
		fmt.Println("Warning: confluent backend selected but brokers/topic not configured, using JSONL only")
		return jsonl, jsonl.Path()
	}

	cp, err := confluent.NewPublisher(confluent.Config{
		Brokers:      cc.Brokers,
		Topic:        cc.Topic,
		ClientID:     cc.ClientID,
		APIKeyEnv:    cc.APIKeyEnv,
		APISecretEnv: cc.APISecretEnv,
	})
	if err != nil {
		fmt.Printf("Warning: confluent publisher: %v, using JSONL only\n", err)
		return jsonl, jsonl.Path()
	}

	return telemetry.NewMultiPublisher(jsonl, cp), jsonl.Path()
}
