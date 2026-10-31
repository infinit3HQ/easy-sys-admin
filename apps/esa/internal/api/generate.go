// Package api serves /api/v1. Handlers are generated from
// packages/api/openapi.yaml, so the Go server and the TypeScript client
// cannot disagree about paths or payloads.
package api

//go:generate go tool oapi-codegen -config oapi-codegen.yaml ../../../../packages/api/openapi.yaml
