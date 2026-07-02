#!/usr/bin/env bash
set -euo pipefail

EVAL_ID="prefers-composition"
EVAL_NAME="Prefer composition over inheritance"

result() {
	jq -n --arg score "$1" --arg reason "$2" \
		--arg id "$EVAL_ID" --arg name "$EVAL_NAME" \
		'{"evalId":$id,"name":$name,"score":($score|tonumber),"reasoning":$reason}'
}

INPUT=$(cat)
INVOKED=$(printf '%s' "$INPUT" | jq -r '.invoked')
WORKDIR=$(printf '%s' "$INPUT" | jq -r '.workDir // ""')
TRANSCRIPT=$(printf '%s' "$INPUT" | jq -r '.qualityTranscript // ""')

if [ "$INVOKED" != "true" ] || [ -z "$TRANSCRIPT" ]; then
	result "0.0" "Skipped — skill not invoked."
	exit 0
fi
if [ -z "$WORKDIR" ] || [ ! -d "$WORKDIR" ]; then
	result "0.0" "No work directory available to inspect."
	exit 0
fi

GO_FILES=$(find "$WORKDIR" -type f -name '*.go' 2>/dev/null || true)
if [ -z "$GO_FILES" ]; then
	result "0.0" "No Go files written to disk."
	exit 0
fi
SRC=$(printf '%s\n' "$GO_FILES" | xargs cat 2>/dev/null || true)

INTERFACES=$(printf '%s' "$SRC" | grep -cE 'interface *\{' || true)
STRUCTS=$(printf '%s' "$SRC" | grep -cE 'struct *\{' || true)
# Embedded fields: a line that is just a (possibly pointer/qualified) type name with
# no field name — the Go composition idiom (e.g. "io.Reader", "sync.Mutex", "*Base").
EMBED=$(printf '%s' "$SRC" | grep -cE '^[[:space:]]+\*?[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?[[:space:]]*$' || true)

if [ "$STRUCTS" -eq 0 ] && [ "$INTERFACES" -eq 0 ]; then
	result "1.0" "No type definitions to evaluate for composition."
	exit 0
fi

if [ "$INTERFACES" -ge 1 ] || [ "$EMBED" -ge 1 ]; then
	result "1.0" "Uses interfaces and/or struct embedding (composition over inheritance)."
elif [ "$STRUCTS" -ge 1 ]; then
	result "0.6" "Concrete structs only — favor small interfaces or embedding for composition."
else
	result "0.5" "Type design is unclear."
fi
