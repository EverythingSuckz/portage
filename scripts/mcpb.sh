#!/bin/sh
#   scripts/mcpb.sh <version>
# Writes dist/portage-mcp_<version>.mcpb.
set -eu

version="${1:?usage: scripts/mcpb.sh <version>}"
mcpb="@anthropic-ai/mcpb@2.1.2"

windows=$(ls dist/portage_windows_amd64*/portage.exe)
darwin=$(ls dist/portage_darwin_all*/portage)

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/server"
cp "$windows" "$stage/server/portage.exe"
cp "$darwin" "$stage/server/portage"
chmod +x "$stage/server/portage"
cp LICENSE "$stage/"
jq --arg v "$version" '.version = $v' mcpb/manifest.json >"$stage/manifest.json"

out="dist/portage-mcp_${version}.mcpb"
npx -y "$mcpb" pack "$stage" "$out"
echo "Packed $out"
