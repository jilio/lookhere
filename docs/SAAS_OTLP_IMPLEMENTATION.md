# LOOKHERE SaaS OTLP Telemetry Implementation Guide

This document describes how to implement the OTLP (OpenTelemetry Protocol) trace collection endpoint in the LOOKHERE SaaS platform.

## Overview

The LOOKHERE client library now sends telemetry data as OTLP traces instead of custom protobuf messages. This makes the system compatible with standard observability tools like Jaeger, Grafana, and Prometheus.

## Architecture

```
┌─────────────────┐
│   EBU Library   │
│  (User's Code)  │
└────────┬────────┘
         │
         │ Observability Events
         │ (publish/handler/persist)
         ▼
┌─────────────────────────┐
│  LOOKHERE Client        │
│  TelemetryCollector     │
│  ├─ Generates OTLP      │
│  │  spans               │
│  └─ Batches & sends     │
└────────┬────────────────┘
         │
         │ OTLP Traces via ExportTrace RPC
         │ (HTTP/2 + Connect Protocol)
         ▼
┌─────────────────────────┐
│  LOOKHERE SaaS          │
│  ├─ ExportTrace         │
│  │  endpoint            │
│  ├─ Processes spans     │
│  └─ Forwards to         │
│     observability       │
│     backend             │
└────────┬────────────────┘
         │
         ▼
┌─────────────────────────┐
│  Observability Backend  │
│  (Jaeger/Grafana/etc)   │
└─────────────────────────┘
```

## Protocol Details

### RPC Endpoint

**Service**: `ebu.v1.EventService`
**Method**: `ExportTrace`
**Protocol**: Connect RPC (gRPC-compatible HTTP/2)
**Path**: `/ebu.v1.EventService/ExportTrace`

### Request Format

```protobuf
message ExportTraceServiceRequest {
  repeated ResourceSpans resource_spans = 1;
}

message ResourceSpans {
  Resource resource = 1;
  repeated ScopeSpans scope_spans = 2;
}

message Resource {
  repeated KeyValue attributes = 1;
}

message ScopeSpans {
  InstrumentationScope scope = 1;
  repeated Span spans = 2;
}

message Span {
  bytes trace_id = 1; // 16 bytes
  bytes span_id = 2; // 8 bytes
  bytes parent_span_id = 3; // 8 bytes (optional)

  string name = 4;
  SpanKind kind = 5;
  fixed64 start_time_unix_nano = 6;
  fixed64 end_time_unix_nano = 7;

  repeated KeyValue attributes = 8;
  Status status = 9;
}
```

### Response Format

```protobuf
message ExportTraceServiceResponse {
  ExportTracePartialSuccess partial_success = 1;
}

message ExportTracePartialSuccess {
  int64 rejected_spans = 1;
  string error_message = 2;
}
```

## Span Types and Attributes

The LOOKHERE client generates three types of spans, each with specific attributes:

### 1. Publish Spans

**Name**: `ebu.publish`
**Kind**: `SPAN_KIND_INTERNAL`

**Attributes**:
- `event.type` (string): The type of event being published (e.g., "user.created")

**Example**:
```json
{
  "trace_id": "5b8aa5a2d2c872e8321cf37308d69df2",
  "span_id": "051581bf3cb55c13",
  "name": "ebu.publish",
  "kind": "SPAN_KIND_INTERNAL",
  "start_time_unix_nano": "1699564800000000000",
  "end_time_unix_nano": "1699564800005000000",
  "attributes": [
    {
      "key": "event.type",
      "value": {"string_value": "user.created"}
    }
  ],
  "status": {"code": "STATUS_CODE_OK"}
}
```

### 2. Handler Spans

**Name**: `ebu.handler`
**Kind**: `SPAN_KIND_INTERNAL`

**Attributes**:
- `event.type` (string): The type of event being handled
- `handler.async` (bool): Whether the handler is async
- `error` (string, optional): Error message if handler failed

**Status**:
- `STATUS_CODE_OK`: Handler completed successfully
- `STATUS_CODE_ERROR`: Handler failed (status.message contains error)

**Example (Success)**:
```json
{
  "trace_id": "6c9bb6a3e3d983f9432dg48419e7aeg3",
  "span_id": "162692cg4dc66d24",
  "name": "ebu.handler",
  "kind": "SPAN_KIND_INTERNAL",
  "start_time_unix_nano": "1699564800010000000",
  "end_time_unix_nano": "1699564800015000000",
  "attributes": [
    {
      "key": "event.type",
      "value": {"string_value": "email.sent"}
    },
    {
      "key": "handler.async",
      "value": {"bool_value": false}
    }
  ],
  "status": {"code": "STATUS_CODE_OK"}
}
```

