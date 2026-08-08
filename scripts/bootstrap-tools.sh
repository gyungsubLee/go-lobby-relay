#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tools_dir="$repo_root/.tools"
go_dir="$tools_dir/go"
buf_bin="$tools_dir/bin/buf"

if [ "$(uname -s)" != "Darwin" ] || [ "$(uname -m)" != "arm64" ]; then
  echo "unsupported bootstrap host: expected Darwin/arm64" >&2
  exit 1
fi

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/relay-tools.XXXXXX")
trap 'rm -rf -- "$tmp_dir"' EXIT HUP INT TERM

if [ ! -x "$go_dir/bin/go" ] || ! "$go_dir/bin/go" version | grep -Fxq 'go version go1.26.5 darwin/arm64'; then
  curl -fL --retry 3 -o "$tmp_dir/go.tar.gz" https://go.dev/dl/go1.26.5.darwin-arm64.tar.gz
  printf '%s  %s\n' efb87ff28af9a188d0536ef5d42e63dd52ba8263cd7344a993cc48dd11dedb6a "$tmp_dir/go.tar.gz" | shasum -a 256 -c -
  tar -C "$tmp_dir" -xzf "$tmp_dir/go.tar.gz"
  rm -rf -- "$go_dir"
  mkdir -p "$tools_dir"
  mv "$tmp_dir/go" "$go_dir"
fi

if [ ! -x "$buf_bin" ] || [ "$("$buf_bin" --version)" != "1.72.0" ]; then
  curl -fL --retry 3 -o "$tmp_dir/buf" https://github.com/bufbuild/buf/releases/download/v1.72.0/buf-Darwin-arm64
  printf '%s  %s\n' 5176f23a6118b9978de1340c3e3301a4ed0d48e16a669510be44b4c355170d57 "$tmp_dir/buf" | shasum -a 256 -c -
  mkdir -p "$(dirname "$buf_bin")"
  install -m 0755 "$tmp_dir/buf" "$buf_bin"
fi

"$go_dir/bin/go" version
"$buf_bin" --version
