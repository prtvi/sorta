#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT/web"
npm run build
cd "$ROOT"
rm -rf cmd/sorta/webdist/*
cp -R web/dist/* cmd/sorta/webdist/
# Ensure embed always has at least one file if dist was empty
touch cmd/sorta/webdist/.gitkeep
mkdir -p bin
go build -o bin/sorta ./cmd/sorta
echo "Built bin/sorta"
