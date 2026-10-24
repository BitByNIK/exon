# Dev image for exon: Go plus the tools used by make, editors and coding
# agents (omp) through LSP and DAP. Versions are pinned; bump them here.
FROM mcr.microsoft.com/devcontainers/go:2-1.27-trixie

ARG GOFUMPT_VERSION=v0.12.0
ARG GOLANGCI_LINT_VERSION=v2.15.0
ARG GOPLS_VERSION=v0.24.0
ARG DELVE_VERSION=v1.27.2
ARG GOLANGCI_LINT_LANGSERVER_VERSION=v0.0.12

# Installed into /go/bin, where the base image keeps its own (unpinned) copies
# of gopls, dlv and golangci-lint: ours replace them, and /go/bin is first on
# PATH. The install runs as root, so its caches are removed afterwards: a
# root-owned /go/pkg would stop the vscode user from downloading modules.
RUN go install mvdan.cc/gofumpt@${GOFUMPT_VERSION} \
 && go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION} \
 && go install golang.org/x/tools/gopls@${GOPLS_VERSION} \
 && go install github.com/go-delve/delve/cmd/dlv@${DELVE_VERSION} \
 && go install github.com/nametake/golangci-lint-langserver@${GOLANGCI_LINT_LANGSERVER_VERSION} \
 && go clean -cache -modcache \
 && rm -rf /go/pkg
