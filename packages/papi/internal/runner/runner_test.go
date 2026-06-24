package runner

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRunHook_ParsesEnvVars(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "hook.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho 'FOO=bar'\necho 'BAZ=qux=with=equals'\n"), 0755)

	env, err := RunHook("hook.sh", dir, nil)
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
	script := filepath.Join(dir, "hook.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho '# comment'\necho ''\necho 'KEY=value'\n"), 0755)

	env, err := RunHook("hook.sh", dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(env) != 1 || env[0] != "KEY=value" {
		t.Errorf("got %v, want [KEY=value]", env)
	}
}

func TestRunHook_ErrorOnNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "hook.sh")
	os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0755)

	_, err := RunHook("hook.sh", dir, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
