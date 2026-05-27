#!/usr/bin/env bash
set -euo pipefail

INPUT=$(cat)
INVOKED=$(printf '%s' "$INPUT" | jq -r '.invoked')
TRANSCRIPT=$(printf '%s' "$INPUT" | jq -r '.qualityTranscript // ""')

if [ "$INVOKED" != "true" ] || [ -z "$TRANSCRIPT" ]; then
  echo '{"evalId":"uses-modules","name":"Prefer modules over raw resources","score":0.0,"reasoning":"Skipped — skill not invoked."}'
  exit 0
fi

HAS_MODULE=$(printf '%s' "$TRANSCRIPT" | grep -cE 'module\s+"' || true)
HAS_RAW=$(printf '%s' "$TRANSCRIPT" | grep -cE 'resource\s+"aws_' || true)

if   [ "$HAS_MODULE" -gt 0 ] && [ "$HAS_RAW" -eq 0 ]; then
  SCORE="1.0"; REASON="Code uses module blocks with no raw aws_* resources."
elif [ "$HAS_MODULE" -eq 0 ] && [ "$HAS_RAW" -gt 0 ]; then
  SCORE="0.1"; REASON="Code uses raw resource \"aws_*\" blocks instead of modules."
elif [ "$HAS_MODULE" -gt 0 ] && [ "$HAS_RAW" -gt 0 ]; then
  SCORE="0.4"; REASON="Code mixes module blocks and raw aws_* resources."
else
  SCORE="0.5"; REASON="Code present but no module or aws_* resource pattern detected."
fi

jq -n --arg score "$SCORE" --arg reason "$REASON" \
  '{"evalId":"uses-modules","name":"Prefer modules over raw resources","score":($score|tonumber),"reasoning":$reason}'
