//go:build tools
// +build tools

// Package tools tracks CLI tool dependencies used by this repository.
// Versions are pinned in the root Makefile and installed via `make setup-tools`.
package tools

import (
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
	_ "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
)
