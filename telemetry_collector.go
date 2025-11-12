package lookhere

import (
	"context"
	"crypto/rand"
	"log"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"
	ebuv1 "github.com/jilio/lookhere/gen/ebu/v1"
	"github.com/jilio/lookhere/gen/ebu/v1/ebuv1connect"
)

// Context keys for tracking spans
type publishStartKey struct{}
type handlerStartKey struct{}
type persistStartKey struct{}

// publishMeta stores publish metadata in context
type publishMeta struct {
	start   time.Time
	traceID []byte
	spanID  []byte
}

// handlerMeta stores handler metadata in context
type handlerMeta struct {
	start     time.Time
	eventType string
	async     bool
	traceID   []byte
	spanID    []byte
}

// persistMeta stores persist metadata in context
type persistMeta struct {
	start     time.Time
	eventType string
	position  int64
	traceID   []byte
	spanID    []byte
}

const (
	// DefaultBatchSize is the default number of metrics before flushing
	DefaultBatchSize = 100
	// DefaultFlushInterval is the default time interval between flushes
	DefaultFlushInterval = 10 * time.Second
	// DefaultWorkerPoolSize is the default number of worker goroutines
	DefaultWorkerPoolSize = 10
	// DefaultMaxQueueSize is the default maximum number of pending batches
	DefaultMaxQueueSize = 100
)

// TelemetryStats contains telemetry health and performance metrics
type TelemetryStats struct {
	BatchesSent     int64
	BatchesFailed   int64
	BatchesDropped  int64
	SpansSent       int64
	SpansDropped    int64
	LastSuccessTime time.Time
	LastErrorTime   time.Time
	LastError       string
}

// TelemetryCollector implements the eventbus.Observability interface
// and batches OTLP spans to send to the remote lookhere server.
type TelemetryCollector struct {
	client    ebuv1connect.EventServiceClient
	apiKey    string
	batchSize int
	interval  time.Duration

	mu    sync.Mutex
	spans []*ebuv1.Span
	stats TelemetryStats

	// Worker pool for sending batches
	batchQueue chan []*ebuv1.Span

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewTelemetryCollector creates a new telemetry collector that sends OTLP spans
// to the remote lookhere server.
func NewTelemetryCollector(httpClient *http.Client, baseURL, apiKey string) *TelemetryCollector {
	ctx, cancel := context.WithCancel(context.Background())

	tc := &TelemetryCollector{
		client:     ebuv1connect.NewEventServiceClient(httpClient, baseURL),
		apiKey:     apiKey,
		batchSize:  DefaultBatchSize,
		interval:   DefaultFlushInterval,
		spans:      make([]*ebuv1.Span, 0, DefaultBatchSize),
		batchQueue: make(chan []*ebuv1.Span, DefaultMaxQueueSize),
		ctx:        ctx,
		cancel:     cancel,
	}

	// Start worker pool
	for i := 0; i < DefaultWorkerPoolSize; i++ {
		tc.wg.Add(1)
		go tc.worker()
	}

	// Start background goroutine to flush spans periodically
	tc.wg.Add(1)
	go tc.flushLoop()

	return tc
}

// worker processes batches from the queue
func (tc *TelemetryCollector) worker() {
	defer tc.wg.Done()
	for {
		select {
		case batch := <-tc.batchQueue:
			tc.sendBatch(batch)
		case <-tc.ctx.Done():
			return
		}
	}
}

// flushLoop periodically flushes spans to the server
func (tc *TelemetryCollector) flushLoop() {
	defer tc.wg.Done()
	ticker := time.NewTicker(tc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			tc.flush()
		case <-tc.ctx.Done():
			// Final flush before shutdown
			tc.flush()
			return
		}
	}
}

// flush sends accumulated spans to the server
func (tc *TelemetryCollector) flush() {
	tc.mu.Lock()
	if len(tc.spans) == 0 {
		tc.mu.Unlock()
		return
	}

	// Take current batch and reset buffer
	batch := tc.spans
	batchSize := int64(len(batch))
	tc.spans = make([]*ebuv1.Span, 0, tc.batchSize)
	tc.mu.Unlock()

	// Send to worker pool (non-blocking with backpressure)
	select {
	case tc.batchQueue <- batch:
		// Successfully queued
	default:
		// Queue is full - drop batch and log
		tc.mu.Lock()
		tc.stats.BatchesDropped++
		tc.stats.SpansDropped += batchSize
		tc.mu.Unlock()
		log.Printf("lookhere: telemetry queue full, dropping batch of %d spans", batchSize)
	}
}

