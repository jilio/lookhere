package lookhere

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	eventbus "github.com/jilio/ebu"
)

// ValidateDSN validates a DSN without creating an option.
// Use this to validate DSNs before passing them to WithCloud or MustWithCloud.
//
// Returns nil if the DSN is valid, or an error describing what's wrong.
//
// Example:
//
//	if err := lookhere.ValidateDSN(dsn); err != nil {
//	    log.Fatalf("invalid DSN: %v", err)
//	}
//	bus := eventbus.New(lookhere.WithCloud(dsn))
func ValidateDSN(dsn string) error {
	_, _, _, err := parseDSN(dsn)
	return err
}

// WithCloud returns an eventbus option that configures the EventBus to use
// a remote LOOKHERE cloud storage backend with automatic telemetry collection.
//
// The DSN format is: grpc://api-key@host?telemetry=true|false
//
// By default, telemetry is enabled and sends observability metrics to the
// lookhere SaaS platform. To disable telemetry, add ?telemetry=false to the DSN.
//
// IMPORTANT: This function panics if the DSN is invalid. For error handling,
// use ValidateDSN() first, or use this in contexts where panic is acceptable
// (e.g., initialization code with hardcoded DSNs).
//
// Example with validation:
//
//	if err := lookhere.ValidateDSN(dsn); err != nil {
//	    return fmt.Errorf("invalid DSN: %w", err)
//	}
//	bus := eventbus.New(lookhere.WithCloud(dsn))
//	defer bus.Shutdown(context.Background())
//
// Example without validation (panics on invalid DSN):
//
//	bus := eventbus.New(
//	    lookhere.WithCloud("grpc://V1StGXR8_Z5jdHi6B-myT@lookhere.tech"),
//	)
//	defer bus.Shutdown(context.Background())
//
// To disable telemetry:
//
//	bus := eventbus.New(
//	    lookhere.WithCloud("grpc://V1StGXR8_Z5jdHi6B-myT@lookhere.tech?telemetry=false"),
//	)
func WithCloud(dsn string) eventbus.Option {
	// Parse DSN upfront (will panic if invalid)
	apiKey, host, telemetryEnabled, err := parseDSN(dsn)
	if err != nil {
		panic(fmt.Sprintf("lookhere.WithCloud: invalid DSN: %v", err))
	}

	// Create shared HTTP client for both store and telemetry
	httpClient := createHTTPClient()

	// Create buffered EventStore (with batching)
	store := NewEventBuffer(httpClient, host, apiKey)

	// Return composite option
	return func(bus *eventbus.EventBus) {
		// Set the store
		eventbus.WithStore(store)(bus)

		// Enable telemetry if not disabled
		if telemetryEnabled {
			collector := NewTelemetryCollector(httpClient, host, apiKey)
			eventbus.WithObservability(collector)(bus)
		}
	}
}

// createHTTPClient creates a secure HTTP client with proper defaults
func createHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second, // Overall request timeout
		Transport: &http.Transport{
			MaxIdleConns:          100,              // Maximum idle connections
			MaxIdleConnsPerHost:   10,               // Maximum idle connections per host
			IdleConnTimeout:       90 * time.Second, // How long idle connections stay open
			TLSHandshakeTimeout:   10 * time.Second, // TLS handshake timeout
			ResponseHeaderTimeout: 10 * time.Second, // Time to wait for response headers
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12, // Enforce TLS 1.2+
			},
		},
	}
}

// parseDSN parses a DSN in the format grpc://api-key@host?telemetry=true|false
func parseDSN(dsn string) (apiKey, host string, telemetryEnabled bool, err error) {
	// Parse as URL
	u, err := url.Parse(dsn)
	if err != nil {
		return "", "", false, fmt.Errorf("invalid DSN: %w", err)
	}

	// Check scheme
	if u.Scheme != "grpc" {
		return "", "", false, fmt.Errorf("invalid scheme: expected grpc, got %s", u.Scheme)
	}

	// Extract API key from user info
	if u.User == nil || u.User.Username() == "" {
		return "", "", false, fmt.Errorf("missing API key in DSN")
	}

	apiKey = u.User.Username()

	// Extract host
	host = u.Host
	if host == "" {
		return "", "", false, fmt.Errorf("missing host in DSN")
	}

	// Check telemetry query parameter (default: enabled)
	telemetryEnabled = true
	if telemetryParam := u.Query().Get("telemetry"); telemetryParam != "" {
		telemetryEnabled = telemetryParam != "false"
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

	return apiKey, host, telemetryEnabled, nil
}
