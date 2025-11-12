package lookhere

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	ebuv1 "github.com/jilio/lookhere/gen/ebu/v1"
	"github.com/jilio/lookhere/gen/ebu/v1/ebuv1connect"
)

func TestNewTelemetryCollector(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	if collector.batchSize != 100 {
		t.Errorf("expected batchSize 100, got %d", collector.batchSize)
	}

	if collector.interval != 10*time.Second {
		t.Errorf("expected interval 10s, got %v", collector.interval)
	}

	if collector.apiKey != "test-key" {
		t.Errorf("expected apiKey 'test-key', got %s", collector.apiKey)
	}

	if len(collector.spans) != 0 {
		t.Errorf("expected empty spans buffer, got %d items", len(collector.spans))
	}
}

func TestTelemetryCollector_OnPublishStart(t *testing.T) {
	collector := &TelemetryCollector{}
	ctx := context.Background()

	// Test that context is returned (we can't test internal keys)
	enrichedCtx := collector.OnPublishStart(ctx, "user.created")

	if enrichedCtx == nil {
		t.Fatal("expected non-nil context")
	}

	// The context should be different from the input
	if enrichedCtx == ctx {
		t.Error("expected enriched context to be different from input")
	}
}

func TestTelemetryCollector_OnPublishComplete(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Start and complete a publish
	ctx := context.Background()
	ctx = collector.OnPublishStart(ctx, "order.created")
	time.Sleep(5 * time.Millisecond)
	collector.OnPublishComplete(ctx, "order.created")

	// Small delay to ensure span is added
	time.Sleep(10 * time.Millisecond)

	// Check that span was added
	collector.mu.Lock()
	spansCount := len(collector.spans)
	var span *ebuv1.Span
	if spansCount > 0 {
		span = collector.spans[0]
	}
	collector.mu.Unlock()

	if spansCount != 1 {
		t.Fatalf("expected 1 span, got %d", spansCount)
	}

	if span.Name != "ebu.publish" {
		t.Errorf("expected span name 'ebu.publish', got %s", span.Name)
	}

	if span.Kind != ebuv1.SpanKind_SPAN_KIND_INTERNAL {
		t.Errorf("expected SPAN_KIND_INTERNAL, got %v", span.Kind)
	}

	if len(span.TraceId) != 16 {
		t.Errorf("expected 16-byte trace ID, got %d bytes", len(span.TraceId))
	}

	if len(span.SpanId) != 8 {
		t.Errorf("expected 8-byte span ID, got %d bytes", len(span.SpanId))
	}

	// Check attributes
	found := false
	for _, attr := range span.Attributes {
		if attr.Key == "event.type" {
			if attr.Value.GetStringValue() != "order.created" {
				t.Errorf("expected event.type='order.created', got %s", attr.Value.GetStringValue())
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("expected event.type attribute")
	}

	if span.Status.Code != ebuv1.StatusCode_STATUS_CODE_OK {
		t.Errorf("expected STATUS_CODE_OK, got %v", span.Status.Code)
	}
}

func TestTelemetryCollector_OnPublishComplete_NoContext(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Complete without start - should not panic
	ctx := context.Background()
	collector.OnPublishComplete(ctx, "test.event")

	// No span should be added
	collector.mu.Lock()
	defer collector.mu.Unlock()

	if len(collector.spans) != 0 {
		t.Errorf("expected 0 spans, got %d", len(collector.spans))
	}
}

func TestTelemetryCollector_OnHandlerStart(t *testing.T) {
	collector := &TelemetryCollector{}
	ctx := context.Background()

	// Test async handler
	enrichedCtx := collector.OnHandlerStart(ctx, "payment.processed", true)

	if enrichedCtx == nil {
		t.Fatal("expected non-nil context")
	}

	// The context should be different from the input
	if enrichedCtx == ctx {
		t.Error("expected enriched context to be different from input")
	}
}

func TestTelemetryCollector_OnHandlerComplete_Success(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Start and complete handler
	ctx := context.Background()
	ctx = collector.OnHandlerStart(ctx, "email.sent", false)
	time.Sleep(3 * time.Millisecond)
	collector.OnHandlerComplete(ctx, 3*time.Millisecond, nil)

	// Check span
	collector.mu.Lock()
	defer collector.mu.Unlock()

	if len(collector.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(collector.spans))
	}

	span := collector.spans[0]
	if span.Name != "ebu.handler" {
		t.Errorf("expected span name 'ebu.handler', got %s", span.Name)
	}

	// Check event.type attribute
	var foundEventType, foundAsync bool
	for _, attr := range span.Attributes {
		if attr.Key == "event.type" {
			if attr.Value.GetStringValue() != "email.sent" {
				t.Errorf("expected event.type='email.sent', got %s", attr.Value.GetStringValue())
			}
			foundEventType = true
		}
		if attr.Key == "handler.async" {
			if attr.Value.GetBoolValue() != false {
				t.Error("expected handler.async=false")
			}
			foundAsync = true
		}
	}
	if !foundEventType {
		t.Error("expected event.type attribute")
	}
	if !foundAsync {
		t.Error("expected handler.async attribute")
	}

	if span.Status.Code != ebuv1.StatusCode_STATUS_CODE_OK {
		t.Errorf("expected STATUS_CODE_OK, got %v", span.Status.Code)
	}
}

func TestTelemetryCollector_OnHandlerComplete_WithError(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Start and complete handler with error
	ctx := context.Background()
	ctx = collector.OnHandlerStart(ctx, "webhook.failed", true)
	testErr := errors.New("connection refused")
	collector.OnHandlerComplete(ctx, 1*time.Millisecond, testErr)

	// Check span
	collector.mu.Lock()
	defer collector.mu.Unlock()

	if len(collector.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(collector.spans))
	}

	span := collector.spans[0]

	// Check attributes
	var foundAsync, foundError bool
	for _, attr := range span.Attributes {
		if attr.Key == "handler.async" {
			if attr.Value.GetBoolValue() != true {
				t.Error("expected handler.async=true")
			}
			foundAsync = true
		}
		if attr.Key == "error" {
			if attr.Value.GetStringValue() != "connection refused" {
				t.Errorf("expected error='connection refused', got %s", attr.Value.GetStringValue())
			}
			foundError = true
		}
	}
	if !foundAsync {
		t.Error("expected handler.async attribute")
	}
	if !foundError {
		t.Error("expected error attribute")
	}

	if span.Status.Code != ebuv1.StatusCode_STATUS_CODE_ERROR {
		t.Errorf("expected STATUS_CODE_ERROR, got %v", span.Status.Code)
	}
	if span.Status.Message != "connection refused" {
		t.Errorf("expected status message 'connection refused', got %s", span.Status.Message)
	}
}

