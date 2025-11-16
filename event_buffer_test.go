package lookhere

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	eventbus "github.com/jilio/ebu"
	ebuv1 "github.com/jilio/lookhere/gen/ebu/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestNewEventBuffer(t *testing.T) {
	buffer := NewEventBuffer(http.DefaultClient, "https://lookhere.tech", "test-api-key")

	if buffer == nil {
		t.Fatal("expected non-nil buffer")
	}
	if buffer.apiKey != "test-api-key" {
		t.Errorf("expected apiKey 'test-api-key', got %q", buffer.apiKey)
	}
	if buffer.client == nil {
		t.Fatal("expected non-nil client")
	}
	if buffer.batchSize != DefaultEventBatchSize {
		t.Errorf("expected batchSize %d, got %d", DefaultEventBatchSize, buffer.batchSize)
	}
	if buffer.interval != DefaultEventFlushInterval {
		t.Errorf("expected interval %v, got %v", DefaultEventFlushInterval, buffer.interval)
	}

	// Clean up
	buffer.Close()
}

func TestEventBuffer_Save_SingleEvent(t *testing.T) {
	var receivedAPIKey string
	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("Authorization")
		receivedPath = r.URL.Path

		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")
	defer buffer.Close()

	event := &eventbus.StoredEvent{
		Position:  1,
		Type:      "TestEvent",
		Data:      []byte("test data"),
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	err := buffer.Save(ctx, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for flush
	time.Sleep(50 * time.Millisecond)

	// Verify the authorization header was sent
	if receivedAPIKey != "Bearer test-api-key" {
		t.Errorf("expected Authorization header 'Bearer test-api-key', got %q", receivedAPIKey)
	}

	// Should use batched endpoint
	if receivedPath != "/ebu.v1.EventService/SaveEvents" {
		t.Errorf("expected SaveEvents endpoint, got %s", receivedPath)
	}
}

func TestEventBuffer_Save_BatchFull(t *testing.T) {
	requestCount := 0
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()

		// Return positions
		resp := &ebuv1.SaveEventsResponse{
			Positions: make([]int64, DefaultEventBatchSize),
		}
		for i := range resp.Positions {
			resp.Positions[i] = int64(i + 1)
		}

		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)

		// Write minimal proto response
		data, _ := proto.Marshal(resp)
		w.Write(data)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")
	defer buffer.Close()

	// Send exactly batch size events
	for i := 0; i < DefaultEventBatchSize; i++ {
		event := &eventbus.StoredEvent{
			Position:  int64(i + 1),
			Type:      "TestEvent",
			Data:      []byte("test data"),
			Timestamp: time.Now(),
		}
		buffer.Save(context.Background(), event)
	}

	// Wait for batch to be sent
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	count := requestCount
	mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 batch request, got %d", count)
	}
}

func TestEventBuffer_Stats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := &ebuv1.SaveEventsResponse{
			Positions: []int64{1},
		}
		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
		data, _ := proto.Marshal(resp)
		w.Write(data)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")
	defer buffer.Close()

	// Save an event
	event := &eventbus.StoredEvent{
		Position:  1,
		Type:      "TestEvent",
		Data:      []byte("test data"),
		Timestamp: time.Now(),
	}
	buffer.Save(context.Background(), event)

	// Force flush
	buffer.flush()
	time.Sleep(100 * time.Millisecond)

	stats := buffer.Stats()
	if stats.EventsSent != 1 {
		t.Errorf("expected 1 event sent, got %d", stats.EventsSent)
	}
	if stats.BatchesSent != 1 {
		t.Errorf("expected 1 batch sent, got %d", stats.BatchesSent)
	}
}