// Stats returns a copy of the current telemetry statistics.
// This method is safe for concurrent use.
func (tc *TelemetryCollector) Stats() TelemetryStats {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.stats
}

// sendBatch sends a batch of OTLP spans to the server
func (tc *TelemetryCollector) sendBatch(spans []*ebuv1.Span) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Build OTLP ExportTraceServiceRequest
	req := &ebuv1.ExportTraceServiceRequest{
		ResourceSpans: []*ebuv1.ResourceSpans{
			{
				Resource: &ebuv1.Resource{
					Attributes: []*ebuv1.KeyValue{
						{
							Key: "service.name",
							Value: &ebuv1.AnyValue{
								Value: &ebuv1.AnyValue_StringValue{
									StringValue: "ebu",
								},
							},
						},
					},
				},
				ScopeSpans: []*ebuv1.ScopeSpans{
					{
						Scope: &ebuv1.InstrumentationScope{
							Name:    "github.com/jilio/lookhere",
							Version: "1.0.0",
						},
						Spans: spans,
					},
				},
			},
		},
	}

	// Send request with authorization header
	request := connect.NewRequest(req)
	request.Header().Set("Authorization", "Bearer "+tc.apiKey)

	resp, err := tc.client.ExportTrace(ctx, request)
	if err != nil {
		tc.mu.Lock()
		tc.stats.BatchesFailed++
		tc.stats.LastErrorTime = time.Now()
		tc.stats.LastError = err.Error()
		tc.mu.Unlock()
		log.Printf("lookhere: failed to send OTLP traces: %v", err)
		return
	}

	// Update success stats
	tc.mu.Lock()
	tc.stats.BatchesSent++
	tc.stats.SpansSent += int64(len(spans))
	tc.stats.LastSuccessTime = time.Now()
	tc.mu.Unlock()

	// Log any partial failures
	if resp.Msg.PartialSuccess != nil && resp.Msg.PartialSuccess.RejectedSpans > 0 {
		log.Printf("lookhere: %d spans rejected: %s",
			resp.Msg.PartialSuccess.RejectedSpans,
			resp.Msg.PartialSuccess.ErrorMessage)
	}
}

// generateTraceID generates a random 16-byte trace ID
func generateTraceID() []byte {
	traceID := make([]byte, 16)
	_, _ = rand.Read(traceID)
	return traceID
}

// generateSpanID generates a random 8-byte span ID
func generateSpanID() []byte {
	spanID := make([]byte, 8)
	_, _ = rand.Read(spanID)
	return spanID
}

// addSpan adds a span to the buffer and flushes if batch size is reached
func (tc *TelemetryCollector) addSpan(span *ebuv1.Span) {
	tc.mu.Lock()
	tc.spans = append(tc.spans, span)

	// Flush if batch size reached
	if len(tc.spans) >= tc.batchSize {
		batch := tc.spans
		batchSize := int64(len(batch))
		tc.spans = make([]*ebuv1.Span, 0, tc.batchSize)
		tc.mu.Unlock()

		// Send to worker pool (non-blocking with backpressure)
		select {
		case tc.batchQueue <- batch:
			// Successfully queued
		default:
			// Queue is full - drop batch and log
			tc.mu.Lock()
			tc.stats.BatchesDropped++
			tc.stats.SpansDropped += batchSize
			tc.mu.Unlock()
			log.Printf("lookhere: telemetry queue full, dropping batch of %d spans", batchSize)
		}
		return
	}
	tc.mu.Unlock()
}

// Close stops the telemetry collector and flushes remaining metrics
func (tc *TelemetryCollector) Close() error {
	tc.cancel()
	tc.wg.Wait()
	return nil
}

// OnPublishStart is called when an event publish starts
func (tc *TelemetryCollector) OnPublishStart(ctx context.Context, eventType string) context.Context {
	return context.WithValue(ctx, publishStartKey{}, &publishMeta{
		start:   time.Now(),
		traceID: generateTraceID(),
		spanID:  generateSpanID(),
	})
}

// OnPublishComplete is called when an event publish completes
func (tc *TelemetryCollector) OnPublishComplete(ctx context.Context, eventType string) {
	meta, ok := ctx.Value(publishStartKey{}).(*publishMeta)
	if !ok {
		return
	}

	endTime := time.Now()
	tc.addSpan(&ebuv1.Span{
		TraceId:           meta.traceID,
		SpanId:            meta.spanID,
		Name:              "ebu.publish",
		Kind:              ebuv1.SpanKind_SPAN_KIND_INTERNAL,
		StartTimeUnixNano: uint64(meta.start.UnixNano()),
		EndTimeUnixNano:   uint64(endTime.UnixNano()),
		Attributes: []*ebuv1.KeyValue{
			{
				Key: "event.type",
				Value: &ebuv1.AnyValue{
					Value: &ebuv1.AnyValue_StringValue{
						StringValue: eventType,
					},
				},
			},
		},
		Status: &ebuv1.Status{
			Code: ebuv1.StatusCode_STATUS_CODE_OK,
		},
	})
}