func TestTelemetryCollector_OnPersistStart(t *testing.T) {
	collector := &TelemetryCollector{}
	ctx := context.Background()

	// Test persist start
	enrichedCtx := collector.OnPersistStart(ctx, "user.updated", 42)

	if enrichedCtx == nil {
		t.Fatal("expected non-nil context")
	}

	// The context should be different from the input
	if enrichedCtx == ctx {
		t.Error("expected enriched context to be different from input")
	}
}

func TestTelemetryCollector_OnPersistComplete_Success(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Start and complete persist
	ctx := context.Background()
	ctx = collector.OnPersistStart(ctx, "inventory.adjusted", 123)
	time.Sleep(10 * time.Millisecond)
	collector.OnPersistComplete(ctx, 10*time.Millisecond, nil)

	// Check span
	collector.mu.Lock()
	defer collector.mu.Unlock()

	if len(collector.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(collector.spans))
	}

	span := collector.spans[0]
	if span.Name != "ebu.persist" {
		t.Errorf("expected span name 'ebu.persist', got %s", span.Name)
	}

	// Check attributes
	var foundEventType, foundPosition bool
	for _, attr := range span.Attributes {
		if attr.Key == "event.type" {
			if attr.Value.GetStringValue() != "inventory.adjusted" {
				t.Errorf("expected event.type='inventory.adjusted', got %s", attr.Value.GetStringValue())
			}
			foundEventType = true
		}
		if attr.Key == "event.position" {
			if attr.Value.GetIntValue() != 123 {
				t.Errorf("expected event.position=123, got %d", attr.Value.GetIntValue())
			}
			foundPosition = true
		}
	}
	if !foundEventType {
		t.Error("expected event.type attribute")
	}
	if !foundPosition {
		t.Error("expected event.position attribute")
	}

	if span.Status.Code != ebuv1.StatusCode_STATUS_CODE_OK {
		t.Errorf("expected STATUS_CODE_OK, got %v", span.Status.Code)
	}
}

