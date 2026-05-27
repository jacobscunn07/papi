#!/usr/bin/env sh
mkdir -p /tmp/terraform-plugin-cache
TF_PLUGIN_CACHE_DIR=/tmp/terraform-plugin-cache TF_INPUT=0 \
  terraform -chdir="$WORK_DIR" init -backend=false -no-color 2>&1 || true
