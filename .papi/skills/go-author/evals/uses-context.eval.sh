#!/usr/bin/env bash
set -euo pipefail

EVAL_ID="uses-context"
EVAL_NAME="Use context.Context for cancellation and deadlines"

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

CTX_TYPE=$(printf '%s' "$SRC" | grep -cE 'context\.Context' || true)
CTX_CTOR=$(printf '%s' "$SRC" | grep -cE 'context\.(WithCancel|WithTimeout|WithDeadline|Background|TODO)' || true)
CTX_DONE=$(printf '%s' "$SRC" | grep -cE '\.Done\(\)' || true)
# Does the code do long-running, blocking, or concurrent work that should be cancellable?
CANCELLABLE=$(printf '%s' "$SRC" | grep -cE 'go +func|net/http|http\.(Get|Post|Do|Client|NewRequest)|time\.(Sleep|After|NewTimer|NewTicker)|sync\.WaitGroup|select *\{|<-' || true)

if [ "$CANCELLABLE" -eq 0 ]; then
	result "1.0" "No long-running or cancellable work that requires a context."
	exit 0
fi

if [ "$CTX_TYPE" -ge 1 ] && { [ "$CTX_CTOR" -ge 1 ] || [ "$CTX_DONE" -ge 1 ]; }; then
	result "1.0" "Uses context.Context and honors cancellation/deadlines."
elif [ "$CTX_TYPE" -ge 1 ]; then
	result "0.7" "Accepts context.Context but does not clearly honor Done()/timeouts."
else
	result "0.2" "Cancellable work present but no context.Context is used."
fi