func TestTelemetryCollector_OnPersistComplete_WithError(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Start and complete persist with error
	ctx := context.Background()
	ctx = collector.OnPersistStart(ctx, "data.corrupted", 999)
	testErr := errors.New("database connection lost")
	collector.OnPersistComplete(ctx, 5*time.Millisecond, testErr)

	// Check span
	collector.mu.Lock()
	defer collector.mu.Unlock()

	span := collector.spans[0]
	if span == nil {
		t.Fatal("expected span")
	}

	// Check error in attributes and status
	var foundError bool
	for _, attr := range span.Attributes {
		if attr.Key == "error" {
			if attr.Value.GetStringValue() != "database connection lost" {
				t.Errorf("expected error='database connection lost', got %s", attr.Value.GetStringValue())
			}
			foundError = true
		}
	}
	if !foundError {
		t.Error("expected error attribute")
	}

	if span.Status.Code != ebuv1.StatusCode_STATUS_CODE_ERROR {
		t.Errorf("expected STATUS_CODE_ERROR, got %v", span.Status.Code)
	}
}

func TestTelemetryCollector_AddSpan_Batching(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	collector.batchSize = 3 // Small batch for testing
	defer collector.Close()

	// Add 5 spans
	for i := 0; i < 5; i++ {
		ctx := context.Background()
		ctx = collector.OnPublishStart(ctx, "test.event")
		collector.OnPublishComplete(ctx, "test.event")
	}

	// Buffer should have 2 spans (3 were flushed)
	collector.mu.Lock()
	bufferSize := len(collector.spans)
	collector.mu.Unlock()

	if bufferSize != 2 {
		t.Errorf("expected 2 spans in buffer, got %d", bufferSize)
	}
}

func TestTelemetryCollector_Close(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")

	// Add some spans
	ctx := context.Background()
	ctx = collector.OnPublishStart(ctx, "test.event")
	collector.OnPublishComplete(ctx, "test.event")

	// Close should not panic
	err := collector.Close()
	if err != nil {
		t.Errorf("expected no error on close, got %v", err)
	}

	// Context should be cancelled
	select {
	case <-collector.ctx.Done():
		// Expected
	default:
		t.Error("expected context to be cancelled after close")
	}
}

func TestTelemetryCollector_MultipleSpanTypes(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	ctx := context.Background()

	// Add publish span
	ctx1 := collector.OnPublishStart(ctx, "event1")
	collector.OnPublishComplete(ctx1, "event1")

	// Add handler span
	ctx2 := collector.OnHandlerStart(ctx, "event2", true)
	collector.OnHandlerComplete(ctx2, 1*time.Millisecond, nil)

	// Add persist span
	ctx3 := collector.OnPersistStart(ctx, "event3", 100)
	collector.OnPersistComplete(ctx3, 2*time.Millisecond, nil)

	// Check all three spans
	collector.mu.Lock()
	defer collector.mu.Unlock()

	if len(collector.spans) != 3 {
		t.Fatalf("expected 3 spans, got %d", len(collector.spans))
	}

	// Verify each span type
	var hasPublish, hasHandler, hasPersist bool
	for _, span := range collector.spans {
		if span.Name == "ebu.publish" {
			hasPublish = true
		}
		if span.Name == "ebu.handler" {
			hasHandler = true
		}
		if span.Name == "ebu.persist" {
			hasPersist = true
		}
	}

	if !hasPublish {
		t.Error("expected at least one publish span")
	}
	if !hasHandler {
		t.Error("expected at least one handler span")
	}
	if !hasPersist {
		t.Error("expected at least one persist span")
	}
}

