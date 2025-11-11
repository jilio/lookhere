package lookhere

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	ebuv1 "github.com/jilio/lookhere/gen/ebu/v1"
	"github.com/jilio/lookhere/gen/ebu/v1/ebuv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Context keys for tracking metrics
type publishStartKey struct{}
type handlerStartKey struct{}
type persistStartKey struct{}

// handlerMeta stores handler metadata in context
type handlerMeta struct {
	start     time.Time
	eventType string
	async     bool
}

// persistMeta stores persist metadata in context
type persistMeta struct {
	start     time.Time
	eventType string
	position  int64
}

// TelemetryCollector implements the eventbus.Observability interface
// and batches telemetry metrics to send to the remote lookhere server.
type TelemetryCollector struct {
	client    ebuv1connect.EventServiceClient
	apiKey    string
	batchSize int
	interval  time.Duration

	mu      sync.Mutex
	metrics []*ebuv1.TelemetryMetric
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewTelemetryCollector creates a new telemetry collector that sends metrics
// to the remote lookhere server.
func NewTelemetryCollector(httpClient *http.Client, baseURL, apiKey string) *TelemetryCollector {
	ctx, cancel := context.WithCancel(context.Background())

	tc := &TelemetryCollector{
		client:    ebuv1connect.NewEventServiceClient(httpClient, baseURL),
		apiKey:    apiKey,
		batchSize: 100,           // Default: send after 100 metrics
		interval:  10 * time.Second, // Default: send every 10 seconds
		metrics:   make([]*ebuv1.TelemetryMetric, 0, 100),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Start background goroutine to flush metrics periodically
	tc.wg.Add(1)
	go tc.flushLoop()

	return tc
}

// flushLoop periodically flushes metrics to the server
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

// flush sends accumulated metrics to the server
func (tc *TelemetryCollector) flush() {
	tc.mu.Lock()
	if len(tc.metrics) == 0 {
		tc.mu.Unlock()
		return
	}

	// Take current batch and reset buffer
	batch := tc.metrics
	tc.metrics = make([]*ebuv1.TelemetryMetric, 0, tc.batchSize)
	tc.mu.Unlock()

	// Send to server (don't block if server is slow/down)
	go tc.sendBatch(batch)
}

// sendBatch sends a batch of metrics to the server
func (tc *TelemetryCollector) sendBatch(metrics []*ebuv1.TelemetryMetric) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream := tc.client.ReportTelemetry(ctx)

	// Send batch
	if err := stream.Send(&ebuv1.TelemetryBatch{
		Metrics: metrics,
	}); err != nil {
		log.Printf("lookhere: failed to send telemetry batch: %v", err)
		return
	}

	// Close stream and receive response
	resp, err := stream.CloseAndReceive()
	if err != nil {
		log.Printf("lookhere: failed to receive telemetry response: %v", err)
		return
	}

	if resp.Msg.ReceivedCount != int64(len(metrics)) {
		log.Printf("lookhere: telemetry count mismatch: sent %d, server received %d",
			len(metrics), resp.Msg.ReceivedCount)
	}
}

// addMetric adds a metric to the buffer and flushes if batch size is reached
func (tc *TelemetryCollector) addMetric(metric *ebuv1.TelemetryMetric) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	tc.metrics = append(tc.metrics, metric)

	// Flush if batch size reached
	if len(tc.metrics) >= tc.batchSize {
		batch := tc.metrics
		tc.metrics = make([]*ebuv1.TelemetryMetric, 0, tc.batchSize)
		go tc.sendBatch(batch)
	}
}

// Close stops the telemetry collector and flushes remaining metrics
func (tc *TelemetryCollector) Close() error {
	tc.cancel()
	tc.wg.Wait()
	return nil
}

// OnPublishStart is called when an event publish starts
func (tc *TelemetryCollector) OnPublishStart(ctx context.Context, eventType string) context.Context {
	return context.WithValue(ctx, publishStartKey{}, time.Now())
}

// OnPublishComplete is called when an event publish completes
func (tc *TelemetryCollector) OnPublishComplete(ctx context.Context, eventType string) {
	start, ok := ctx.Value(publishStartKey{}).(time.Time)
	if !ok {
		return
	}

	duration := time.Since(start)
	tc.addMetric(&ebuv1.TelemetryMetric{
		Timestamp: timestamppb.Now(),
		Metric: &ebuv1.TelemetryMetric_Publish{
			Publish: &ebuv1.PublishMetric{
				EventType:  eventType,
				DurationNs: duration.Nanoseconds(),
			},
		},
	})
}

// OnHandlerStart is called when an event handler starts
func (tc *TelemetryCollector) OnHandlerStart(ctx context.Context, eventType string, async bool) context.Context {
	return context.WithValue(ctx, handlerStartKey{}, &handlerMeta{
		start:     time.Now(),
		eventType: eventType,
		async:     async,
	})
}

// OnHandlerComplete is called when an event handler completes
func (tc *TelemetryCollector) OnHandlerComplete(ctx context.Context, duration time.Duration, err error) {
	meta, ok := ctx.Value(handlerStartKey{}).(*handlerMeta)
	if !ok {
		return
	}

	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	tc.addMetric(&ebuv1.TelemetryMetric{
		Timestamp: timestamppb.Now(),
		Metric: &ebuv1.TelemetryMetric_Handler{
			Handler: &ebuv1.HandlerMetric{
				EventType:  meta.eventType,
				Async:      meta.async,
				DurationNs: duration.Nanoseconds(),
				Error:      errMsg,
			},
		},
	})
}

// OnPersistStart is called when event persistence starts
func (tc *TelemetryCollector) OnPersistStart(ctx context.Context, eventType string, position int64) context.Context {
	return context.WithValue(ctx, persistStartKey{}, &persistMeta{
		start:     time.Now(),
		eventType: eventType,
		position:  position,
	})
}

// OnPersistComplete is called when event persistence completes
func (tc *TelemetryCollector) OnPersistComplete(ctx context.Context, duration time.Duration, err error) {
	meta, ok := ctx.Value(persistStartKey{}).(*persistMeta)
	if !ok {
		return
	}

	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	tc.addMetric(&ebuv1.TelemetryMetric{
		Timestamp: timestamppb.Now(),
		Metric: &ebuv1.TelemetryMetric_Persist{
			Persist: &ebuv1.PersistMetric{
				EventType:  meta.eventType,
				Position:   meta.position,
				DurationNs: duration.Nanoseconds(),
				Error:      errMsg,
			},
		},
	})
}
