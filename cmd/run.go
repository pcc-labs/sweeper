package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
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
	var providerName string
	var providerModel string
	var providerAPI string
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
			if rootPF.Changed("no-paper") {
				tc.Paper.Enabled = !noPaper
			}
			if rootPF.Changed("no-tapes") { // deprecated alias for --no-paper
				tc.Run.NoTapes = noTapes
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
			}
			if cmd.Flags().Changed("model") {
				tc.Provider.Model = providerModel
			}
			if cmd.Flags().Changed("api-base") {
				tc.Provider.APIBase = providerAPI
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
			pub := buildPublisher(tc)

			// Validate provider exists before proceeding.
			if _, err := provider.Get(cfg.Provider); err != nil {
				return err
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

			if vmName != "" || vmJcard != "" {
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

			if useVM {
				absTarget, _ := filepath.Abs(cfg.TargetDir)
				if cfg.VMName != "" {
					vmHandle := vm.Attach(cfg.VMName, absTarget)
					opts = append(opts, agent.WithVM(vmHandle))
					opts = append(opts, agent.WithExecutor(worker.NewVMExecutor(vmHandle)))
					fmt.Printf("VM: using existing VM %s\n", cfg.VMName)
				} else {
					name := vm.NewVMName()
					jcardDir := filepath.Join(absTarget, ".sweeper", "vm")
					if cfg.VMJcard != "" {
						jcardDir = filepath.Dir(cfg.VMJcard)
					}
					vmHandle, err := vm.Boot(name, absTarget, jcardDir)
					if err != nil {
						return fmt.Errorf("booting VM: %w", err)
					}
					opts = append(opts, agent.WithVM(vmHandle))
					opts = append(opts, agent.WithExecutor(worker.NewVMExecutor(vmHandle)))
					fmt.Printf("VM: booted %s (managed, will teardown on exit)\n", name)
				}
			}

			opts = append(opts, agent.WithPublisher(pub))

			a := agent.New(cfg, opts...)
			summary, err := a.Run(ctx)
			if err != nil {
				return err
			}
			fmt.Printf("\nSummary: %d issues found, %d fixed, %d tasks failed\n",
				summary.TotalIssues, summary.Fixed, summary.Failed)
			if summary.Failed > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be fixed without making changes")
	cmd.Flags().IntVar(&maxRounds, "max-rounds", 1, "maximum retry rounds (1 = single pass)")
	cmd.Flags().IntVar(&staleThreshold, "stale-threshold", 2, "consecutive non-improving rounds before exploration mode")
	cmd.Flags().BoolVar(&useVM, "vm", false, "boot ephemeral stereOS VM, teardown on exit")
	cmd.Flags().StringVar(&vmName, "vm-name", "", "use existing VM by name (no managed lifecycle, implies --vm)")
	cmd.Flags().StringVar(&vmJcard, "vm-jcard", "", "custom jcard.toml path (implies --vm)")
	cmd.Flags().StringVar(&providerName, "provider", "claude", "AI provider (claude, codex, ollama)")
	cmd.Flags().StringVar(&providerModel, "model", "", "model name for the provider (e.g. qwen2.5-coder:7b)")
	cmd.Flags().StringVar(&providerAPI, "api-base", "", "API base URL for API providers (e.g. http://localhost:11434)")
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

func buildPublisher(tc config.TOMLConfig) telemetry.Publisher {
	jsonl := telemetry.NewJSONLPublisher(tc.Telemetry.Dir)

	if tc.Telemetry.Backend != "confluent" {
		return jsonl
	}

	cc := tc.Telemetry.Confluent
	if len(cc.Brokers) == 0 || cc.Topic == "" {
		fmt.Println("Warning: confluent backend selected but brokers/topic not configured, using JSONL only")
		return jsonl
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
		return jsonl
	}

	return telemetry.NewMultiPublisher(jsonl, cp)
}