func TestTelemetryCollector_TimestampsAreSet(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	startTime := time.Now()
	ctx := context.Background()
	ctx = collector.OnPublishStart(ctx, "timestamped.event")
	collector.OnPublishComplete(ctx, "timestamped.event")

	collector.mu.Lock()
	defer collector.mu.Unlock()

	span := collector.spans[0]
	if span.StartTimeUnixNano == 0 {
		t.Fatal("expected start time to be set")
	}
	if span.EndTimeUnixNano == 0 {
		t.Fatal("expected end time to be set")
	}

	// Timestamps should be reasonable
	spanStart := time.Unix(0, int64(span.StartTimeUnixNano))
	spanEnd := time.Unix(0, int64(span.EndTimeUnixNano))

	if spanStart.Before(startTime.Add(-time.Second)) || spanStart.After(time.Now().Add(time.Second)) {
		t.Errorf("span start time seems wrong: %v", spanStart)
	}

	if spanEnd.Before(spanStart) {
		t.Error("end time should be after start time")
	}
}

func TestTelemetryCollector_ConcurrentAccess(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Concurrent writes
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			ctx := context.Background()
			ctx = collector.OnPublishStart(ctx, "concurrent.event")
			time.Sleep(1 * time.Millisecond)
			collector.OnPublishComplete(ctx, "concurrent.event")
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have some spans (may be less than 10 due to batching)
	collector.mu.Lock()
	count := len(collector.spans)
	collector.mu.Unlock()

	if count == 0 {
		t.Error("expected at least some spans after concurrent access")
	}
}

func TestTelemetryCollector_Flush(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Add spans below batch threshold
	ctx := context.Background()
	ctx = collector.OnPublishStart(ctx, "flush.test")
	collector.OnPublishComplete(ctx, "flush.test")

	// Verify spans in buffer
	collector.mu.Lock()
	initialCount := len(collector.spans)
	collector.mu.Unlock()

	if initialCount != 1 {
		t.Fatalf("expected 1 span before flush, got %d", initialCount)
	}

	// Flush should clear the buffer
	collector.flush()

	collector.mu.Lock()
	afterCount := len(collector.spans)
	collector.mu.Unlock()

	if afterCount != 0 {
		t.Errorf("expected 0 spans after flush, got %d", afterCount)
	}
}

func TestTelemetryCollector_EmptyFlush(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Flush with no spans should not panic
	collector.flush()

	collector.mu.Lock()
	count := len(collector.spans)
	collector.mu.Unlock()

	if count != 0 {
		t.Errorf("expected 0 spans after empty flush, got %d", count)
	}
}

func TestTelemetryCollector_EventTypePreserved(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	testCases := []string{
		"user.created",
		"order.placed.v2",
		"payment-processed",
		"LEGACY_EVENT_TYPE",
		"event.with.many.dots",
	}

	for _, eventType := range testCases {
		ctx := context.Background()
		ctx = collector.OnPublishStart(ctx, eventType)
		collector.OnPublishComplete(ctx, eventType)
	}

	collector.mu.Lock()
	defer collector.mu.Unlock()

	if len(collector.spans) != len(testCases) {
		t.Fatalf("expected %d spans, got %d", len(testCases), len(collector.spans))
	}

	for i, span := range collector.spans {
		found := false
		for _, attr := range span.Attributes {
			if attr.Key == "event.type" {
				if attr.Value.GetStringValue() != testCases[i] {
					t.Errorf("span %d: expected event.type %q, got %q",
						i, testCases[i], attr.Value.GetStringValue())
				}
				found = true
				break
			}
		}
		if !found {
			t.Errorf("span %d: missing event.type attribute", i)
		}
	}
}

func TestTelemetryCollector_FlushLoop_TickerFires(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	// Set very short interval for testing
	collector.interval = 50 * time.Millisecond
	defer collector.Close()

	// Add a span
	ctx := context.Background()
	ctx = collector.OnPublishStart(ctx, "test.event")
	collector.OnPublishComplete(ctx, "test.event")

	// Verify span is in buffer
	collector.mu.Lock()
	initialCount := len(collector.spans)
	collector.mu.Unlock()

	if initialCount != 1 {
		t.Fatalf("expected 1 span in buffer, got %d", initialCount)
	}

	// Wait for ticker to fire and flush
	time.Sleep(100 * time.Millisecond)

	// Buffer should be cleared by ticker
	collector.mu.Lock()
	afterCount := len(collector.spans)
	collector.mu.Unlock()

	if afterCount != 0 {
		t.Errorf("expected 0 spans after ticker flush, got %d", afterCount)
	}
}

