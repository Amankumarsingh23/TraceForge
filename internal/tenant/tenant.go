// Package tenant defines trusted local ownership bindings, never payload routing.
package tenant

import (
	"fmt"
	"os"
)

func Valid(id string) bool { return id == "legacy" || id == "alpha" || id == "beta" }
func Current() (string, error) {
	id := os.Getenv("TRACEFORGE_TRUSTED_TENANT")
	if id == "" {
		id = "legacy"
	}
	if !Valid(id) {
		return "", fmt.Errorf("invalid trusted tenant")
	}
	return id, nil
}
func Schema(id string) string {
	if id == "legacy" {
		return "public"
	}
	return "tf_tenant_" + id
}
func Database(id string) string {
	if id == "legacy" {
		return "traceforge"
	}
	return "tf_tenant_" + id
}
