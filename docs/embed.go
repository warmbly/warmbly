// Package docs embeds the customer docs and the OpenAPI spec, so Remie and the MCP server read the same pages and API surface customers do.
package docs

import "embed"

// Content holds every docs page under content/docs.
//
//go:embed content/docs
var Content embed.FS

// OpenAPI is the public API description served at docs.warmbly.com/openapi.json.
//
//go:embed public/openapi.json
var OpenAPI []byte