**Example (Error)**:
```json
{
  "trace_id": "7d0cc7b4f4e094ga543eh59520f8bfh4",
  "span_id": "273703dh5ed77e35",
  "name": "ebu.handler",
  "kind": "SPAN_KIND_INTERNAL",
  "start_time_unix_nano": "1699564800020000000",
  "end_time_unix_nano": "1699564800025000000",
  "attributes": [
    {
      "key": "event.type",
      "value": {"string_value": "webhook.failed"}
    },
    {
      "key": "handler.async",
      "value": {"bool_value": true}
    },
    {
      "key": "error",
      "value": {"string_value": "connection refused"}
    }
  ],
  "status": {
    "code": "STATUS_CODE_ERROR",
    "message": "connection refused"
  }
}
```

### 3. Persist Spans

**Name**: `ebu.persist`
**Kind**: `SPAN_KIND_INTERNAL`

**Attributes**:
- `event.type` (string): The type of event being persisted
- `event.position` (int64): The event's position in the stream
- `error` (string, optional): Error message if persistence failed

**Status**:
- `STATUS_CODE_OK`: Event persisted successfully
- `STATUS_CODE_ERROR`: Persistence failed

**Example**:
```json
{
  "trace_id": "8e1dd8c5g5f1a5hb654fi6a631g9cgi5",
  "span_id": "384814ei6fe88f46",
  "name": "ebu.persist",
  "kind": "SPAN_KIND_INTERNAL",
  "start_time_unix_nano": "1699564800030000000",
  "end_time_unix_nano": "1699564800040000000",
  "attributes": [
    {
      "key": "event.type",
      "value": {"string_value": "inventory.adjusted"}
    },
    {
      "key": "event.position",
      "value": {"int_value": 12345}
    }
  ],
  "status": {"code": "STATUS_CODE_OK"}
}
```

## Resource Attributes

All spans include resource-level attributes that identify the service:

```json
{
  "resource": {
    "attributes": [
      {
        "key": "service.name",
        "value": {"string_value": "ebu"}
      }
    ]
  }
}
```

## Instrumentation Scope

All spans include an instrumentation scope identifying the library:

```json
{
  "scope": {
    "name": "github.com/jilio/lookhere",
    "version": "1.0.0"
  }
}
```

## Authentication

All requests include an Authorization header:

```
Authorization: Bearer <api-key>
```

Extract the API key from the Authorization header and validate it before processing the request.

## Implementation Checklist

### 1. Implement the ExportTrace Endpoint

```go
func (s *SaaSServer) ExportTrace(
    ctx context.Context,
    req *connect.Request[ebuv1.ExportTraceServiceRequest],
) (*connect.Response[ebuv1.ExportTraceServiceResponse], error) {
    // 1. Extract and validate API key
    apiKey := extractBearerToken(req.Header().Get("Authorization"))
    if !isValidAPIKey(apiKey) {
        return nil, connect.NewError(connect.CodeUnauthenticated,
            errors.New("invalid API key"))
    }

    // 2. Extract customer/project from API key
    customerID := getCustomerID(apiKey)
    projectID := getProjectID(apiKey)

    // 3. Process spans
    for _, resourceSpans := range req.Msg.ResourceSpans {
        for _, scopeSpans := range resourceSpans.ScopeSpans {
            for _, span := range scopeSpans.Spans {
                // Store or forward span
                err := processSpan(ctx, customerID, projectID, span)
                if err != nil {
                    // Log error but continue processing other spans
                    log.Printf("failed to process span: %v", err)
                }
            }
        }
    }

    // 4. Return success (or partial success)
    return connect.NewResponse(&ebuv1.ExportTraceServiceResponse{}), nil
}
```

### 2. Store or Forward Spans

You have two main options:

#### Option A: Store in Database
Store spans directly in your observability database:

```go
func processSpan(ctx context.Context, customerID, projectID string, span *ebuv1.Span) error {
    return db.InsertSpan(ctx, &SpanRecord{
        CustomerID:  customerID,
        ProjectID:   projectID,
        TraceID:     hex.EncodeToString(span.TraceId),
        SpanID:      hex.EncodeToString(span.SpanId),
        Name:        span.Name,
        Kind:        span.Kind.String(),
        StartTime:   time.Unix(0, int64(span.StartTimeUnixNano)),
        EndTime:     time.Unix(0, int64(span.EndTimeUnixNano)),
        Attributes:  extractAttributes(span.Attributes),
        StatusCode:  span.Status.Code.String(),
        StatusMsg:   span.Status.Message,
    })
}
```

