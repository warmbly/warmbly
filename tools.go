//go:build tools
// +build tools

// Package tools tracks code generator dependencies used by this repository.
// Versions are pinned in the root Makefile and installed via `make setup-tools`.
// Lint runs from its standalone module to isolate its developer dependencies.
package tools

import (
	_ "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
)
