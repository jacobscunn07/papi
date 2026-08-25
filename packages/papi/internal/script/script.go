// Package script resolves the interpreter used to execute papi's user-supplied
// scripts (custom evals and lifecycle hooks). TypeScript and JavaScript are the
// only supported languages; anything else is an error rather than a silent skip.
package script

import (
	"fmt"
	"path/filepath"
)

// SupportedExts lists the extensions papi can execute, in discovery order.
var SupportedExts = []string{".ts", ".js"}

// Resolve returns the command prefix for executing path, or an error if path's
// extension is not supported.
func Resolve(path string) ([]string, error) {
	ext := filepath.Ext(path)
	switch ext {
	case ".ts":
		return []string{"tsx"}, nil
	case ".js":
		return []string{"node"}, nil
	}
	return nil, fmt.Errorf("unsupported script extension %q in %s: evals and hooks must be .ts or .js", ext, path)
}
