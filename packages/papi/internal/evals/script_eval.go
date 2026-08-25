package evals

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"papi/internal/script"
	"papi/internal/types"
)

type scriptEval struct {
	id       string
	name     string
	filePath string
	runner   []string
}

func (e *scriptEval) ID() string       { return e.id }
func (e *scriptEval) Name() string     { return e.name }
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

// unsupportedEvalExts are extensions papi used to execute. A leftover file with
// one of these produces an error naming it, rather than being silently ignored
// by the narrowed glob below.
var unsupportedEvalExts = []string{".py", ".sh", ".go"}

// discoverEvals finds *.eval.ts and *.eval.js files in dir and returns them as Eval instances.
// Returns nil (not error) if dir does not exist or has no eval files; returns an error if dir
// contains an eval file written in a no-longer-supported language.
func discoverEvals(dir string) ([]types.Eval, error) {
	for _, ext := range unsupportedEvalExts {
		matches, err := filepath.Glob(filepath.Join(dir, "*.eval"+ext))
		if err != nil {
			return nil, fmt.Errorf("scan evals dir %s: %w", dir, err)
		}
		if len(matches) > 0 {
			return nil, fmt.Errorf("unsupported eval %s: evals must be .ts or .js", matches[0])
		}
	}

	var out []types.Eval
	for _, ext := range script.SupportedExts {
		matches, err := filepath.Glob(filepath.Join(dir, "*.eval"+ext))
		if err != nil {
			return nil, fmt.Errorf("scan evals dir %s: %w", dir, err)
		}
		runner, err := script.Resolve("eval" + ext)
		if err != nil {
			return nil, err
		}
		for _, f := range matches {
			id := strings.TrimSuffix(filepath.Base(f), ".eval"+ext)
			out = append(out, &scriptEval{
				id:       id,
				name:     id,
				filePath: f,
				runner:   runner,
			})
		}
	}
	return out, nil
}
