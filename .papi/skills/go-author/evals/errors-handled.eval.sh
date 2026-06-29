#!/usr/bin/env bash
set -euo pipefail

EVAL_ID="errors-handled"
EVAL_NAME="Handle errors explicitly"

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

ERR_CHECKS=$(printf '%s' "$SRC" | grep -cE 'if +err +!= +nil' || true)
PANICS=$(printf '%s' "$SRC" | grep -cE '\bpanic\(' || true)
# Common stdlib calls that return an error and therefore demand a check.
ERR_OPS=$(printf '%s' "$SRC" | grep -cE 'os\.(Open|Create|ReadFile|WriteFile|Stat|Remove)|strconv\.(Atoi|Parse)|json\.(Marshal|Unmarshal)|http\.(Get|Post|Do|NewRequest)|\.Read\(|\.Write\(|\.Scan\(|\.Decode\(|\.Encode\(' || true)

if [ "$ERR_OPS" -eq 0 ] && [ "$ERR_CHECKS" -eq 0 ]; then
	result "1.0" "No error-returning operations to handle."
	exit 0
fi

if [ "$ERR_CHECKS" -ge 1 ] && [ "$PANICS" -eq 0 ]; then
	result "1.0" "Errors are checked with 'if err != nil' and not swallowed by panic."
elif [ "$ERR_CHECKS" -ge 1 ] && [ "$PANICS" -ge 1 ]; then
	result "0.7" "Errors are checked, but panic() is used where a returned error is preferable."
elif [ "$ERR_CHECKS" -eq 0 ] && [ "$ERR_OPS" -ge 1 ]; then
	result "0.2" "Error-returning calls present but no 'if err != nil' checks found."
else
	result "0.5" "Error handling is unclear."
fi
