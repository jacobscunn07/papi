package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"papi/internal/loop"
	"papi/internal/types"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var papiCmd = &cobra.Command{
	Use:   "papi",
	Short: "Autoresearch loop for self-improving Claude skills",
}

var runCmd = &cobra.Command{
	Use:   "run <skill>",
	Short: "Run the autoresearch loop for a skill",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		viper.SetConfigFile(filepath.Join(viper.GetString("repo-root"), ".papi", "config"))
		viper.SetConfigType("yaml")
		if err := viper.ReadInConfig(); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read .papi/config: %w", err)
		}

		repoRoot, err := filepath.Abs(viper.GetString("repo-root"))
		if err != nil {
			return fmt.Errorf("resolve repo-root: %w", err)
		}

		skillName := args[0]
		tagsRaw := viper.GetString("tags")
		var tags []string
		if tagsRaw != "" {
			for _, t := range strings.Split(tagsRaw, ",") {
				if trimmed := strings.TrimSpace(t); trimmed != "" {
					tags = append(tags, trimmed)
				}
			}
		}

		cfg := &types.ResearchConfig{
			SkillName:      skillName,
			SkillDir:       filepath.Join(repoRoot, "skills", skillName),
			ScenariosDir:   filepath.Join(repoRoot, ".papi", "skills", skillName, "scenarios"),
			CustomEvalsDir: filepath.Join(repoRoot, ".papi", "skills", skillName, "evals"),
			MaxIterations:  viper.GetInt("iterations"),
			MaxBudgetUSD:   viper.GetFloat64("budget"),
			Tags:           tags,
			DryRun:         viper.GetBool("dry-run"),
			ScenarioModel:  viper.GetString("scenario-model"),
			QualityModel:   viper.GetString("quality-model"),
			ResearchModel:  viper.GetString("research-model"),
			MaxRuns:           viper.GetInt("max-runs"),
			LLMJudgeWeight:    viper.GetFloat64("llm-weight"),
			NonLLMJudgeWeight: viper.GetFloat64("non-llm-weight"),
		}

		return loop.Run(cfg, repoRoot)
	},
}

func init() {
	flags := runCmd.Flags()

	flags.Int("iterations", 20, "Max iterations")
	flags.Float64("budget", 5.0, "Max spend in USD")
	flags.String("tags", "", "Filter scenarios by comma-separated tags")
	flags.Bool("dry-run", false, "Run evals without modifying SKILL.md or committing")
	flags.String("scenario-model", "claude-haiku-4-5-20251001", "Model for invocation check (description-only)")
	flags.String("quality-model", "claude-sonnet-4-6", "Model for quality check (full skill execution)")
	flags.String("research-model", "claude-opus-4-7", "Model for research agent")
	flags.String("repo-root", ".", "Repository root (defaults to current directory)")
	flags.Int("max-runs", 3, "Max runs to retain per skill (0 = keep all)")
	flags.Float64("llm-weight", 0.3, "Category weight for LLM judge evals (0–1)")
	flags.Float64("non-llm-weight", 0.7, "Category weight for non-LLM judge evals (0–1)")

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
