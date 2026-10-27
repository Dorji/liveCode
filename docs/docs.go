// Package docs embeds the OpenAPI specification so the running service can
// serve it (and a Swagger UI) without depending on the filesystem layout.
package docs

import _ "embed"

// OpenAPI is the full OpenAPI 3.0 specification of the service.
//
//go:embed openapi.yaml
var OpenAPI []byte
