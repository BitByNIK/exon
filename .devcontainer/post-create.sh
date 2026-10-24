#!/usr/bin/env bash
# Installs the Go tools used in this container: by make, and by editors and
# coding agents (omp) through LSP and DAP. Versions are pinned so every
# container gets the same tools; bump them here.
set -euo pipefail

tools=(
  mvdan.cc/gofumpt@v0.12.0                                         # formatter (make fmt)
  github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0   # linter (make lint)
  golang.org/x/tools/gopls@v0.24.0                                 # Go language server
  github.com/go-delve/delve/cmd/dlv@v1.27.2                        # Go debugger (DAP)
  github.com/nametake/golangci-lint-langserver@v0.0.12             # golangci-lint as a language server
)

for t in "${tools[@]}"; do
  echo "installing ${t}"
  go install "${t}"
done