func TestTelemetryCollector_OnHandlerComplete_NoContext(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Complete without start - should not panic or add span
	ctx := context.Background()
	collector.OnHandlerComplete(ctx, 1*time.Millisecond, nil)

	// No span should be added
	collector.mu.Lock()
	defer collector.mu.Unlock()

	if len(collector.spans) != 0 {
		t.Errorf("expected 0 spans, got %d", len(collector.spans))
	}
}

func TestTelemetryCollector_OnPersistComplete_NoContext(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Complete without start - should not panic or add span
	ctx := context.Background()
	collector.OnPersistComplete(ctx, 1*time.Millisecond, nil)

	// No span should be added
	collector.mu.Lock()
	defer collector.mu.Unlock()

	if len(collector.spans) != 0 {
		t.Errorf("expected 0 spans, got %d", len(collector.spans))
	}
}

func TestTelemetryCollector_SendBatch_ErrorHandling(t *testing.T) {
	httpClient := createHTTPClient()

	// Test with invalid URL to trigger connection errors
	collector := NewTelemetryCollector(httpClient, "http://localhost:1", "test-key")
	defer collector.Close()

	// Create a test span
	span := &ebuv1.Span{
		TraceId:           generateTraceID(),
		SpanId:            generateSpanID(),
		Name:              "test.span",
		Kind:              ebuv1.SpanKind_SPAN_KIND_INTERNAL,
		StartTimeUnixNano: uint64(time.Now().UnixNano()),
		EndTimeUnixNano:   uint64(time.Now().UnixNano()),
		Attributes: []*ebuv1.KeyValue{
			{
				Key: "test",
				Value: &ebuv1.AnyValue{
					Value: &ebuv1.AnyValue_StringValue{
						StringValue: "value",
					},
				},
			},
		},
		Status: &ebuv1.Status{
			Code: ebuv1.StatusCode_STATUS_CODE_OK,
		},
	}

	// Call sendBatch directly - this should log errors but not panic
	collector.sendBatch([]*ebuv1.Span{span})

	// If we get here without panic, error handling worked
}

func TestTelemetryCollector_SendBatch_PartialSuccess(t *testing.T) {
	// Create a mock server that returns partial success
	mux := http.NewServeMux()
	mux.Handle(ebuv1connect.NewEventServiceHandler(&mockOTLPServiceWithPartialFailure{}))
	server := httptest.NewServer(mux)
	defer server.Close()

	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, server.URL, "test-key")
	defer collector.Close()

	// Create test spans
	spans := []*ebuv1.Span{
		{
			TraceId:           generateTraceID(),
			SpanId:            generateSpanID(),
			Name:              "test1",
			Kind:              ebuv1.SpanKind_SPAN_KIND_INTERNAL,
			StartTimeUnixNano: uint64(time.Now().UnixNano()),
			EndTimeUnixNano:   uint64(time.Now().UnixNano()),
			Status:            &ebuv1.Status{Code: ebuv1.StatusCode_STATUS_CODE_OK},
		},
		{
			TraceId:           generateTraceID(),
			SpanId:            generateSpanID(),
			Name:              "test2",
			Kind:              ebuv1.SpanKind_SPAN_KIND_INTERNAL,
			StartTimeUnixNano: uint64(time.Now().UnixNano()),
			EndTimeUnixNano:   uint64(time.Now().UnixNano()),
			Status:            &ebuv1.Status{Code: ebuv1.StatusCode_STATUS_CODE_OK},
		},
	}

	// Call sendBatch - should log partial failure but not panic
	collector.sendBatch(spans)

	// If we get here without panic, error handling worked
}

// mockOTLPServiceWithPartialFailure implements OTLP service with partial failures
type mockOTLPServiceWithPartialFailure struct {
	ebuv1connect.UnimplementedEventServiceHandler
}

func (m *mockOTLPServiceWithPartialFailure) ExportTrace(
	ctx context.Context,
	req *connect.Request[ebuv1.ExportTraceServiceRequest],
) (*connect.Response[ebuv1.ExportTraceServiceResponse], error) {
	// Return partial success (reject 1 span)
	return connect.NewResponse(&ebuv1.ExportTraceServiceResponse{
		PartialSuccess: &ebuv1.ExportTracePartialSuccess{
			RejectedSpans: 1,
			ErrorMessage:  "some spans rejected",
		},
	}), nil
}

