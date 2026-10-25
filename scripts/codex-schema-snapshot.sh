#!/usr/bin/env bash
# Writes the snapshot of the Codex app-server JSON schema that backend/internal/chatgpt/codex/protocol checks mam's
# message types against (the schema test). It holds the schema files mam's messages stand for, from the official
# release tag, the method names of the client requests, server requests and server notifications, and the config
# values mam passes to the app-server with -c.
# Usage: scripts/codex-schema-snapshot.sh <codex version> [OUTPUT]      e.g. 0.164.0
#   OUTPUT defaults to backend/internal/chatgpt/codex/protocol/testdata/schema.json.
# Run it when the pinned version of backend/internal/chatgpt/codex/catalog/data/catalog.json changes, then run the
# tests: a message mam uses that the new version changed fails protocol's schema test (spec/architecture/chatgpt-signin.md §8).
# The schema files are the upstream's (Apache-2.0, https://github.com/openai/codex), unchanged except that the parts mam
# does not read are dropped to keep the snapshot small. Needs: curl, jq.
set -euo pipefail

cd "$(dirname "$0")/.."
version="${1:?usage: codex-schema-snapshot.sh <codex version> [OUTPUT]}"
out="${2:-backend/internal/chatgpt/codex/protocol/testdata/schema.json}"
command -v jq >/dev/null || { echo "jq not found" >&2; exit 1; }
base="https://raw.githubusercontent.com/openai/codex/rust-v${version}/codex-rs/app-server-protocol/schema/json"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/mam-codex-schema-XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
get() { curl -fsSL --retry 3 "$base/$1" -o "$tmp/$(basename "$1")"; }

top_level=(v1/InitializeParams v1/InitializeResponse CommandExecutionRequestApprovalResponse FileChangeRequestApprovalResponse
  PermissionsRequestApprovalResponse McpServerElicitationRequestResponse ToolRequestUserInputResponse DynamicToolCallResponse
  ApplyPatchApprovalResponse ExecCommandApprovalResponse)
v2=(GetAccountParams GetAccountResponse LoginAccountParams LoginAccountResponse CancelLoginAccountParams
  AccountLoginCompletedNotification AccountUpdatedNotification GetAccountRateLimitsResponse
  AccountRateLimitsUpdatedNotification ModelListParams ModelListResponse ThreadStartParams ThreadStartResponse
  TurnStartParams TurnCompletedNotification ErrorNotification AgentMessageDeltaNotification ItemCompletedNotification
  ThreadUnsubscribeParams TurnInterruptParams)
for f in "${top_level[@]}"; do get "$f.json"; done
for f in "${v2[@]}"; do get "v2/$f.json"; done
get ClientRequest.json
get ServerRequest.json
get ServerNotification.json
curl -fsSL --retry 3 "https://raw.githubusercontent.com/openai/codex/rust-v${version}/codex-rs/core/config.schema.json" -o "$tmp/config.schema.json"

# What mam does not read is dropped: the thread items other than agent messages, and the turns inside a thread / turn.
prune() { # <name>
  case "$1" in
    ItemCompletedNotification) jq -c '.definitions.ThreadItem.oneOf |= map(select(.properties.type.enum[0] == "agentMessage"))' "$tmp/$1.json" ;;
    TurnCompletedNotification) jq -c '.definitions |= with_entries(select(.key | IN("Turn", "TurnError", "CodexErrorInfo", "TurnStatus"))) | .definitions.Turn.properties |= with_entries(select(.key | IN("id", "status", "error")))' "$tmp/$1.json" ;;
    ThreadStartResponse) jq -c '.definitions |= with_entries(select(.key == "Thread")) | .definitions.Thread.properties |= with_entries(select(.key == "id"))' "$tmp/$1.json" ;;
    *) jq -c . "$tmp/$1.json" ;;
  esac
}
methods() { jq -c '[.oneOf[].properties.method.enum[0]]' "$tmp/$1.json"; }

names=(InitializeParams InitializeResponse CommandExecutionRequestApprovalResponse FileChangeRequestApprovalResponse
  PermissionsRequestApprovalResponse McpServerElicitationRequestResponse ToolRequestUserInputResponse DynamicToolCallResponse
  ApplyPatchApprovalResponse ExecCommandApprovalResponse "${v2[@]}")
last=$((${#names[@]} - 1))
snapshot="$(
  echo "{"
  echo "\"tag\": \"rust-v${version}\","
  echo "\"clientRequests\": $(methods ClientRequest),"
  echo "\"serverRequests\": $(methods ServerRequest),"
  echo "\"serverNotifications\": $(methods ServerNotification),"
  echo "\"config\": $(jq -c '{AuthCredentialsStoreMode: .definitions.AuthCredentialsStoreMode, WebSearchMode: .definitions.WebSearchMode, keys: (.properties | with_entries(select(.key | IN("cli_auth_credentials_store", "web_search"))) | keys), features: (.properties.features.properties | with_entries(select(.key | IN("apps", "collab", "image_generation", "browser_use"))) | keys)}' "$tmp/config.schema.json"),"
  echo "\"schemas\": {"
  for i in "${!names[@]}"; do
    sep=","
    [[ $i -eq $last ]] && sep=""
    echo "\"${names[$i]}\": $(prune "${names[$i]}")${sep}"
  done
  echo "}"
  echo "}"
)"
printf '%s\n' "$snapshot" | jq -e . >/dev/null # valid JSON before it is written
mkdir -p "$(dirname "$out")"
printf '%s\n' "$snapshot" >"$out"
echo "wrote $out"
