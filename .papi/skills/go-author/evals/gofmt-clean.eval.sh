#!/usr/bin/env bash
set -euo pipefail

EVAL_ID="gofmt-clean"
EVAL_NAME="Go source parses and is gofmt-clean"

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

# Collect the .go files Claude wrote to disk.
GO_FILES=$(find "$WORKDIR" -type f -name '*.go' 2>/dev/null || true)
if [ -z "$GO_FILES" ]; then
  result "0.0" "No Go files written to disk in the work directory."
  exit 0
fi

# gofmt -e parses (reporting syntax errors) and -l lists files that are not
# gofmt-formatted. No go.mod is required for either.
FMT_ERR=$(printf '%s\n' "$GO_FILES" | xargs gofmt -e -l 2>&1 >/dev/null || true)
if [ -n "$FMT_ERR" ]; then
  ERR=$(printf '%s' "$FMT_ERR" | head -c 300)
  result "0.2" "Go source does not parse: $ERR"
  exit 0
fi

UNFORMATTED=$(printf '%s\n' "$GO_FILES" | xargs gofmt -l 2>/dev/null || true)
if [ -n "$UNFORMATTED" ]; then
  result "0.7" "Valid Go, but not gofmt-clean: $(printf '%s' "$UNFORMATTED" | tr '\n' ' ')"
  exit 0
fi

result "1.0" "Go source parses and is gofmt-clean."
