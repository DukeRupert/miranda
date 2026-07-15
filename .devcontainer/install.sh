#!/bin/bash
set -e

echo "Installing Mage..."
go install github.com/magefile/mage@latest

echo "Installing golangci-lint..."
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

echo "Installing templ..."
go install github.com/a-h/templ/cmd/templ@latest

echo "Installing goose..."
go install github.com/pressly/goose/v3/cmd/goose@latest

echo "Installing sqlc..."
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

echo "Installing Claude Code..."
npm install -g @anthropic-ai/claude-code

echo "Installing Impeccable plugin..."
claude plugin marketplace add pbakaus/impeccable
claude plugin install impeccable

echo "Done."