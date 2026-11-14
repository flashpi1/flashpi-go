//go:build tools

// Package tools pins the development tools of flashpi-go in a module of their own, so the SDK's
// go.mod never lists them. The import makes staticcheck a direct requirement, which Dependabot
// updates (it skips indirect ones, and does not read tool directives yet).
package tools

import _ "honnef.co/go/tools/cmd/staticcheck"
