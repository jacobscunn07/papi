// Package appconfig resolves the repo root and assembles a ResearchConfig from
// viper (config file + env + bound flag defaults). It lives in its own package so
// both the cmd layer and the TUI can build configs without an import cycle.
package appconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"papi/internal/types"

	"github.com/spf13/viper"
)

// Resolve determines the repo root and loads .papi/config into viper.
//
// It starts from the --repo-root value (default "."). If that directory has no
// .papi directory, it walks up the parent directories until it finds one, so the
// app works whether launched from the repo root or a subdirectory (e.g. via
// `go run -C packages/papi .`).
func Resolve() (string, error) {
	start, err := filepath.Abs(viper.GetString("repo-root"))
	if err != nil {
		return "", fmt.Errorf("resolve repo-root: %w", err)
	}
	repoRoot := findRepoRoot(start)

	viper.SetConfigFile(filepath.Join(repoRoot, ".papi", "config"))
	viper.SetConfigType("yaml")
	if err := viper.ReadInConfig(); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read .papi/config: %w", err)
	}
	return repoRoot, nil
}

// findRepoRoot returns the nearest ancestor of start (inclusive) that contains a
// .papi directory, or start itself if none is found.
func findRepoRoot(start string) string {
	for dir := start; ; {
		if fi, err := os.Stat(filepath.Join(dir, ".papi")); err == nil && fi.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
}

// Build assembles a ResearchConfig for skillName from current viper settings.
func Build(repoRoot, skillName string) (*types.ResearchConfig, error) {
	tagsRaw := viper.GetString("tags")
	var tags []string
	if tagsRaw != "" {
		for _, t := range strings.Split(tagsRaw, ",") {
			if trimmed := strings.TrimSpace(t); trimmed != "" {
				tags = append(tags, trimmed)
			}
		}
	}

	llmPct := viper.GetInt("llm-weight")
	nonLLMPct := viper.GetInt("weight")
	if llmPct+nonLLMPct != 100 {
		return nil, fmt.Errorf("llm-weight (%d) and weight (%d) must sum to 100", llmPct, nonLLMPct)
	}

	return &types.ResearchConfig{
		SkillName:         skillName,
		SkillDir:          filepath.Join(repoRoot, "skills", skillName),
		ScenariosDir:      filepath.Join(repoRoot, ".papi", "skills", skillName, "scenarios"),
		CustomEvalsDir:    filepath.Join(repoRoot, ".papi", "skills", skillName, "evals"),
		MaxIterations:     viper.GetInt("iterations"),
		MaxBudgetUSD:      viper.GetFloat64("budget"),
		Tags:              tags,
		DryRun:            viper.GetBool("dry-run"),
		Resume:            viper.GetBool("resume"),
		ScenarioModel:     viper.GetString("scenario-model"),
		QualityModel:      viper.GetString("quality-model"),
		ResearchModel:     viper.GetString("research-model"),
		MaxRuns:           viper.GetInt("max-runs"),
		LLMJudgeWeight:    float64(llmPct) / 100.0,
		NonLLMJudgeWeight: float64(nonLLMPct) / 100.0,
	}, nil
}
