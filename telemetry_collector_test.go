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
	"google.golang.org/protobuf/types/known/timestamppb"
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

	if len(collector.metrics) != 0 {
		t.Errorf("expected empty metrics buffer, got %d items", len(collector.metrics))
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

	// Small delay to ensure metric is added
	time.Sleep(10 * time.Millisecond)

	// Check that metric was added
	collector.mu.Lock()
	metricsCount := len(collector.metrics)
	var metric *ebuv1.TelemetryMetric
	if metricsCount > 0 {
		metric = collector.metrics[0]
	}
	collector.mu.Unlock()

	if metricsCount != 1 {
		t.Fatalf("expected 1 metric, got %d", metricsCount)
	}

	if metric.Timestamp == nil {
		t.Error("expected timestamp to be set")
	}

	publish := metric.GetPublish()
	if publish == nil {
		t.Fatal("expected PublishMetric")
	}

	if publish.EventType != "order.created" {
		t.Errorf("expected event type 'order.created', got %s", publish.EventType)
	}

	if publish.DurationNs <= 0 {
		t.Errorf("expected positive duration, got %d", publish.DurationNs)
	}
}

func TestTelemetryCollector_OnPublishComplete_NoContext(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Complete without start - should not panic
	ctx := context.Background()
	collector.OnPublishComplete(ctx, "test.event")

	// No metric should be added
	collector.mu.Lock()
	defer collector.mu.Unlock()

	if len(collector.metrics) != 0 {
		t.Errorf("expected 0 metrics, got %d", len(collector.metrics))
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

	// Check metric
	collector.mu.Lock()
	defer collector.mu.Unlock()

	if len(collector.metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(collector.metrics))
	}

	handler := collector.metrics[0].GetHandler()
	if handler == nil {
		t.Fatal("expected HandlerMetric")
	}

	if handler.EventType != "email.sent" {
		t.Errorf("expected event type 'email.sent', got %s", handler.EventType)
	}

	if handler.Async {
		t.Error("expected async=false")
	}

	if handler.DurationNs != 3000000 { // 3ms in nanoseconds
		t.Errorf("expected duration 3000000ns, got %d", handler.DurationNs)
	}

	if handler.Error != "" {
		t.Errorf("expected no error, got %s", handler.Error)
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

	// Check metric
	collector.mu.Lock()
	defer collector.mu.Unlock()

	if len(collector.metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(collector.metrics))
	}

	handler := collector.metrics[0].GetHandler()
	if handler == nil {
		t.Fatal("expected HandlerMetric")
	}

	if !handler.Async {
		t.Error("expected async=true")
	}

	if handler.Error != "connection refused" {
		t.Errorf("expected error 'connection refused', got %s", handler.Error)
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

	// Check metric
	collector.mu.Lock()
	defer collector.mu.Unlock()

	if len(collector.metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(collector.metrics))
	}

	persist := collector.metrics[0].GetPersist()
	if persist == nil {
		t.Fatal("expected PersistMetric")
	}

	if persist.EventType != "inventory.adjusted" {
		t.Errorf("expected event type 'inventory.adjusted', got %s", persist.EventType)
	}

	if persist.Position != 123 {
		t.Errorf("expected position 123, got %d", persist.Position)
	}

	if persist.DurationNs != 10000000 { // 10ms in nanoseconds
		t.Errorf("expected duration 10000000ns, got %d", persist.DurationNs)
	}

	if persist.Error != "" {
		t.Errorf("expected no error, got %s", persist.Error)
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

	// Check metric
	collector.mu.Lock()
	defer collector.mu.Unlock()

	persist := collector.metrics[0].GetPersist()
	if persist == nil {
		t.Fatal("expected PersistMetric")
	}

	if persist.Error != "database connection lost" {
		t.Errorf("expected error 'database connection lost', got %s", persist.Error)
	}
}

func TestTelemetryCollector_AddMetric_Batching(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	collector.batchSize = 3 // Small batch for testing
	defer collector.Close()

	// Add 5 metrics
	for i := 0; i < 5; i++ {
		ctx := context.Background()
		ctx = collector.OnPublishStart(ctx, "test.event")
		collector.OnPublishComplete(ctx, "test.event")
	}

	// Buffer should have 2 metrics (3 were flushed)
	collector.mu.Lock()
	bufferSize := len(collector.metrics)
	collector.mu.Unlock()

	if bufferSize != 2 {
		t.Errorf("expected 2 metrics in buffer, got %d", bufferSize)
	}
}

func TestTelemetryCollector_Close(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")

	// Add some metrics
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

func TestTelemetryCollector_MultipleMetricTypes(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	ctx := context.Background()

	// Add publish metric
	ctx1 := collector.OnPublishStart(ctx, "event1")
	collector.OnPublishComplete(ctx1, "event1")

	// Add handler metric
	ctx2 := collector.OnHandlerStart(ctx, "event2", true)
	collector.OnHandlerComplete(ctx2, 1*time.Millisecond, nil)

	// Add persist metric
	ctx3 := collector.OnPersistStart(ctx, "event3", 100)
	collector.OnPersistComplete(ctx3, 2*time.Millisecond, nil)

	// Check all three metrics
	collector.mu.Lock()
	defer collector.mu.Unlock()

	if len(collector.metrics) != 3 {
		t.Fatalf("expected 3 metrics, got %d", len(collector.metrics))
	}

	// Verify each metric type
	var hasPublish, hasHandler, hasPersist bool
	for _, metric := range collector.metrics {
		if metric.GetPublish() != nil {
			hasPublish = true
		}
		if metric.GetHandler() != nil {
			hasHandler = true
		}
		if metric.GetPersist() != nil {
			hasPersist = true
		}
	}

	if !hasPublish {
		t.Error("expected at least one PublishMetric")
	}
	if !hasHandler {
		t.Error("expected at least one HandlerMetric")
	}
	if !hasPersist {
		t.Error("expected at least one PersistMetric")
	}
}

func TestTelemetryCollector_TimestampsAreSet(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	ctx := context.Background()
	ctx = collector.OnPublishStart(ctx, "timestamped.event")
	collector.OnPublishComplete(ctx, "timestamped.event")

	collector.mu.Lock()
	defer collector.mu.Unlock()

	metric := collector.metrics[0]
	if metric.Timestamp == nil {
		t.Fatal("expected timestamp to be set")
	}

	if !metric.Timestamp.IsValid() {
		t.Error("expected valid timestamp")
	}

	// Timestamp should be recent (within last second)
	now := time.Now()
	metricTime := metric.Timestamp.AsTime()
	diff := now.Sub(metricTime)

	if diff < 0 || diff > time.Second {
		t.Errorf("timestamp seems wrong: %v (diff from now: %v)", metricTime, diff)
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

	// Should have some metrics (may be less than 10 due to batching)
	collector.mu.Lock()
	count := len(collector.metrics)
	collector.mu.Unlock()

	if count == 0 {
		t.Error("expected at least some metrics after concurrent access")
	}
}

func TestTelemetryCollector_Flush(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Add metrics below batch threshold
	ctx := context.Background()
	ctx = collector.OnPublishStart(ctx, "flush.test")
	collector.OnPublishComplete(ctx, "flush.test")

	// Verify metrics in buffer
	collector.mu.Lock()
	initialCount := len(collector.metrics)
	collector.mu.Unlock()

	if initialCount != 1 {
		t.Fatalf("expected 1 metric before flush, got %d", initialCount)
	}

	// Flush should clear the buffer
	collector.flush()

	collector.mu.Lock()
	afterCount := len(collector.metrics)
	collector.mu.Unlock()

	if afterCount != 0 {
		t.Errorf("expected 0 metrics after flush, got %d", afterCount)
	}
}

func TestTelemetryCollector_EmptyFlush(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Flush with no metrics should not panic
	collector.flush()

	collector.mu.Lock()
	count := len(collector.metrics)
	collector.mu.Unlock()

	if count != 0 {
		t.Errorf("expected 0 metrics after empty flush, got %d", count)
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

	if len(collector.metrics) != len(testCases) {
		t.Fatalf("expected %d metrics, got %d", len(testCases), len(collector.metrics))
	}

	for i, metric := range collector.metrics {
		publish := metric.GetPublish()
		if publish == nil {
			t.Errorf("metric %d: expected PublishMetric", i)
			continue
		}

		if publish.EventType != testCases[i] {
			t.Errorf("metric %d: expected event type %q, got %q",
				i, testCases[i], publish.EventType)
		}
	}
}

func TestTelemetryCollector_FlushLoop_TickerFires(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	// Set very short interval for testing
	collector.interval = 50 * time.Millisecond
	defer collector.Close()

	// Add a metric
	ctx := context.Background()
	ctx = collector.OnPublishStart(ctx, "test.event")
	collector.OnPublishComplete(ctx, "test.event")

	// Verify metric is in buffer
	collector.mu.Lock()
	initialCount := len(collector.metrics)
	collector.mu.Unlock()

	if initialCount != 1 {
		t.Fatalf("expected 1 metric in buffer, got %d", initialCount)
	}

	// Wait for ticker to fire and flush
	time.Sleep(100 * time.Millisecond)

	// Buffer should be cleared by ticker
	collector.mu.Lock()
	afterCount := len(collector.metrics)
	collector.mu.Unlock()

	if afterCount != 0 {
		t.Errorf("expected 0 metrics after ticker flush, got %d", afterCount)
	}
}

func TestTelemetryCollector_OnHandlerComplete_NoContext(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Complete without start - should not panic or add metric
	ctx := context.Background()
	collector.OnHandlerComplete(ctx, 1*time.Millisecond, nil)

	// No metric should be added
	collector.mu.Lock()
	defer collector.mu.Unlock()

	if len(collector.metrics) != 0 {
		t.Errorf("expected 0 metrics, got %d", len(collector.metrics))
	}
}

func TestTelemetryCollector_OnPersistComplete_NoContext(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	defer collector.Close()

	// Complete without start - should not panic or add metric
	ctx := context.Background()
	collector.OnPersistComplete(ctx, 1*time.Millisecond, nil)

	// No metric should be added
	collector.mu.Lock()
	defer collector.mu.Unlock()

	if len(collector.metrics) != 0 {
		t.Errorf("expected 0 metrics, got %d", len(collector.metrics))
	}
}

func TestTelemetryCollector_SendBatch_ErrorHandling(t *testing.T) {
	httpClient := createHTTPClient()

	// Test with invalid URL to trigger connection errors
	collector := NewTelemetryCollector(httpClient, "http://localhost:1", "test-key")
	defer collector.Close()

	// Create a test metric
	metric := &ebuv1.TelemetryMetric{
		Timestamp: timestamppb.Now(),
		Metric: &ebuv1.TelemetryMetric_Publish{
			Publish: &ebuv1.PublishMetric{
				EventType:  "test.event",
				DurationNs: 1000000,
			},
		},
	}

	// Call sendBatch directly - this should log errors but not panic
	// We can't easily verify the log output, but we verify it doesn't panic
	collector.sendBatch([]*ebuv1.TelemetryMetric{metric})

	// If we get here without panic, error handling worked
}

func TestTelemetryCollector_SendBatch_CountMismatch(t *testing.T) {
	// Create a mock server that returns incorrect count
	mux := http.NewServeMux()
	mux.Handle(ebuv1connect.NewEventServiceHandler(&mockEventServiceWithMismatch{}))
	server := httptest.NewServer(mux)
	defer server.Close()

	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, server.URL, "test-key")
	defer collector.Close()

	// Create test metrics
	metrics := []*ebuv1.TelemetryMetric{
		{
			Timestamp: timestamppb.Now(),
			Metric: &ebuv1.TelemetryMetric_Publish{
				Publish: &ebuv1.PublishMetric{
					EventType:  "test1",
					DurationNs: 1000000,
				},
			},
		},
		{
			Timestamp: timestamppb.Now(),
			Metric: &ebuv1.TelemetryMetric_Publish{
				Publish: &ebuv1.PublishMetric{
					EventType:  "test2",
					DurationNs: 2000000,
				},
			},
		},
	}

	// Call sendBatch - should log count mismatch but not panic
	collector.sendBatch(metrics)

	// If we get here without panic, error handling worked
}

// mockEventServiceWithMismatch implements a partial EventService that returns wrong count
type mockEventServiceWithMismatch struct {
	ebuv1connect.UnimplementedEventServiceHandler
}

func (m *mockEventServiceWithMismatch) ReportTelemetry(
	ctx context.Context,
	stream *connect.ClientStream[ebuv1.TelemetryBatch],
) (*connect.Response[ebuv1.TelemetryResponse], error) {
	// Receive the batch
	for stream.Receive() {
		// Just consume, don't process
	}
	if stream.Err() != nil {
		return nil, stream.Err()
	}

	// Return wrong count (1 instead of actual count)
	return connect.NewResponse(&ebuv1.TelemetryResponse{
		ReceivedCount: 1, // Always return 1 to trigger mismatch
	}), nil
}

func TestTelemetryCollector_SendBatch_StreamError(t *testing.T) {
	// Create a mock server that returns error on receive
	mux := http.NewServeMux()
	mux.Handle(ebuv1connect.NewEventServiceHandler(&mockEventServiceWithError{}))
	server := httptest.NewServer(mux)
	defer server.Close()

	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, server.URL, "test-key")
	defer collector.Close()

	// Create a test metric
	metric := &ebuv1.TelemetryMetric{
		Timestamp: timestamppb.Now(),
		Metric: &ebuv1.TelemetryMetric_Publish{
			Publish: &ebuv1.PublishMetric{
				EventType:  "test.event",
				DurationNs: 1000000,
			},
		},
	}

	// Call sendBatch - should log error but not panic
	collector.sendBatch([]*ebuv1.TelemetryMetric{metric})

	// If we get here without panic, error handling worked
}

// mockEventServiceWithError implements a partial EventService that returns errors
type mockEventServiceWithError struct {
	ebuv1connect.UnimplementedEventServiceHandler
}

func (m *mockEventServiceWithError) ReportTelemetry(
	ctx context.Context,
	stream *connect.ClientStream[ebuv1.TelemetryBatch],
) (*connect.Response[ebuv1.TelemetryResponse], error) {
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
	if stats.MetricsSent != 0 {
		t.Errorf("expected 0 metrics sent, got %d", stats.MetricsSent)
	}
}

func TestTelemetryCollector_Stats_AfterSuccess(t *testing.T) {
	// Create a mock server that accepts metrics
	mux := http.NewServeMux()
	mux.Handle(ebuv1connect.NewEventServiceHandler(&mockEventService{}))
	server := httptest.NewServer(mux)
	defer server.Close()

	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, server.URL, "test-key")
	defer collector.Close()

	// Add some metrics
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
	if stats.MetricsSent == 0 {
		t.Error("expected at least 1 metric sent")
	}
	if stats.LastSuccessTime.IsZero() {
		t.Error("expected LastSuccessTime to be set")
	}
}

func TestTelemetryCollector_Stats_AfterError(t *testing.T) {
	// Create a mock server that returns errors
	mux := http.NewServeMux()
	mux.Handle(ebuv1connect.NewEventServiceHandler(&mockEventServiceWithError{}))
	server := httptest.NewServer(mux)
	defer server.Close()

	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, server.URL, "test-key")
	defer collector.Close()

	// Add a metric
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
	// Override with zero-buffer queue
	collector.batchQueue = make(chan []*ebuv1.TelemetryMetric, 0) // No buffer
	defer collector.Close()

	// Add metrics to buffer
	ctx := context.Background()
	ctx = collector.OnPublishStart(ctx, "test.event")
	collector.OnPublishComplete(ctx, "test.event")

	// Try to flush multiple times rapidly - should drop because channel is unbuffered
	// and workers aren't processing
	collector.flush() // First one might queue
	collector.flush() // This should drop (channel full, no buffer)

	// Check stats
	stats := collector.Stats()
	if stats.BatchesDropped == 0 {
		t.Error("expected at least one batch dropped when queue is full")
	}
	if stats.MetricsDropped == 0 {
		t.Error("expected at least one metric dropped when queue is full")
	}
}

func TestTelemetryCollector_Backpressure_DropsInAddMetric(t *testing.T) {
	httpClient := createHTTPClient()
	collector := NewTelemetryCollector(httpClient, "https://lookhere.tech", "test-key")
	// Override with zero-buffer queue and small batch size
	collector.batchQueue = make(chan []*ebuv1.TelemetryMetric, 0) // No buffer
	collector.batchSize = 5                                        // Small batch
	defer collector.Close()

	// Add enough metrics to trigger batch size flush in addMetric
	ctx := context.Background()
	for i := 0; i < 10; i++ { // Add twice the batch size
		ctx = collector.OnPublishStart(ctx, "test.event")
		collector.OnPublishComplete(ctx, "test.event")
	}

	// Wait a moment for processing
	time.Sleep(50 * time.Millisecond)

	// Check stats - should have dropped some batches when addMetric hit batchSize
	stats := collector.Stats()
	if stats.BatchesDropped == 0 {
		t.Error("expected at least one batch dropped in addMetric when queue is full")
	}
	if stats.MetricsDropped == 0 {
		t.Error("expected at least one metric dropped in addMetric when queue is full")
	}
}