#### Option B: Forward to OTLP Backend
Forward spans to an existing OTLP collector (Jaeger, Grafana, etc):

```go
import (
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func initOTLPExporter() (*otlptracegrpc.Exporter, error) {
    return otlptracegrpc.New(
        context.Background(),
        otlptracegrpc.WithEndpoint("jaeger:4317"),
        otlptracegrpc.WithInsecure(),
    )
}

func processSpan(ctx context.Context, customerID, projectID string, span *ebuv1.Span) error {
    // Convert span to OpenTelemetry format
    // Add customer/project as span attributes
    // Forward to OTLP exporter
    return exporter.ExportSpans(ctx, []sdktrace.ReadOnlySpan{convertSpan(span, customerID, projectID)})
}
```

### 3. Enrich Spans with Customer Context

Add customer and project identifiers to spans before forwarding:

```go
func enrichSpanWithContext(span *ebuv1.Span, customerID, projectID string) {
    span.Attributes = append(span.Attributes,
        &ebuv1.KeyValue{
            Key: "customer.id",
            Value: &ebuv1.AnyValue{
                Value: &ebuv1.AnyValue_StringValue{
                    StringValue: customerID,
                },
            },
        },
        &ebuv1.KeyValue{
            Key: "project.id",
            Value: &ebuv1.AnyValue{
                Value: &ebuv1.AnyValue_StringValue{
                    StringValue: projectID,
                },
            },
        },
    )
}
```

### 4. Handle Partial Failures

If some spans fail to process, return partial success:

```go
func (s *SaaSServer) ExportTrace(
    ctx context.Context,
    req *connect.Request[ebuv1.ExportTraceServiceRequest],
) (*connect.Response[ebuv1.ExportTraceServiceResponse], error) {
    var rejectedSpans int64
    var errorMessages []string

    for _, resourceSpans := range req.Msg.ResourceSpans {
        for _, scopeSpans := range resourceSpans.ScopeSpans {
            for _, span := range scopeSpans.Spans {
                err := processSpan(ctx, customerID, projectID, span)
                if err != nil {
                    rejectedSpans++
                    errorMessages = append(errorMessages, err.Error())
                }
            }
        }
    }

    resp := &ebuv1.ExportTraceServiceResponse{}
    if rejectedSpans > 0 {
        resp.PartialSuccess = &ebuv1.ExportTracePartialSuccess{
            RejectedSpans: rejectedSpans,
            ErrorMessage:  strings.Join(errorMessages, "; "),
        }
    }

    return connect.NewResponse(resp), nil
}
```

### 5. Monitoring and Observability

Track these metrics for the ExportTrace endpoint:

- **Request rate**: Requests per second
- **Span rate**: Spans processed per second
- **Error rate**: Failed requests / rejected spans
- **Latency**: P50, P95, P99 processing time
- **Customer activity**: Spans per customer

### 6. Rate Limiting

Implement rate limiting per customer:

```go
func (s *SaaSServer) ExportTrace(
    ctx context.Context,
    req *connect.Request[ebuv1.ExportTraceServiceRequest],
) (*connect.Response[ebuv1.ExportTraceServiceResponse], error) {
    apiKey := extractBearerToken(req.Header().Get("Authorization"))
    customerID := getCustomerID(apiKey)

    // Check rate limit
    if !s.rateLimiter.Allow(customerID) {
        return nil, connect.NewError(connect.CodeResourceExhausted,
            errors.New("rate limit exceeded"))
    }

    // ... process request
}
```

## Example: Full Implementation with Jaeger

