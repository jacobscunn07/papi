package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"papi/internal/appconfig"
	"papi/internal/loop"
	"papi/internal/progress"
	"papi/internal/tui"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var papiCmd = &cobra.Command{
	Use:   "papi",
	Short: "Autoresearch loop for self-improving Claude skills",
	// With no subcommand, launch the interactive TUI.
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := appconfig.Resolve()
		if err != nil {
			return err
		}
		return tui.Run(repoRoot)
	},
}

var runCmd = &cobra.Command{
	Use:   "run <skill>",
	Short: "Run the autoresearch loop for a skill",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := appconfig.Resolve()
		if err != nil {
			return err
		}

		cfg, err := appconfig.Build(repoRoot, args[0])
		if err != nil {
			return err
		}

		rep := progress.NewCLIReporter(cfg.MaxIterations, cfg.LLMJudgeWeight, cfg.NonLLMJudgeWeight)
		return loop.Run(context.Background(), cfg, repoRoot, rep, false)
	},
}

func init() {
	flags := runCmd.Flags()

	flags.Int("iterations", 20, "Max iterations")
	flags.Float64("budget", 5.0, "Max spend in USD")
	flags.String("tags", "", "Filter scenarios by comma-separated tags")
	flags.Bool("dry-run", false, "Run evals without modifying SKILL.md or committing")
	flags.Bool("resume", false, "Resume the latest unfinished run for this skill instead of starting fresh")
	flags.String("scenario-model", "claude-haiku-4-5-20251001", "Model for invocation check (description-only)")
	flags.String("quality-model", "claude-sonnet-4-6", "Model for quality check (full skill execution)")
	flags.String("research-model", "claude-opus-4-7", "Model for research agent")
	flags.String("repo-root", ".", "Repository root (defaults to current directory)")
	flags.Int("max-runs", 3, "Max runs to retain per skill (0 = keep all)")
	flags.Int("llm-weight", 30, "Category weight % for LLM judge evals (default 30)")
	flags.Int("weight", 70, "Category weight % for non-LLM judge evals (default 70; with llm-weight must sum to 100)")

	// Bind all flags to viper so env vars and config files also work.
	// Env var convention: RESEARCH_BUDGET, RESEARCH_ITERATIONS, etc.
	_ = viper.BindPFlags(flags)
	viper.SetEnvPrefix("RESEARCH")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	papiCmd.AddCommand(runCmd)
}

// Execute is the entrypoint called from main.
func Execute() {
	if err := papiCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