func TestTelemetryCollector_SendBatch_StreamError(t *testing.T) {
	// Create a mock server that returns error
	mux := http.NewServeMux()
	mux.Handle(ebuv1connect.NewEventServiceHandler(&mockOTLPServiceWithError{}))
	server := httptest.NewServer(mux)
	defer server.Close()

	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, server.URL, "test-key")
	defer collector.Close()

	// Create a test span
	span := &ebuv1.Span{
		TraceId:           generateTraceID(),
		SpanId:            generateSpanID(),
		Name:              "test.span",
		Kind:              ebuv1.SpanKind_SPAN_KIND_INTERNAL,
		StartTimeUnixNano: uint64(time.Now().UnixNano()),
		EndTimeUnixNano:   uint64(time.Now().UnixNano()),
		Status:            &ebuv1.Status{Code: ebuv1.StatusCode_STATUS_CODE_OK},
	}

	// Call sendBatch - should log error but not panic
	collector.sendBatch([]*ebuv1.Span{span})

	// If we get here without panic, error handling worked
}

// mockOTLPServiceWithError implements OTLP service that returns errors
type mockOTLPServiceWithError struct {
	ebuv1connect.UnimplementedEventServiceHandler
}

func (m *mockOTLPServiceWithError) ExportTrace(
	ctx context.Context,
	req *connect.Request[ebuv1.ExportTraceServiceRequest],
) (*connect.Response[ebuv1.ExportTraceServiceResponse], error) {
	// Return an error
	return nil, connect.NewError(connect.CodeInternal, errors.New("mock server error"))
}

func TestTelemetryCollector_Stats(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Initial stats should be zero
	stats := collector.Stats()
	if stats.BatchesSent != 0 {
		t.Errorf("expected 0 batches sent, got %d", stats.BatchesSent)
	}
	if stats.BatchesFailed != 0 {
		t.Errorf("expected 0 batches failed, got %d", stats.BatchesFailed)
	}
	if stats.SpansSent != 0 {
		t.Errorf("expected 0 spans sent, got %d", stats.SpansSent)
	}
}

func TestTelemetryCollector_Stats_AfterSuccess(t *testing.T) {
	// Create a mock server that accepts spans
	mux := http.NewServeMux()
	mux.Handle(ebuv1connect.NewEventServiceHandler(&mockOTLPService{}))
	server := httptest.NewServer(mux)
	defer server.Close()

	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, server.URL, "test-key")
	defer collector.Close()

	// Add some spans
	ctx := context.Background()
	ctx = collector.OnPublishStart(ctx, "test.event")
	collector.OnPublishComplete(ctx, "test.event")

	// Force flush
	collector.flush()

	// Wait for batch to be processed
	time.Sleep(100 * time.Millisecond)

	// Check stats
	stats := collector.Stats()
	if stats.BatchesSent == 0 {
		t.Error("expected at least 1 batch sent")
	}
	if stats.SpansSent == 0 {
		t.Error("expected at least 1 span sent")
	}
	if stats.LastSuccessTime.IsZero() {
		t.Error("expected LastSuccessTime to be set")
	}
}

func TestTelemetryCollector_Stats_AfterError(t *testing.T) {
	// Create a mock server that returns errors
	mux := http.NewServeMux()
	mux.Handle(ebuv1connect.NewEventServiceHandler(&mockOTLPServiceWithError{}))
	server := httptest.NewServer(mux)
	defer server.Close()

	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, server.URL, "test-key")
	defer collector.Close()

	// Add a span
	ctx := context.Background()
	ctx = collector.OnPublishStart(ctx, "test.event")
	collector.OnPublishComplete(ctx, "test.event")

	// Force flush
	collector.flush()

	// Wait for batch to be processed
	time.Sleep(100 * time.Millisecond)

	// Check stats
	stats := collector.Stats()
	if stats.BatchesFailed == 0 {
		t.Error("expected at least 1 batch failed")
	}
	if stats.LastError == "" {
		t.Error("expected LastError to be set")
	}
	if stats.LastErrorTime.IsZero() {
		t.Error("expected LastErrorTime to be set")
	}
}