func TestEventBuffer_Load(t *testing.T) {
	var receivedAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("Authorization")

		// Return events with data
		resp := &ebuv1.LoadEventsResponse{
			Events: []*ebuv1.StoredEvent{
				{
					Position:  1,
					Type:      "TestEvent1",
					Data:      []byte("data1"),
					Timestamp: timestamppb.New(time.Now()),
				},
				{
					Position:  2,
					Type:      "TestEvent2",
					Data:      []byte("data2"),
					Timestamp: timestamppb.New(time.Now()),
				},
			},
		}
		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
		data, _ := proto.Marshal(resp)
		w.Write(data)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")
	defer buffer.Close()

	ctx := context.Background()
	events, err := buffer.Load(ctx, 0, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}

	// Verify event data
	if events[0].Position != 1 {
		t.Errorf("expected position 1, got %d", events[0].Position)
	}
	if events[0].Type != "TestEvent1" {
		t.Errorf("expected type TestEvent1, got %s", events[0].Type)
	}

	// Verify the authorization header was sent
	if receivedAPIKey != "Bearer test-api-key" {
		t.Errorf("expected Authorization header 'Bearer test-api-key', got %q", receivedAPIKey)
	}
}

func TestEventBuffer_GetPosition(t *testing.T) {
	var receivedAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("Authorization")

		resp := &ebuv1.GetPositionResponse{
			Position: 42,
		}
		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
		data, _ := proto.Marshal(resp)
		w.Write(data)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")
	defer buffer.Close()

	ctx := context.Background()
	pos, err := buffer.GetPosition(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pos != 42 {
		t.Errorf("expected position 42, got %d", pos)
	}

	// Verify the authorization header was sent
	if receivedAPIKey != "Bearer test-api-key" {
		t.Errorf("expected Authorization header 'Bearer test-api-key', got %q", receivedAPIKey)
	}
}

func TestEventBuffer_SaveSubscriptionPosition(t *testing.T) {
	var receivedAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("Authorization")

		resp := &ebuv1.SaveSubscriptionPositionResponse{}
		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
		data, _ := proto.Marshal(resp)
		w.Write(data)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")
	defer buffer.Close()

	ctx := context.Background()
	err := buffer.SaveSubscriptionPosition(ctx, "test-sub", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the authorization header was sent
	if receivedAPIKey != "Bearer test-api-key" {
		t.Errorf("expected Authorization header 'Bearer test-api-key', got %q", receivedAPIKey)
	}
}

func TestEventBuffer_LoadSubscriptionPosition(t *testing.T) {
	var receivedAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("Authorization")

		resp := &ebuv1.LoadSubscriptionPositionResponse{
			Position: 10,
		}
		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
		data, _ := proto.Marshal(resp)
		w.Write(data)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")
	defer buffer.Close()

	ctx := context.Background()
	pos, err := buffer.LoadSubscriptionPosition(ctx, "test-sub")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pos != 10 {
		t.Errorf("expected position 10, got %d", pos)
	}

	// Verify the authorization header was sent
	if receivedAPIKey != "Bearer test-api-key" {
		t.Errorf("expected Authorization header 'Bearer test-api-key', got %q", receivedAPIKey)
	}
}

func TestEventBuffer_Close(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := &ebuv1.SaveEventsResponse{
			Positions: []int64{1},
		}
		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
		data, _ := proto.Marshal(resp)
		w.Write(data)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")

	// Save an event
	event := &eventbus.StoredEvent{
		Position:  1,
		Type:      "TestEvent",
		Data:      []byte("test data"),
		Timestamp: time.Now(),
	}
	buffer.Save(context.Background(), event)

	// Close should flush remaining events
	err := buffer.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify stats show the event was sent
	time.Sleep(100 * time.Millisecond)
	stats := buffer.Stats()
	if stats.EventsSent != 1 {
		t.Errorf("expected 1 event sent after close, got %d", stats.EventsSent)
	}
}

func TestEventBuffer_EventStore_Interface(t *testing.T) {
	buffer := NewEventBuffer(http.DefaultClient, "https://lookhere.tech", "test-key")
	defer buffer.Close()

	// Verify the buffer implements EventStore interface
	var _ eventbus.EventStore = buffer
}

func TestEventBuffer_Load_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")
	defer buffer.Close()

	ctx := context.Background()
	_, err := buffer.Load(ctx, 0, 100)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestEventBuffer_GetPosition_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")
	defer buffer.Close()

	ctx := context.Background()
	_, err := buffer.GetPosition(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestEventBuffer_SaveSubscriptionPosition_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")
	defer buffer.Close()

	ctx := context.Background()
	err := buffer.SaveSubscriptionPosition(ctx, "test-sub", 10)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestEventBuffer_LoadSubscriptionPosition_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")
	defer buffer.Close()

	ctx := context.Background()
	_, err := buffer.LoadSubscriptionPosition(ctx, "test-sub")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestEventBuffer_sendBatch_Error(t *testing.T) {
	requestCount := 0
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")
	defer buffer.Close()

	// Send an event
	event := &eventbus.StoredEvent{
		Position:  1,
		Type:      "TestEvent",
		Data:      []byte("test data"),
		Timestamp: time.Now(),
	}
	buffer.Save(context.Background(), event)

	// Force flush
	buffer.flush()
	time.Sleep(100 * time.Millisecond)

	stats := buffer.Stats()
	if stats.BatchesFailed == 0 {
		t.Error("expected at least 1 failed batch")
	}
}

func TestEventBuffer_sendBatch_PositionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return wrong number of positions
		resp := &ebuv1.SaveEventsResponse{
			Positions: []int64{}, // Empty instead of matching events
		}
		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
		data, _ := proto.Marshal(resp)
		w.Write(data)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")
	defer buffer.Close()

	// Send an event
	event := &eventbus.StoredEvent{
		Position:  1,
		Type:      "TestEvent",
		Data:      []byte("test data"),
		Timestamp: time.Now(),
	}
	buffer.Save(context.Background(), event)

	// Force flush
	buffer.flush()
	time.Sleep(100 * time.Millisecond)

	// Should still succeed but log warning
	stats := buffer.Stats()
	if stats.EventsSent != 1 {
		t.Errorf("expected 1 event sent, got %d", stats.EventsSent)
	}
}

func TestEventBuffer_Flush_EmptyBuffer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not make request when buffer is empty")
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")
	defer buffer.Close()

	// Flush empty buffer - should not make any requests
	buffer.flush()
	time.Sleep(50 * time.Millisecond)
}

func TestEventBuffer_Flush_QueueFull_DropPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := &ebuv1.SaveEventsResponse{
			Positions: []int64{1},
		}
		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
		data, _ := proto.Marshal(resp)
		w.Write(data)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")

	// Stop workers to prevent queue from draining
	buffer.cancel()
	buffer.wg.Wait()

	// Replace with unbuffered channel to force immediate drop
	buffer.batchQueue = make(chan []*ebuv1.StoredEvent, 0)
	defer buffer.Close()

	// Add event to buffer
	event := &eventbus.StoredEvent{
		Position:  1,
		Type:      "TestEvent",
		Data:      []byte("test data"),
		Timestamp: time.Now(),
	}
	buffer.events = append(buffer.events, &ebuv1.StoredEvent{
		Position:  event.Position,
		Type:      event.Type,
		Data:      event.Data,
		Timestamp: timestamppb.New(event.Timestamp),
	})

	// First flush will block trying to send (channel has no buffer)
	go buffer.flush()
	time.Sleep(10 * time.Millisecond) // Let it block

	// Second flush should drop because channel is full
	buffer.flush()

	stats := buffer.Stats()
	if stats.BatchesDropped == 0 {
		t.Error("expected at least 1 dropped batch when queue is full")
	}
	if stats.EventsDropped == 0 {
		t.Error("expected at least 1 dropped event when queue is full")
	}
}

