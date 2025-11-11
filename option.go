package lookhere

import (
	"fmt"
	"net/url"
	"strings"

	eventbus "github.com/jilio/ebu"
)

// WithCloud returns an eventbus option that configures the EventBus to use
// a remote LOOKHERE cloud storage backend.
//
// The DSN format is: grpc://api-key@host
//
// Example:
//
//	bus := eventbus.New(
//	    lookhere.WithCloud("grpc://V1StGXR8_Z5jdHi6B-myT@lookhere.tech"),
//	)
//
// Note: If DSN parsing fails, this will panic. Validate your DSN before using it.
func WithCloud(dsn string) eventbus.Option {
	// Parse DSN upfront (will panic if invalid)
	apiKey, host, err := parseDSN(dsn)
	if err != nil {
		panic(fmt.Sprintf("lookhere.WithCloud: invalid DSN: %v", err))
	}

	// Create remote EventStore
	store := NewRemoteStore(host, apiKey)

	// Return the option that sets the EventStore
	return eventbus.WithStore(store)
}

// parseDSN parses a DSN in the format grpc://api-key@host
func parseDSN(dsn string) (apiKey, host string, err error) {
	// Parse as URL
	u, err := url.Parse(dsn)
	if err != nil {
		return "", "", fmt.Errorf("invalid DSN: %w", err)
	}

	// Check scheme
	if u.Scheme != "grpc" {
		return "", "", fmt.Errorf("invalid scheme: expected grpc, got %s", u.Scheme)
	}

	// Extract API key from user info
	if u.User == nil || u.User.Username() == "" {
		return "", "", fmt.Errorf("missing API key in DSN")
	}

	apiKey = u.User.Username()

	// Extract host
	host = u.Host
	if host == "" {
		return "", "", fmt.Errorf("missing host in DSN")
	}

	// Add scheme for HTTP client
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		// Use http:// for localhost, https:// for everything else
		if strings.HasPrefix(host, "localhost:") || strings.HasPrefix(host, "127.0.0.1:") {
			host = "http://" + host
		} else {
			host = "https://" + host
		}
	}

	return apiKey, host, nil
}
