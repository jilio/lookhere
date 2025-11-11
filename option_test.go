package lookhere

import (
	"testing"

	eventbus "github.com/jilio/ebu"
)

func TestParseDSN_Valid(t *testing.T) {
	tests := []struct {
		name                  string
		dsn                   string
		expectedAPIKey        string
		expectedHost          string
		expectedTelemetry     bool
	}{
		{
			name:              "grpc with domain",
			dsn:               "grpc://V1StGXR8_Z5jdHi6B-myT@lookhere.tech",
			expectedAPIKey:    "V1StGXR8_Z5jdHi6B-myT",
			expectedHost:      "https://lookhere.tech",
			expectedTelemetry: true, // default enabled
		},
		{
			name:              "grpc with localhost",
			dsn:               "grpc://test-key@localhost:8080",
			expectedAPIKey:    "test-key",
			expectedHost:      "http://localhost:8080",
			expectedTelemetry: true,
		},
		{
			name:              "grpc with 127.0.0.1",
			dsn:               "grpc://test-key@127.0.0.1:8080",
			expectedAPIKey:    "test-key",
			expectedHost:      "http://127.0.0.1:8080",
			expectedTelemetry: true,
		},
		{
			name:              "grpc with subdomain",
			dsn:               "grpc://api-key-123@api.lookhere.tech",
			expectedAPIKey:    "api-key-123",
			expectedHost:      "https://api.lookhere.tech",
			expectedTelemetry: true,
		},
		{
			name:              "grpc with port",
			dsn:               "grpc://key@example.com:443",
			expectedAPIKey:    "key",
			expectedHost:      "https://example.com:443",
			expectedTelemetry: true,
		},
		{
			name:              "telemetry explicitly enabled",
			dsn:               "grpc://key@lookhere.tech?telemetry=true",
			expectedAPIKey:    "key",
			expectedHost:      "https://lookhere.tech",
			expectedTelemetry: true,
		},
		{
			name:              "telemetry disabled",
			dsn:               "grpc://key@lookhere.tech?telemetry=false",
			expectedAPIKey:    "key",
			expectedHost:      "https://lookhere.tech",
			expectedTelemetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiKey, host, telemetryEnabled, err := parseDSN(tt.dsn)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if apiKey != tt.expectedAPIKey {
				t.Errorf("expected apiKey %q, got %q", tt.expectedAPIKey, apiKey)
			}
			if host != tt.expectedHost {
				t.Errorf("expected host %q, got %q", tt.expectedHost, host)
			}
			if telemetryEnabled != tt.expectedTelemetry {
				t.Errorf("expected telemetry %v, got %v", tt.expectedTelemetry, telemetryEnabled)
			}
		})
	}
}

func TestParseDSN_Invalid(t *testing.T) {
	tests := []struct {
		name            string
		dsn             string
		expectedErrPart string
	}{
		{
			name:            "empty DSN",
			dsn:             "",
			expectedErrPart: "invalid scheme",
		},
		{
			name:            "missing scheme",
			dsn:             "api-key@lookhere.tech",
			expectedErrPart: "invalid scheme",
		},
		{
			name:            "wrong scheme",
			dsn:             "http://api-key@lookhere.tech",
			expectedErrPart: "invalid scheme",
		},
		{
			name:            "missing API key",
			dsn:             "grpc://@lookhere.tech",
			expectedErrPart: "missing API key",
		},
		{
			name:            "missing host",
			dsn:             "grpc://api-key@",
			expectedErrPart: "missing host",
		},
		{
			name:            "only scheme",
			dsn:             "grpc://",
			expectedErrPart: "missing API key",
		},
		{
			name:            "malformed URL",
			dsn:             "grpc://[::1]:namedport",
			expectedErrPart: "invalid DSN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := parseDSN(tt.dsn)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			// Just check that error contains the expected part
			if !containsString(err.Error(), tt.expectedErrPart) {
				t.Errorf("expected error containing %q, got %q", tt.expectedErrPart, err.Error())
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestWithCloud_ValidDSN(t *testing.T) {
	// Test with valid DSN - should not panic
	dsn := "grpc://test-key@lookhere.tech"
	option := WithCloud(dsn)

	if option == nil {
		t.Fatal("expected non-nil option")
	}

	// Verify option can be applied to EventBus without error
	bus := eventbus.New(option)
	if bus == nil {
		t.Fatal("expected non-nil EventBus")
	}
}

func TestWithCloud_InvalidDSN_Panics(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
	}{
		{
			name: "empty DSN",
			dsn:  "",
		},
		{
			name: "wrong scheme",
			dsn:  "http://key@host",
		},
		{
			name: "missing API key",
			dsn:  "grpc://@host",
		},
		{
			name: "missing host",
			dsn:  "grpc://key@",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("expected panic, got none")
				}
				// Verify panic message contains expected text
				panicMsg, ok := r.(string)
				if !ok {
					t.Fatalf("expected string panic, got %T", r)
				}
				if len(panicMsg) == 0 {
					t.Fatal("expected non-empty panic message")
				}
				// Check that panic message mentions "lookhere.WithCloud"
				if !containsString(panicMsg, "lookhere.WithCloud") {
					t.Errorf("expected panic message to contain 'lookhere.WithCloud', got %q", panicMsg)
				}
			}()

			WithCloud(tt.dsn)
		})
	}
}

func TestWithCloud_ReturnsEventBusOption(t *testing.T) {
	dsn := "grpc://test-key@lookhere.tech"
	option := WithCloud(dsn)

	// Verify the option is actually an eventbus.Option
	var _ eventbus.Option = option

	// Create an EventBus with the option to ensure it's valid
	bus := eventbus.New(option)
	if bus == nil {
		t.Fatal("expected non-nil EventBus from WithCloud option")
	}
}

func TestParseDSN_SchemeInference(t *testing.T) {
	tests := []struct {
		name         string
		host         string
		expectedHTTP string
	}{
		{
			name:         "localhost uses http",
			host:         "localhost:8080",
			expectedHTTP: "http",
		},
		{
			name:         "127.0.0.1 uses http",
			host:         "127.0.0.1:8080",
			expectedHTTP: "http",
		},
		{
			name:         "domain uses https",
			host:         "lookhere.tech",
			expectedHTTP: "https",
		},
		{
			name:         "subdomain uses https",
			host:         "api.lookhere.tech",
			expectedHTTP: "https",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn := "grpc://key@" + tt.host
			_, fullHost, _, err := parseDSN(dsn)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if tt.expectedHTTP == "http" {
				if !containsString(fullHost, "http://") {
					t.Errorf("expected http scheme, got %q", fullHost)
				}
			} else {
				if !containsString(fullHost, "https://") {
					t.Errorf("expected https scheme, got %q", fullHost)
				}
			}
		})
	}
}