func TestEventBuffer_Save_BatchFull_QueueFull_DropPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := &ebuv1.SaveEventsResponse{
			Positions: make([]int64, DefaultEventBatchSize),
		}
		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
		data, _ := proto.Marshal(resp)
		w.Write(data)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")
	buffer.batchSize = 5 // Small batch for faster testing

	// Stop workers to prevent queue from draining
	buffer.cancel()
	buffer.wg.Wait()

	// Replace with unbuffered channel
	buffer.batchQueue = make(chan []*ebuv1.StoredEvent, 0)
	defer buffer.Close()

	// Add enough events to trigger batch size flush in Save
	for i := 0; i < 10; i++ { // Twice the batch size
		event := &eventbus.StoredEvent{
			Position:  int64(i + 1),
			Type:      "TestEvent",
			Data:      []byte("test data"),
			Timestamp: time.Now(),
		}
		buffer.Save(context.Background(), event)
	}

	stats := buffer.Stats()
	if stats.EventsDropped == 0 {
		t.Error("expected at least 1 dropped event in Save when queue is full")
	}
	if stats.BatchesDropped == 0 {
		t.Error("expected at least 1 dropped batch in Save when queue is full")
	}
}

func TestEventBuffer_Close_DrainsBatches(t *testing.T) {
	eventsSent := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		eventsSent++
		resp := &ebuv1.SaveEventsResponse{
			Positions: []int64{1},
		}
		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
		data, _ := proto.Marshal(resp)
		w.Write(data)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")

	// Fill buffer to trigger a batch
	for i := 0; i < 100; i++ {
		buffer.Save(context.Background(), &eventbus.StoredEvent{
			Position:  int64(i + 1),
			Type:      "TestEvent",
			Data:      []byte("test data"),
			Timestamp: time.Now(),
		})
	}

	// Close should drain the batch
	err := buffer.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify batch was sent
	if eventsSent == 0 {
		t.Error("expected batch to be sent during Close()")
	}
}