```go
package saas

import (
    "context"
    "encoding/hex"
    "errors"
    "strings"

    "connectrpc.com/connect"
    ebuv1 "github.com/jilio/lookhere/gen/ebu/v1"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/attribute"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/trace"
)

type SaaSServer struct {
    exporter *otlptracegrpc.Exporter
}

func NewSaaSServer(jaegerEndpoint string) (*SaaSServer, error) {
    exporter, err := otlptracegrpc.New(
        context.Background(),
        otlptracegrpc.WithEndpoint(jaegerEndpoint),
        otlptracegrpc.WithInsecure(),
    )
    if err != nil {
        return nil, err
    }

    return &SaaSServer{exporter: exporter}, nil
}

func (s *SaaSServer) ExportTrace(
    ctx context.Context,
    req *connect.Request[ebuv1.ExportTraceServiceRequest],
) (*connect.Response[ebuv1.ExportTraceServiceResponse], error) {
    // Authenticate
    apiKey := extractBearerToken(req.Header().Get("Authorization"))
    if !isValidAPIKey(apiKey) {
        return nil, connect.NewError(connect.CodeUnauthenticated,
            errors.New("invalid API key"))
    }

    customerID := getCustomerID(apiKey)
    projectID := getProjectID(apiKey)

    // Process all spans
    var rejectedSpans int64
    var errorMessages []string

    for _, resourceSpans := range req.Msg.ResourceSpans {
        for _, scopeSpans := range resourceSpans.ScopeSpans {
            for _, span := range scopeSpans.Spans {
                // Enrich with customer context
                enrichedSpan := enrichSpan(span, customerID, projectID)

                // Convert to OTEL format and export
                otelSpan := convertToOTELSpan(enrichedSpan)
                err := s.exporter.ExportSpans(ctx, []sdktrace.ReadOnlySpan{otelSpan})
                if err != nil {
                    rejectedSpans++
                    errorMessages = append(errorMessages, err.Error())
                }
            }
        }
    }

    // Build response
    resp := &ebuv1.ExportTraceServiceResponse{}
    if rejectedSpans > 0 {
        resp.PartialSuccess = &ebuv1.ExportTracePartialSuccess{
            RejectedSpans: rejectedSpans,
            ErrorMessage:  strings.Join(errorMessages, "; "),
        }
    }

    return connect.NewResponse(resp), nil
}

func extractBearerToken(authHeader string) string {
    if strings.HasPrefix(authHeader, "Bearer ") {
        return strings.TrimPrefix(authHeader, "Bearer ")
    }
    return ""
}

func enrichSpan(span *ebuv1.Span, customerID, projectID string) *ebuv1.Span {
    // Clone span to avoid mutation
    enriched := &ebuv1.Span{
        TraceId:           span.TraceId,
        SpanId:            span.SpanId,
        ParentSpanId:      span.ParentSpanId,
        Name:              span.Name,
        Kind:              span.Kind,
        StartTimeUnixNano: span.StartTimeUnixNano,
        EndTimeUnixNano:   span.EndTimeUnixNano,
        Status:            span.Status,
    }

    // Add customer context
    enriched.Attributes = append(span.Attributes,
        &ebuv1.KeyValue{
            Key: "customer.id",
            Value: &ebuv1.AnyValue{
                Value: &ebuv1.AnyValue_StringValue{StringValue: customerID},
            },
        },
        &ebuv1.KeyValue{
            Key: "project.id",
            Value: &ebuv1.AnyValue{
                Value: &ebuv1.AnyValue_StringValue{StringValue: projectID},
            },
        },
    )

    return enriched
}

func convertToOTELSpan(span *ebuv1.Span) sdktrace.ReadOnlySpan {
    // Convert OTLP span to OpenTelemetry SDK span
    // Implementation depends on your OTEL SDK version
    // See: https://opentelemetry.io/docs/specs/otlp/

    // This is a simplified example - you'll need to implement
    // full conversion logic based on your OTEL SDK version
    panic("implement OTLP to OTEL conversion")
}
```

## Testing

Use the provided mock services in the test file to verify your implementation:

```go
// See telemetry_collector_test.go for examples of:
// - mockOTLPService: Basic successful handling
// - mockOTLPServiceWithPartialFailure: Partial success handling
// - mockOTLPServiceWithError: Error handling
// - mockOTLPServiceWithAuthCapture: Authentication verification
```

## Migration Notes

### Breaking Changes from Previous Implementation

The old `ReportTelemetry` streaming RPC has been replaced with `ExportTrace` unary RPC:

**Old (Deprecated)**:
```protobuf
rpc ReportTelemetry(stream TelemetryBatch) returns (TelemetryResponse);
```

**New**:
```protobuf
rpc ExportTrace(ExportTraceServiceRequest) returns (ExportTraceServiceResponse);
```

### Client Behavior

- Clients batch up to 100 spans before sending
- Flush interval: 10 seconds
- Worker pool size: 10 concurrent requests
- Backpressure: Drops batches when queue is full (queue size: 100)

## Support

For questions or issues:
- Check the protobuf definitions: `proto/ebu/v1/events.proto`
- Review client implementation: `telemetry_collector.go`
- See test examples: `telemetry_collector_test.go`
