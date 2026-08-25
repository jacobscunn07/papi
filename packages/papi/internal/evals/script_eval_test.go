package evals

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestDiscoverEvals_FindsTSAndJS(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"alpha.eval.ts", "beta.eval.js", "types.ts", "utils.ts", "notes.md"} {
		os.WriteFile(filepath.Join(dir, name), []byte("// x\n"), 0644)
	}

	found, err := discoverEvals(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var ids []string
	for _, e := range found {
		ids = append(ids, e.ID())
	}
	sort.Strings(ids)
	if len(ids) != 2 || ids[0] != "alpha" || ids[1] != "beta" {
		t.Errorf("got %v, want [alpha beta]", ids)
	}
}

func TestDiscoverEvals_ErrorsOnUnsupportedLanguage(t *testing.T) {
	for _, ext := range unsupportedEvalExts {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "ok.eval.ts"), []byte("// x\n"), 0644)
		stale := filepath.Join(dir, "stale.eval"+ext)
		os.WriteFile(stale, []byte("// x\n"), 0644)

		_, err := discoverEvals(dir)
		if err == nil {
			t.Fatalf("%s: expected error, got nil", ext)
		}
		if !strings.Contains(err.Error(), stale) {
			t.Errorf("%s: error %q should name the offending file %q", ext, err, stale)
		}
	}
}

func TestDiscoverEvals_MissingDir(t *testing.T) {
	found, err := discoverEvals(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != nil {
		t.Errorf("got %v, want nil", found)
	}
}
