#!/usr/bin/env bash
set -euo pipefail

cd frontend
vp build --outDir ./dist/  --emptyOutDir

