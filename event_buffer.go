package lookhere

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"
	eventbus "github.com/jilio/ebu"
	ebuv1 "github.com/jilio/lookhere/gen/ebu/v1"
	"github.com/jilio/lookhere/gen/ebu/v1/ebuv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// DefaultEventBatchSize is the default number of events before flushing
	DefaultEventBatchSize = 100
	// DefaultEventFlushInterval is the default time interval between flushes
	DefaultEventFlushInterval = 10 * time.Millisecond
	// DefaultEventWorkerPoolSize is the default number of worker goroutines
	DefaultEventWorkerPoolSize = 10
	// DefaultEventMaxQueueSize is the default maximum number of pending batches
	DefaultEventMaxQueueSize = 100
)

// EventStats contains event buffer health and performance metrics
type EventStats struct {
	BatchesSent     int64
	BatchesFailed   int64
	BatchesDropped  int64
	EventsSent      int64
	EventsDropped   int64
	LastSuccessTime time.Time
	LastErrorTime   time.Time
	LastError       string
}

// EventBuffer implements the eventbus.EventStore interface
// and batches events to send to the remote lookhere server.
type EventBuffer struct {
	client    ebuv1connect.EventServiceClient
	apiKey    string
	batchSize int
	interval  time.Duration

	mu     sync.Mutex
	events []*ebuv1.StoredEvent
	stats  EventStats

	// Worker pool for sending batches
	batchQueue chan []*ebuv1.StoredEvent

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewEventBuffer creates a new event buffer that sends events
// to the remote lookhere server in batches.
func NewEventBuffer(httpClient *http.Client, baseURL, apiKey string) *EventBuffer {
	ctx, cancel := context.WithCancel(context.Background())

	eb := &EventBuffer{
		client:     ebuv1connect.NewEventServiceClient(httpClient, baseURL),
		apiKey:     apiKey,
		batchSize:  DefaultEventBatchSize,
		interval:   DefaultEventFlushInterval,
		events:     make([]*ebuv1.StoredEvent, 0, DefaultEventBatchSize),
		batchQueue: make(chan []*ebuv1.StoredEvent, DefaultEventMaxQueueSize),
		ctx:        ctx,
		cancel:     cancel,
	}

	// Start worker pool
	for i := 0; i < DefaultEventWorkerPoolSize; i++ {
		eb.wg.Add(1)
		go eb.worker()
	}

	// Start background goroutine to flush events periodically
	eb.wg.Add(1)
	go eb.flushLoop()

	return eb
}

// worker processes batches from the queue
func (eb *EventBuffer) worker() {
	defer eb.wg.Done()
	for {
		select {
		case batch := <-eb.batchQueue:
			eb.sendBatch(batch)
		case <-eb.ctx.Done():
			return
		}
	}
}

// flushLoop periodically flushes events to the server
func (eb *EventBuffer) flushLoop() {
	defer eb.wg.Done()
	ticker := time.NewTicker(eb.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			eb.flush()
		case <-eb.ctx.Done():
			// Final flush before shutdown
			eb.flush()
			return
		}
	}
}

// flush sends accumulated events to the server
func (eb *EventBuffer) flush() {
	eb.mu.Lock()
	if len(eb.events) == 0 {
		eb.mu.Unlock()
		return
	}

	// Take current batch and reset buffer
	batch := eb.events
	batchSize := int64(len(batch))
	eb.events = make([]*ebuv1.StoredEvent, 0, eb.batchSize)
	eb.mu.Unlock()

	// Send to worker pool (non-blocking with backpressure)
	select {
	case eb.batchQueue <- batch:
		// Successfully queued
	default:
		// Queue is full - drop batch and log
		eb.mu.Lock()
		eb.stats.BatchesDropped++
		eb.stats.EventsDropped += batchSize
		eb.mu.Unlock()
		log.Printf("lookhere: event queue full, dropping batch of %d events", batchSize)
	}
}

// Stats returns a copy of the current event statistics.
// This method is safe for concurrent use.
func (eb *EventBuffer) Stats() EventStats {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	return eb.stats
}

