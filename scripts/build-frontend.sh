#!/usr/bin/env bash
set -euo pipefail

cd frontend
pnpm vite build --outDir ./dist/  --emptyOutDir

