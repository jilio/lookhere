package lookhere

import (
	"context"
	"errors"
	"testing"
	"time"

	ebuv1 "github.com/jilio/lookhere/gen/ebu/v1"
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