// sendBatch sends a batch of events to the server
func (eb *EventBuffer) sendBatch(events []*ebuv1.StoredEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Build SaveEventsRequest
	req := &ebuv1.SaveEventsRequest{
		Events: events,
	}

	// Send request with authorization header
	request := connect.NewRequest(req)
	request.Header().Set("Authorization", "Bearer "+eb.apiKey)

	resp, err := eb.client.SaveEvents(ctx, request)
	if err != nil {
		eb.mu.Lock()
		eb.stats.BatchesFailed++
		eb.stats.LastErrorTime = time.Now()
		eb.stats.LastError = err.Error()
		eb.mu.Unlock()
		log.Printf("lookhere: failed to send event batch: %v", err)
		return
	}

	// Update success stats
	eb.mu.Lock()
	eb.stats.BatchesSent++
	eb.stats.EventsSent += int64(len(events))
	eb.stats.LastSuccessTime = time.Now()
	eb.mu.Unlock()

	// Verify we got positions back
	if len(resp.Msg.Positions) != len(events) {
		log.Printf("lookhere: warning: received %d positions for %d events",
			len(resp.Msg.Positions), len(events))
	}
}

// Save implements eventbus.EventStore interface
func (eb *EventBuffer) Save(ctx context.Context, event *eventbus.StoredEvent) error {
	eb.mu.Lock()
	eb.events = append(eb.events, &ebuv1.StoredEvent{
		Position:  event.Position,
		Type:      event.Type,
		Data:      event.Data,
		Timestamp: timestamppb.New(event.Timestamp),
	})

	// Flush if batch size reached
	if len(eb.events) >= eb.batchSize {
		batch := eb.events
		batchSize := int64(len(batch))
		eb.events = make([]*ebuv1.StoredEvent, 0, eb.batchSize)
		eb.mu.Unlock()

		// Send to worker pool (non-blocking with backpressure)
		select {
		case eb.batchQueue <- batch:
			// Successfully queued
		default:
			// Queue is full - drop batch and log
			eb.mu.Lock()
			eb.stats.BatchesDropped++
			eb.stats.EventsDropped += batchSize
			eb.mu.Unlock()
			log.Printf("lookhere: event queue full, dropping batch of %d events", batchSize)
		}
		return nil
	}
	eb.mu.Unlock()
	return nil
}

// Load implements eventbus.EventStore interface
func (eb *EventBuffer) Load(ctx context.Context, from, to int64) ([]*eventbus.StoredEvent, error) {
	// Flush any pending events before loading
	eb.flush()

	req := connect.NewRequest(&ebuv1.LoadEventsRequest{
		From: from,
		To:   to,
	})
	req.Header().Set("Authorization", "Bearer "+eb.apiKey)

	resp, err := eb.client.LoadEvents(ctx, req)
	if err != nil {
		return nil, err
	}

	events := make([]*eventbus.StoredEvent, len(resp.Msg.Events))
	for i, e := range resp.Msg.Events {
		events[i] = &eventbus.StoredEvent{
			Position:  e.Position,
			Type:      e.Type,
			Data:      e.Data,
			Timestamp: e.Timestamp.AsTime(),
		}
	}

	return events, nil
}

// GetPosition implements eventbus.EventStore interface
func (eb *EventBuffer) GetPosition(ctx context.Context) (int64, error) {
	// Flush any pending events before getting position
	eb.flush()

	req := connect.NewRequest(&ebuv1.GetPositionRequest{})
	req.Header().Set("Authorization", "Bearer "+eb.apiKey)

	resp, err := eb.client.GetPosition(ctx, req)
	if err != nil {
		return 0, err
	}

	return resp.Msg.Position, nil
}

// SaveSubscriptionPosition implements eventbus.EventStore interface
func (eb *EventBuffer) SaveSubscriptionPosition(ctx context.Context, subscriptionID string, position int64) error {
	req := connect.NewRequest(&ebuv1.SaveSubscriptionPositionRequest{
		SubscriptionId: subscriptionID,
		Position:       position,
	})
	req.Header().Set("Authorization", "Bearer "+eb.apiKey)

	_, err := eb.client.SaveSubscriptionPosition(ctx, req)
	return err
}

// LoadSubscriptionPosition implements eventbus.EventStore interface
func (eb *EventBuffer) LoadSubscriptionPosition(ctx context.Context, subscriptionID string) (int64, error) {
	req := connect.NewRequest(&ebuv1.LoadSubscriptionPositionRequest{
		SubscriptionId: subscriptionID,
	})
	req.Header().Set("Authorization", "Bearer "+eb.apiKey)

	resp, err := eb.client.LoadSubscriptionPosition(ctx, req)
	if err != nil {
		return 0, err
	}

	return resp.Msg.Position, nil
}

// Close stops the event buffer and flushes remaining events
func (eb *EventBuffer) Close() error {
	eb.cancel()
	eb.wg.Wait()
	return nil
}
