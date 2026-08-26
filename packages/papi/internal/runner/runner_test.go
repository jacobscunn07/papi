package runner

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRunHook_ParsesEnvVars(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "hook.js")
	os.WriteFile(script, []byte("console.log('FOO=bar');\nconsole.log('BAZ=qux=with=equals');\n"), 0755)

	env, err := RunHook("hook.js", dir, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"FOO=bar", "BAZ=qux=with=equals"}
	if !reflect.DeepEqual(env, want) {
		t.Errorf("got %v, want %v", env, want)
	}
}

func TestRunHook_IgnoresNonEnvLines(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "hook.js")
	os.WriteFile(script, []byte("console.log('# comment');\nconsole.log('');\nconsole.log('KEY=value');\n"), 0755)

	env, err := RunHook("hook.js", dir, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(env) != 1 || env[0] != "KEY=value" {
		t.Errorf("got %v, want [KEY=value]", env)
	}
}

func TestRunHook_ErrorOnNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "hook.js")
	os.WriteFile(script, []byte("process.exit(1);\n"), 0755)

	_, err := RunHook("hook.js", dir, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// A hook in a language papi no longer supports must fail loudly rather than
// falling back to a shell.
func TestRunHook_UnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "hook.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho 'FOO=bar'\n"), 0755)

	_, err := RunHook("hook.sh", dir, nil, nil)
	if err == nil {
		t.Fatal("expected error for .sh hook, got nil")
	}
}