// OnHandlerStart is called when an event handler starts
func (tc *TelemetryCollector) OnHandlerStart(ctx context.Context, eventType string, async bool) context.Context {
	return context.WithValue(ctx, handlerStartKey{}, &handlerMeta{
		start:     time.Now(),
		eventType: eventType,
		async:     async,
		traceID:   generateTraceID(),
		spanID:    generateSpanID(),
	})
}

// OnHandlerComplete is called when an event handler completes
func (tc *TelemetryCollector) OnHandlerComplete(ctx context.Context, duration time.Duration, err error) {
	meta, ok := ctx.Value(handlerStartKey{}).(*handlerMeta)
	if !ok {
		return
	}

	endTime := time.Now()

	// Build attributes
	attrs := []*ebuv1.KeyValue{
		{
			Key: "event.type",
			Value: &ebuv1.AnyValue{
				Value: &ebuv1.AnyValue_StringValue{
					StringValue: meta.eventType,
				},
			},
		},
		{
			Key: "handler.async",
			Value: &ebuv1.AnyValue{
				Value: &ebuv1.AnyValue_BoolValue{
					BoolValue: meta.async,
				},
			},
		},
	}

	// Determine status
	status := &ebuv1.Status{
		Code: ebuv1.StatusCode_STATUS_CODE_OK,
	}
	if err != nil {
		status.Code = ebuv1.StatusCode_STATUS_CODE_ERROR
		status.Message = err.Error()
		attrs = append(attrs, &ebuv1.KeyValue{
			Key: "error",
			Value: &ebuv1.AnyValue{
				Value: &ebuv1.AnyValue_StringValue{
					StringValue: err.Error(),
				},
			},
		})
	}

	tc.addSpan(&ebuv1.Span{
		TraceId:           meta.traceID,
		SpanId:            meta.spanID,
		Name:              "ebu.handler",
		Kind:              ebuv1.SpanKind_SPAN_KIND_INTERNAL,
		StartTimeUnixNano: uint64(meta.start.UnixNano()),
		EndTimeUnixNano:   uint64(endTime.UnixNano()),
		Attributes:        attrs,
		Status:            status,
	})
}

// OnPersistStart is called when event persistence starts
func (tc *TelemetryCollector) OnPersistStart(ctx context.Context, eventType string, position int64) context.Context {
	return context.WithValue(ctx, persistStartKey{}, &persistMeta{
		start:     time.Now(),
		eventType: eventType,
		position:  position,
		traceID:   generateTraceID(),
		spanID:    generateSpanID(),
	})
}

// OnPersistComplete is called when event persistence completes
func (tc *TelemetryCollector) OnPersistComplete(ctx context.Context, duration time.Duration, err error) {
	meta, ok := ctx.Value(persistStartKey{}).(*persistMeta)
	if !ok {
		return
	}

	endTime := time.Now()

	// Build attributes
	attrs := []*ebuv1.KeyValue{
		{
			Key: "event.type",
			Value: &ebuv1.AnyValue{
				Value: &ebuv1.AnyValue_StringValue{
					StringValue: meta.eventType,
				},
			},
		},
		{
			Key: "event.position",
			Value: &ebuv1.AnyValue{
				Value: &ebuv1.AnyValue_IntValue{
					IntValue: meta.position,
				},
			},
		},
	}

	// Determine status
	status := &ebuv1.Status{
		Code: ebuv1.StatusCode_STATUS_CODE_OK,
	}
	if err != nil {
		status.Code = ebuv1.StatusCode_STATUS_CODE_ERROR
		status.Message = err.Error()
		attrs = append(attrs, &ebuv1.KeyValue{
			Key: "error",
			Value: &ebuv1.AnyValue{
				Value: &ebuv1.AnyValue_StringValue{
					StringValue: err.Error(),
				},
			},
		})
	}

	tc.addSpan(&ebuv1.Span{
		TraceId:           meta.traceID,
		SpanId:            meta.spanID,
		Name:              "ebu.persist",
		Kind:              ebuv1.SpanKind_SPAN_KIND_INTERNAL,
		StartTimeUnixNano: uint64(meta.start.UnixNano()),
		EndTimeUnixNano:   uint64(endTime.UnixNano()),
		Attributes:        attrs,
		Status:            status,
	})
}
