# lookhere

[![Go Reference](https://pkg.go.dev/badge/github.com/jilio/lookhere.svg)](https://pkg.go.dev/github.com/jilio/lookhere)
[![Go Report Card](https://goreportcard.com/badge/github.com/jilio/lookhere)](https://goreportcard.com/report/github.com/jilio/lookhere)
[![Coverage](https://github.com/jilio/lookhere/wiki/coverage.svg)](https://raw.githack.com/wiki/jilio/lookhere/coverage.html)

Official Go client library for [lookhere](https://lookhere.tech) - Event sourcing as a service.

## Overview

lookhere provides cloud-based event storage and observability for Go applications using the [github.com/jilio/ebu](https://github.com/jilio/ebu) event sourcing library. This client library allows you to seamlessly persist your event-sourced applications to lookhere's managed infrastructure with automatic telemetry collection.

**Key Features:**
- ✅ Cloud-based event storage
- ✅ Automatic telemetry collection
- ✅ Zero-config observability
- ✅ Secure TLS 1.2+ connections
- ✅ Connection pooling and keep-alive

## Installation

```bash
go get github.com/jilio/lookhere
```

## Requirements

- Go 1.24.2 or later
- github.com/jilio/ebu v0.8.4 or later

## Quick Start

```go
package main

import (
    "context"
    "log"
    "time"

    eventbus "github.com/jilio/ebu"
    "github.com/jilio/lookhere"
)

func main() {
    // Connect to lookhere cloud storage
    dsn := "grpc://your-api-key@lookhere.tech"
    bus := eventbus.New(lookhere.WithCloud(dsn))

    // Use the EventBus as normal - events are automatically persisted
    // to lookhere cloud storage

    // Shutdown to flush events before exit
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    if err := bus.Shutdown(ctx); err != nil {
        log.Printf("shutdown error: %v", err)
    }
}
```

## DSN Format

The connection string (DSN) follows this format:

```
grpc://API_KEY@HOST[:PORT][?telemetry=true|false]
```

**Components:**
- `grpc://` - Required scheme
- `API_KEY` - Your lookhere API key (get one at https://lookhere.tech)
- `HOST` - lookhere server hostname (e.g., `lookhere.tech`)
- `PORT` - Optional port number
- `telemetry` - Optional query parameter to enable/disable telemetry (default: `true`)

**Examples:**
```go
// Production (telemetry enabled by default)
"grpc://V1StGXR8_Z5jdHi6B-myT@lookhere.tech"

// Production with telemetry explicitly disabled
"grpc://V1StGXR8_Z5jdHi6B-myT@lookhere.tech?telemetry=false"

// Development (localhost)
"grpc://test-key@localhost:8080"
```

## Usage Examples

### Basic Event Storage

```go
package main

import (
    "context"
    "log"
    "time"

    eventbus "github.com/jilio/ebu"
    "github.com/jilio/lookhere"
)

type UserCreated struct {
    UserID string
    Email  string
}

func main() {
    // Initialize EventBus with lookhere cloud storage
    dsn := "grpc://your-api-key@lookhere.tech"
    bus := eventbus.New(lookhere.WithCloud(dsn))

    // Publish events - they're automatically saved to the cloud
    ctx := context.Background()
    event := UserCreated{
        UserID: "user-123",
        Email:  "user@example.com",
    }

    if err := bus.Publish(ctx, event); err != nil {
        log.Fatalf("Failed to publish event: %v", err)
    }

    // Shutdown to flush remaining events
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    if err := bus.Shutdown(shutdownCtx); err != nil {
        log.Fatalf("Shutdown error: %v", err)
    }
}
```

### Error Handling

```go
func connectToLookhere(dsn string) (*eventbus.EventBus, error) {
    // Note: WithCloud panics on invalid DSN, so validate first
    defer func() {
        if r := recover(); r != nil {
            log.Printf("Invalid DSN: %v", r)
        }
    }()
    
    bus := eventbus.New(lookhere.WithCloud(dsn))
    return bus, nil
}
```

### With Context Timeouts

```go
import "time"

func publishWithTimeout(bus *eventbus.EventBus, event interface{}) error {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    return bus.Publish(ctx, event)
}
```

## Telemetry & Observability

**By default, lookhere automatically collects telemetry** from your EventBus and sends it to the lookhere SaaS platform. This provides you with zero-config observability into your event-sourced application.

### What Telemetry is Collected

The following metrics are automatically collected:

1. **Publish Metrics**
   - Event type
   - Publish duration
   - Timestamp

2. **Handler Metrics**
   - Event type
   - Handler execution duration
   - Async vs sync handlers
   - Error messages (if any)

3. **Persistence Metrics**
   - Event type
   - Event position in storage
   - Storage latency
   - Error messages (if any)

### Viewing Your Telemetry

All telemetry is available in your lookhere dashboard at https://lookhere.tech/dashboard

### Disabling Telemetry

If you need to disable telemetry (for privacy, compliance, or testing), add `?telemetry=false` to your DSN:

```go
// Telemetry disabled
dsn := "grpc://your-api-key@lookhere.tech?telemetry=false"
bus := eventbus.New(lookhere.WithCloud(dsn))
```

### Privacy & Data Retention

- **No sensitive data:** Telemetry does not include event payloads, only metadata
- **Secure transmission:** All telemetry is sent over TLS 1.2+
- **Data retention:** Telemetry is retained for 30 days by default
- **GDPR compliant:** See https://lookhere.tech/privacy for details

### Batching & Performance

Both events and telemetry are batched automatically to minimize network overhead:

**Event Batching:**
- **Batch size:** 100 events per batch
- **Flush interval:** 10 milliseconds
- **Automatic flushing:** Batches flush when size is reached or on interval
- **Worker pool:** 10 concurrent workers process batches
- **Backpressure:** Drops batches when queue is full (logs warning)

**Telemetry Batching:**
- **Batch size:** 100 metrics per batch
- **Flush interval:** 10 seconds
- **Async sending:** Telemetry sending doesn't block event processing
- **Failure handling:** Failed telemetry sends are logged but don't affect your app

## API Reference

### `WithCloud(dsn string) eventbus.Option`

Creates an EventBus option that configures cloud storage with automatic telemetry.

**Parameters:**
- `dsn` - Connection string in format `grpc://api-key@host[?telemetry=true|false]`

**Returns:**
- `eventbus.Option` - Configuration option for EventBus

**Panics:**
- If DSN format is invalid
- If API key is missing
- If host is missing

**Example:**
```go
option := lookhere.WithCloud("grpc://key@lookhere.tech")
bus := eventbus.New(option)
```

### `NewEventBuffer(httpClient *http.Client, baseURL, apiKey string) *EventBuffer`

Creates a buffered EventStore client with automatic batching (advanced usage).

**Parameters:**
- `httpClient` - HTTP client for making requests
- `baseURL` - Full URL to lookhere server (e.g., "https://lookhere.tech")
- `apiKey` - Your API key

**Returns:**
- `*EventBuffer` - Buffered EventStore implementation with batching

**Example:**
```go
buffer := lookhere.NewEventBuffer(http.DefaultClient, "https://lookhere.tech", "your-api-key")
bus := eventbus.New(eventbus.WithStore(buffer))
defer buffer.Close() // Important: close to flush remaining events
```

### `NewRemoteStore(host, apiKey string) *RemoteStore`

Creates a simple remote EventStore client without batching (advanced usage).

**Note:** For production use, prefer `WithCloud()` which uses `EventBuffer` with automatic batching.

**Parameters:**
- `host` - Full URL to lookhere server (e.g., "https://lookhere.tech")
- `apiKey` - Your API key

**Returns:**
- `*RemoteStore` - Simple EventStore implementation (no batching)

**Example:**
```go
store := lookhere.NewRemoteStore("https://lookhere.tech", "your-api-key")
bus := eventbus.New(eventbus.WithStore(store))
```

## Security Best Practices

### API Key Management

**Never commit API keys to version control:**

```go
// ❌ Bad - hardcoded
dsn := "grpc://V1StGXR8_Z5jdHi6B-myT@lookhere.tech"

// ✅ Good - from environment
dsn := os.Getenv("LOOKHERE_DSN")
if dsn == "" {
    log.Fatal("LOOKHERE_DSN environment variable is required")
}
```

### TLS Configuration

The client automatically:
- Enforces TLS 1.2+ for all connections (except localhost)
- Uses HTTPS for remote hosts
- Uses HTTP only for localhost/127.0.0.1

### Timeouts

The HTTP client includes secure defaults:
- **Request timeout:** 30 seconds
- **TLS handshake timeout:** 10 seconds
- **Response header timeout:** 10 seconds
- **Idle connection timeout:** 90 seconds

### Connection Limits

- **Max idle connections:** 100
- **Max idle connections per host:** 10

## Configuration Options

### Environment Variables

```bash
# Set your lookhere connection string
export LOOKHERE_DSN="grpc://your-api-key@lookhere.tech"
```

```go
dsn := os.Getenv("LOOKHERE_DSN")
bus := eventbus.New(lookhere.WithCloud(dsn))
```

## Troubleshooting

### "invalid scheme: expected grpc"

Ensure your DSN starts with `grpc://`:
```go
// ❌ Wrong
dsn := "https://api-key@lookhere.tech"

// ✅ Correct
dsn := "grpc://api-key@lookhere.tech"
```

### "missing API key in DSN"

The API key must come before the `@` symbol:
```go
// ❌ Wrong
dsn := "grpc://@lookhere.tech"

// ✅ Correct
dsn := "grpc://your-api-key@lookhere.tech"
```

### Connection Timeouts

If you're experiencing timeouts, check:
1. Your network connection
2. lookhere service status at https://status.lookhere.tech
3. Your API key is valid
4. No firewall blocking HTTPS traffic

### Context Cancellation

Always use contexts with timeouts to prevent hanging:
```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

err := bus.Publish(ctx, event)
```

## Performance Considerations

### Connection Pooling

The client maintains a connection pool with:
- Keep-alive connections enabled
- Automatic connection reuse
- Idle connection cleanup after 90 seconds

### Batching

For high-throughput scenarios, consider batching events:
```go
// Use EventBus's built-in batching capabilities
// See github.com/jilio/ebu documentation
```

## Development

### Running Tests

```bash
go test -v ./...
```

### Code Coverage

```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Building

```bash
go build ./...
```

## Support

- **Documentation:** https://docs.lookhere.tech
- **Issues:** https://github.com/jilio/lookhere/issues
- **Email:** support@lookhere.tech
- **Status Page:** https://status.lookhere.tech

## License

MIT License - see [LICENSE](LICENSE) file for details

## Related Projects

- [github.com/jilio/ebu](https://github.com/jilio/ebu) - Core event sourcing library
- [lookhere SaaS](https://lookhere.tech) - Managed event storage service
