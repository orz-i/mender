// Package configenv adapts explicitly mounted deployment secrets at the outer
// configuration boundary. It never mutates os.Environ or logs secret contents.
package configenv

import (
	"errors"
	"io"
	"os"
	"strings"
)

// SecretNames is deliberately closed: a _FILE suffix is not a generic way to
// override feature flags, executable paths, issuer URLs or egress policies.
var secretNames = []string{
	"MENDER_DATABASE_URL", "MENDER_ADMIN_DATABASE_URL", "MENDER_BROWSER_SESSION_DATABASE_URL",
	"MENDER_ADMISSION_DATABASE_URL", "MENDER_CANCELLATION_DATABASE_URL", "MENDER_CURSOR_SIGNING_KEY",
	"MENDER_CONNECTION_MANAGER_DATABASE_URL", "MENDER_CATALOG_MANAGER_DATABASE_URL",
	"MENDER_PUBLISHER_MANAGER_DATABASE_URL", "MENDER_RELEASE_MANAGER_DATABASE_URL",
	"MENDER_DANGEROUS_OPERATION_MANAGER_DATABASE_URL", "MENDER_SUPPORT_READER_DATABASE_URL",
	"MENDER_PLATFORM_ADMIN_MANAGER_DATABASE_URL", "MENDER_BILLING_MANAGER_DATABASE_URL",
	"MENDER_PAYMENT_MANAGER_DATABASE_URL", "MENDER_PAYMENT_CALLBACK_INGESTOR_DATABASE_URL",
	"MENDER_GOVERNANCE_REVIEWER_DATABASE_URL", "MENDER_GOVERNANCE_POLICY_MANAGER_DATABASE_URL",
	"MENDER_GOVERNANCE_EXECUTION_CONFIRMER_DATABASE_URL", "MENDER_COMMERCE_OBSERVER_DATABASE_URL",
	"MENDER_CALLBACK_INGESTOR_DATABASE_URL", "MENDER_CALLBACK_OBSERVER_DATABASE_URL",
	"MENDER_WORKER_DATABASE_URL", "MENDER_EXECUTOR_DATABASE_URL", "MENDER_MCP_CONNECTOR_DATABASE_URL",
	"MENDER_RECONCILER_DATABASE_URL", "MENDER_SETTLEMENT_DATABASE_URL", "MENDER_OAUTH_REFRESH_DATABASE_URL",
	"MENDER_ARTIFACT_MATERIALIZER_DATABASE_URL", "MENDER_ARTIFACT_OBJECT_READER_DATABASE_URL",
	"MENDER_AGENT_INPUT_DATABASE_URL", "MENDER_AGENT_INPUT_EXECUTOR_DATABASE_URL",
}

// Resolve snapshots file values. Supplying both VALUE and VALUE_FILE fails even
// if equal; rotating secrets requires a controlled process restart. Per-service
// secret mounts and host ACLs remain the operator's responsibility.
func Resolve(getenv func(string) string) (func(string) string, error) {
	values := map[string]string{}
	for _, name := range secretNames {
		path := getenv(name + "_FILE")
		if path == "" {
			continue
		}
		if getenv(name) != "" {
			return nil, errors.New(name + ": value and file are mutually exclusive")
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 16384 {
			return nil, errors.New(name + ": mounted secret is unavailable or invalid")
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, errors.New(name + ": mounted secret cannot be read")
		}
		opened, statErr := f.Stat()
		if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			_ = f.Close()
			return nil, errors.New(name + ": mounted secret changed during open")
		}
		b, readErr := io.ReadAll(io.LimitReader(f, 16385))
		_ = f.Close()
		value := strings.TrimSpace(string(b))
		if readErr != nil || len(b) > 16384 || value == "" || strings.ContainsAny(value, "\r\n\x00") {
			return nil, errors.New(name + ": mounted secret content is invalid")
		}
		values[name] = value
	}
	return func(name string) string {
		if value, ok := values[name]; ok {
			return value
		}
		if strings.HasSuffix(name, "_FILE") {
			if _, ok := values[strings.TrimSuffix(name, "_FILE")]; ok {
				return ""
			}
		}
		return getenv(name)
	}, nil
}