func TestTelemetryCollector_Backpressure_DropsWhenQueueFull(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")

	// Cancel context to stop workers, then replace with zero-buffer queue
	collector.cancel()
	collector.wg.Wait()

	// Now replace queue with unbuffered channel
	collector.batchQueue = make(chan []*ebuv1.Span, 0) // No buffer
	defer collector.Close()

	// Add spans and flush - workers are stopped so queue will fill
	ctx := context.Background()
	ctx = collector.OnPublishStart(ctx, "test.event")
	collector.OnPublishComplete(ctx, "test.event")

	// First flush - will block trying to send
	go collector.flush()

	// Wait a moment for flush to block
	time.Sleep(10 * time.Millisecond)

	// Second flush - should drop because channel is full
	collector.flush()

	// Check stats
	stats := collector.Stats()
	if stats.BatchesDropped == 0 {
		t.Error("expected at least one batch dropped when queue is full")
	}
	if stats.SpansDropped == 0 {
		t.Error("expected at least one span dropped when queue is full")
	}
}

func TestTelemetryCollector_Backpressure_DropsInAddSpan(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	collector.batchSize = 5 // Small batch

	// Cancel context to stop workers
	collector.cancel()
	collector.wg.Wait()

	// Replace with zero-buffer queue
	collector.batchQueue = make(chan []*ebuv1.Span, 0) // No buffer
	defer collector.Close()

	// Add enough spans to trigger batch size flush in addSpan
	ctx := context.Background()
	for i := 0; i < 10; i++ { // Add twice the batch size
		ctx = collector.OnPublishStart(ctx, "test.event")
		collector.OnPublishComplete(ctx, "test.event")
	}

	// Check stats - should have dropped some batches when addSpan hit batchSize
	stats := collector.Stats()
	if stats.BatchesDropped == 0 {
		t.Error("expected at least one batch dropped in addSpan when queue is full")
	}
	if stats.SpansDropped == 0 {
		t.Error("expected at least one span dropped in addSpan when queue is full")
	}
}

func TestTelemetryCollector_SendsAuthorizationHeader(t *testing.T) {
	// Create a mock server that captures headers
	var receivedAuth string
	mockService := &mockOTLPServiceWithAuthCapture{
		authCapture: &receivedAuth,
	}
	mux := http.NewServeMux()
	mux.Handle(ebuv1connect.NewEventServiceHandler(mockService))
	server := httptest.NewServer(mux)
	defer server.Close()

	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, server.URL, "test-api-key-123")
	defer collector.Close()

	// Add and send a span
	ctx := context.Background()
	ctx = collector.OnPublishStart(ctx, "test.event")
	collector.OnPublishComplete(ctx, "test.event")

	// Force flush
	collector.flush()

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Verify Authorization header was sent
	expectedAuth := "Bearer test-api-key-123"
	if receivedAuth != expectedAuth {
		t.Errorf("expected Authorization header %q, got %q", expectedAuth, receivedAuth)
	}
}

// mockOTLPService implements basic OTLP trace service
type mockOTLPService struct {
	ebuv1connect.UnimplementedEventServiceHandler
}

func (m *mockOTLPService) ExportTrace(
	ctx context.Context,
	req *connect.Request[ebuv1.ExportTraceServiceRequest],
) (*connect.Response[ebuv1.ExportTraceServiceResponse], error) {
	return connect.NewResponse(&ebuv1.ExportTraceServiceResponse{}), nil
}

// mockOTLPServiceWithAuthCapture captures the Authorization header
type mockOTLPServiceWithAuthCapture struct {
	ebuv1connect.UnimplementedEventServiceHandler
	authCapture *string
}

func (m *mockOTLPServiceWithAuthCapture) ExportTrace(
	ctx context.Context,
	req *connect.Request[ebuv1.ExportTraceServiceRequest],
) (*connect.Response[ebuv1.ExportTraceServiceResponse], error) {
	// Capture authorization header
	*m.authCapture = req.Header().Get("Authorization")
	return connect.NewResponse(&ebuv1.ExportTraceServiceResponse{}), nil
}
