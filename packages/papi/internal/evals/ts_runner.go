package evals

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"papi/internal/types"
)

type scriptEval struct {
	id       string
	name     string
	weight   float64
	filePath string
	runner   []string
}

func (e *scriptEval) ID() string       { return e.id }
func (e *scriptEval) Name() string     { return e.name }
func (e *scriptEval) Weight() float64  { return e.weight }
func (e *scriptEval) IsLLMJudge() bool { return false }

func (e *scriptEval) Evaluate(ctx types.EvalContext) (types.EvalResult, error) {
	ctxJSON, err := json.Marshal(ctx)
	if err != nil {
		return types.EvalResult{}, fmt.Errorf("marshal ctx: %w", err)
	}
	args := append(e.runner[1:], e.filePath)
	cmd := exec.Command(e.runner[0], args...)
	cmd.Stdin = bytes.NewReader(ctxJSON)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return types.EvalResult{}, fmt.Errorf("eval %s: %w\n%s", e.filePath, err, stderr.String())
	}
	var result types.EvalResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return types.EvalResult{}, fmt.Errorf("parse eval result from %s: %w", e.filePath, err)
	}
	return result, nil
}

// resolveEvalRunner returns the command prefix for executing the given eval file based on its extension.
func resolveEvalRunner(filePath string) ([]string, bool) {
	switch filepath.Ext(filePath) {
	case ".ts":
		return []string{"tsx"}, true
	case ".js":
		return []string{"node"}, true
	case ".py":
		return []string{"python3"}, true
	case ".sh":
		return []string{"bash"}, true
	case ".go":
		return []string{"go", "run"}, true
	}
	return nil, false
}

// discoverEvals finds *.eval.<ext> files in dir for all supported languages and returns them as Eval instances.
// Returns nil (not error) if dir does not exist or has no eval files.
func discoverEvals(dir string) []types.Eval {
	exts := []string{".ts", ".js", ".py", ".sh", ".go"}
	var out []types.Eval
	for _, ext := range exts {
		matches, err := filepath.Glob(filepath.Join(dir, "*.eval"+ext))
		if err != nil || len(matches) == 0 {
			continue
		}
		runner, _ := resolveEvalRunner("x" + ext)
		for _, f := range matches {
			base := filepath.Base(f)
			id := strings.TrimSuffix(base, ".eval"+ext)
			out = append(out, &scriptEval{
				id:       id,
				name:     id,
				weight:   1.0,
				filePath: f,
				runner:   runner,
			})
		}
	}
	return out
}