func TestEventBuffer_Worker_ChannelClosed(t *testing.T) {
	batchesProcessed := 0
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		batchesProcessed++
		mu.Unlock()
		resp := &ebuv1.SaveEventsResponse{
			Positions: []int64{1},
		}
		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
		data, _ := proto.Marshal(resp)
		w.Write(data)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")

	// Queue multiple batches directly to the channel
	// This ensures batches are in the queue when workers are running
	for i := 0; i < 5; i++ {
		batch := []*ebuv1.StoredEvent{
			{
				Position:  int64(i + 1),
				Type:      "TestEvent",
				Data:      []byte("test data"),
				Timestamp: timestamppb.New(time.Now()),
			},
		}
		buffer.batchQueue <- batch
	}

	// Give workers time to start processing
	time.Sleep(50 * time.Millisecond)

	// Now close the channel directly to trigger the !ok path
	// The workers will finish current batches and then hit the !ok case
	close(buffer.batchQueue)

	// Wait for workers to finish
	buffer.wg.Wait()

	mu.Lock()
	processed := batchesProcessed
	mu.Unlock()

	// Workers should have processed the batches before exiting
	if processed == 0 {
		t.Error("expected workers to process batches before exiting via !ok path")
	}
}

func TestEventBuffer_Close_EmptyQueue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := &ebuv1.SaveEventsResponse{
			Positions: []int64{1},
		}
		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
		data, _ := proto.Marshal(resp)
		w.Write(data)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")

	// Don't add any events - queue should be empty

	// Close with empty queue should hit the default case in the select
	err := buffer.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEventBuffer_Close_DrainsQueuedBatches(t *testing.T) {
	batchesProcessed := 0
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		batchesProcessed++
		mu.Unlock()
		resp := &ebuv1.SaveEventsResponse{
			Positions: []int64{1},
		}
		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
		data, _ := proto.Marshal(resp)
		w.Write(data)
	}))
	defer server.Close()

	buffer := NewEventBuffer(http.DefaultClient, server.URL, "test-api-key")

	// Stop workers first so batches will queue up
	buffer.cancel()
	buffer.wg.Wait()

	// Now add batches directly to the queue (workers are stopped)
	batch1 := []*ebuv1.StoredEvent{
		{
			Position:  1,
			Type:      "TestEvent",
			Data:      []byte("test data"),
			Timestamp: timestamppb.New(time.Now()),
		},
	}
	batch2 := []*ebuv1.StoredEvent{
		{
			Position:  2,
			Type:      "TestEvent",
			Data:      []byte("test data"),
			Timestamp: timestamppb.New(time.Now()),
		},
	}
	buffer.batchQueue <- batch1
	buffer.batchQueue <- batch2

	// Close should drain these batches (line 322-323 coverage)
	err := buffer.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	processed := batchesProcessed
	mu.Unlock()

	// Close should have drained and sent both batches
	if processed != 2 {
		t.Errorf("expected 2 batches to be drained and sent, got %d", processed)
	}
}
