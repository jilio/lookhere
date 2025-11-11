package lookhere

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	eventbus "github.com/jilio/ebu"
	ebuv1 "github.com/jilio/lookhere/gen/ebu/v1"
	"github.com/jilio/lookhere/gen/ebu/v1/ebuv1connect"
)

func TestNewRemoteStore(t *testing.T) {
	store := NewRemoteStore("https://lookhere.tech", "test-api-key")

	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if store.apiKey != "test-api-key" {
		t.Errorf("expected apiKey 'test-api-key', got %q", store.apiKey)
	}
	if store.client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewRemoteStore_HTTPClientConfiguration(t *testing.T) {
	store := NewRemoteStore("https://lookhere.tech", "test-key")

	// We can't directly access the http.Client from the Connect client,
	// but we can verify the store was created successfully
	if store == nil {
		t.Fatal("expected non-nil store")
	}

	// Verify the store implements EventStore interface
	var _ eventbus.EventStore = store
}

func TestRemoteStore_Save(t *testing.T) {
	// Create a test server
	var receivedAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("Authorization")

		// Verify it's a SaveEvent request
		if r.URL.Path != "/ebu.v1.EventService/SaveEvent" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := NewRemoteStore(server.URL, "test-api-key")

	event := &eventbus.StoredEvent{
		Position:  0,
		Type:      "TestEvent",
		Data:      []byte("test data"),
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	_ = store.Save(ctx, event)

	// Verify the authorization header was sent
	if receivedAPIKey != "Bearer test-api-key" {
		t.Errorf("expected Authorization header 'Bearer test-api-key', got %q", receivedAPIKey)
	}
}

func TestRemoteStore_Load(t *testing.T) {
	var receivedAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("Authorization")

		// Verify it's a LoadEvents request
		if r.URL.Path != "/ebu.v1.EventService/LoadEvents" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := NewRemoteStore(server.URL, "test-api-key")

	ctx := context.Background()
	_, _ = store.Load(ctx, 0, 100)

	// Verify the authorization header was sent
	if receivedAPIKey != "Bearer test-api-key" {
		t.Errorf("expected Authorization header 'Bearer test-api-key', got %q", receivedAPIKey)
	}
}

func TestRemoteStore_GetPosition(t *testing.T) {
	var receivedAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("Authorization")

		// Verify it's a GetPosition request
		if r.URL.Path != "/ebu.v1.EventService/GetPosition" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := NewRemoteStore(server.URL, "test-api-key")

	ctx := context.Background()
	_, _ = store.GetPosition(ctx)

	// Verify authorization header
	if receivedAPIKey != "Bearer test-api-key" {
		t.Errorf("expected Authorization header 'Bearer test-api-key', got %q", receivedAPIKey)
	}
}

func TestRemoteStore_SaveSubscriptionPosition(t *testing.T) {
	var receivedAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("Authorization")

		// Verify it's a SaveSubscriptionPosition request
		if r.URL.Path != "/ebu.v1.EventService/SaveSubscriptionPosition" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := NewRemoteStore(server.URL, "test-api-key")

	ctx := context.Background()
	_ = store.SaveSubscriptionPosition(ctx, "test-subscription", 42)

	// Verify authorization header
	if receivedAPIKey != "Bearer test-api-key" {
		t.Errorf("expected Authorization header 'Bearer test-api-key', got %q", receivedAPIKey)
	}
}

func TestRemoteStore_LoadSubscriptionPosition(t *testing.T) {
	var receivedAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("Authorization")

		// Verify it's a LoadSubscriptionPosition request
		if r.URL.Path != "/ebu.v1.EventService/LoadSubscriptionPosition" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/proto")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := NewRemoteStore(server.URL, "test-api-key")

	ctx := context.Background()
	_, _ = store.LoadSubscriptionPosition(ctx, "test-subscription")

	// Verify authorization header
	if receivedAPIKey != "Bearer test-api-key" {
		t.Errorf("expected Authorization header 'Bearer test-api-key', got %q", receivedAPIKey)
	}
}

func TestRemoteStore_ImplementsEventStore(t *testing.T) {
	// Compile-time check that RemoteStore implements EventStore
	var _ eventbus.EventStore = (*RemoteStore)(nil)

	// Runtime check
	store := NewRemoteStore("https://lookhere.tech", "test-key")
	var _ eventbus.EventStore = store
}

func TestRemoteStore_ContextCancellation(t *testing.T) {
	// Create a server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := NewRemoteStore(server.URL, "test-key")

	// Create a context that cancels immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.Save(ctx, &eventbus.StoredEvent{
		Type:      "TestEvent",
		Data:      []byte("test"),
		Timestamp: time.Now(),
	})

	// Should get a context cancellation error
	if err == nil {
		t.Error("expected error due to context cancellation, got nil")
	}
}

// Integration tests with actual Connect server

type mockEventService struct {
	ebuv1connect.UnimplementedEventServiceHandler
	savedEvents           []*ebuv1.StoredEvent
	subscriptionPositions map[string]int64
	lastSavePosition      int64
	receivedAuthHeaders   []string
}

func (m *mockEventService) SaveEvent(
	ctx context.Context,
	req *connect.Request[ebuv1.SaveEventRequest],
) (*connect.Response[ebuv1.SaveEventResponse], error) {
	m.receivedAuthHeaders = append(m.receivedAuthHeaders, req.Header().Get("Authorization"))
	m.lastSavePosition++
	m.savedEvents = append(m.savedEvents, req.Msg.Event)
	return connect.NewResponse(&ebuv1.SaveEventResponse{
		Position: m.lastSavePosition,
	}), nil
}

func (m *mockEventService) LoadEvents(
	ctx context.Context,
	req *connect.Request[ebuv1.LoadEventsRequest],
) (*connect.Response[ebuv1.LoadEventsResponse], error) {
	m.receivedAuthHeaders = append(m.receivedAuthHeaders, req.Header().Get("Authorization"))
	return connect.NewResponse(&ebuv1.LoadEventsResponse{
		Events: m.savedEvents,
	}), nil
}

func (m *mockEventService) GetPosition(
	ctx context.Context,
	req *connect.Request[ebuv1.GetPositionRequest],
) (*connect.Response[ebuv1.GetPositionResponse], error) {
	m.receivedAuthHeaders = append(m.receivedAuthHeaders, req.Header().Get("Authorization"))
	return connect.NewResponse(&ebuv1.GetPositionResponse{
		Position: m.lastSavePosition,
	}), nil
}

func (m *mockEventService) SaveSubscriptionPosition(
	ctx context.Context,
	req *connect.Request[ebuv1.SaveSubscriptionPositionRequest],
) (*connect.Response[ebuv1.SaveSubscriptionPositionResponse], error) {
	m.receivedAuthHeaders = append(m.receivedAuthHeaders, req.Header().Get("Authorization"))
	if m.subscriptionPositions == nil {
		m.subscriptionPositions = make(map[string]int64)
	}
	m.subscriptionPositions[req.Msg.SubscriptionId] = req.Msg.Position
	return connect.NewResponse(&ebuv1.SaveSubscriptionPositionResponse{}), nil
}

func (m *mockEventService) LoadSubscriptionPosition(
	ctx context.Context,
	req *connect.Request[ebuv1.LoadSubscriptionPositionRequest],
) (*connect.Response[ebuv1.LoadSubscriptionPositionResponse], error) {
	m.receivedAuthHeaders = append(m.receivedAuthHeaders, req.Header().Get("Authorization"))
	pos := m.subscriptionPositions[req.Msg.SubscriptionId]
	return connect.NewResponse(&ebuv1.LoadSubscriptionPositionResponse{
		Position: pos,
	}), nil
}

func (m *mockEventService) ReportTelemetry(
	ctx context.Context,
	stream *connect.ClientStream[ebuv1.TelemetryBatch],
) (*connect.Response[ebuv1.TelemetryResponse], error) {
	count := int64(0)
	for stream.Receive() {
		batch := stream.Msg()
		count += int64(len(batch.Metrics))
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	return connect.NewResponse(&ebuv1.TelemetryResponse{
		ReceivedCount: count,
	}), nil
}

func TestRemoteStore_Integration(t *testing.T) {
	// Create mock service
	mock := &mockEventService{
		subscriptionPositions: make(map[string]int64),
	}

	// Create test server with Connect
	mux := http.NewServeMux()
	path, handler := ebuv1connect.NewEventServiceHandler(mock)
	mux.Handle(path, handler)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Create remote store
	store := NewRemoteStore(server.URL, "test-api-key")

	ctx := context.Background()

	// Test Save
	t.Run("Save", func(t *testing.T) {
		event := &eventbus.StoredEvent{
			Type:      "UserCreated",
			Data:      []byte(`{"id": "123"}`),
			Timestamp: time.Now(),
		}

		err := store.Save(ctx, event)
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		if event.Position != 1 {
			t.Errorf("expected position 1, got %d", event.Position)
		}

		if len(mock.savedEvents) != 1 {
			t.Fatalf("expected 1 saved event, got %d", len(mock.savedEvents))
		}

		if mock.savedEvents[0].Type != "UserCreated" {
			t.Errorf("expected event type 'UserCreated', got %q", mock.savedEvents[0].Type)
		}
	})

	// Test Load
	t.Run("Load", func(t *testing.T) {
		events, err := store.Load(ctx, 0, 100)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if len(events) != 1 {
			t.Errorf("expected 1 event, got %d", len(events))
		}

		if events[0].Type != "UserCreated" {
			t.Errorf("expected event type 'UserCreated', got %q", events[0].Type)
		}
	})

	// Test GetPosition
	t.Run("GetPosition", func(t *testing.T) {
		pos, err := store.GetPosition(ctx)
		if err != nil {
			t.Fatalf("GetPosition failed: %v", err)
		}

		if pos != 1 {
			t.Errorf("expected position 1, got %d", pos)
		}
	})

	// Test SaveSubscriptionPosition
	t.Run("SaveSubscriptionPosition", func(t *testing.T) {
		err := store.SaveSubscriptionPosition(ctx, "sub-1", 42)
		if err != nil {
			t.Fatalf("SaveSubscriptionPosition failed: %v", err)
		}

		if mock.subscriptionPositions["sub-1"] != 42 {
			t.Errorf("expected subscription position 42, got %d", mock.subscriptionPositions["sub-1"])
		}
	})

	// Test LoadSubscriptionPosition
	t.Run("LoadSubscriptionPosition", func(t *testing.T) {
		pos, err := store.LoadSubscriptionPosition(ctx, "sub-1")
		if err != nil {
			t.Fatalf("LoadSubscriptionPosition failed: %v", err)
		}

		if pos != 42 {
			t.Errorf("expected position 42, got %d", pos)
		}
	})

	// Verify all authorization headers
	t.Run("AuthorizationHeaders", func(t *testing.T) {
		expectedAuth := "Bearer test-api-key"
		for i, auth := range mock.receivedAuthHeaders {
			if auth != expectedAuth {
				t.Errorf("request %d: expected Authorization %q, got %q", i, expectedAuth, auth)
			}
		}
	})
}

func TestRemoteStore_ErrorHandling(t *testing.T) {
	// Create a server that returns errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	store := NewRemoteStore(server.URL, "test-key")
	ctx := context.Background()

	t.Run("Save error", func(t *testing.T) {
		err := store.Save(ctx, &eventbus.StoredEvent{Type: "Test", Data: []byte("test"), Timestamp: time.Now()})
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("Load error", func(t *testing.T) {
		_, err := store.Load(ctx, 0, 100)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("GetPosition error", func(t *testing.T) {
		_, err := store.GetPosition(ctx)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("SaveSubscriptionPosition error", func(t *testing.T) {
		err := store.SaveSubscriptionPosition(ctx, "sub-1", 42)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("LoadSubscriptionPosition error", func(t *testing.T) {
		_, err := store.LoadSubscriptionPosition(ctx, "sub-1")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestRemoteStore_TLSConfiguration(t *testing.T) {
	// This test verifies that TLS configuration exists
	// We can't easily test the actual TLS settings without setting up a TLS server
	// But we can verify the store creation doesn't panic with TLS hosts
	store := NewRemoteStore("https://lookhere.tech", "test-key")
	if store == nil {
		t.Fatal("expected non-nil store for https host")
	}

	// Verify we can create requests without errors
	ctx := context.Background()
	event := &eventbus.StoredEvent{
		Type:      "Test",
		Data:      []byte("test"),
		Timestamp: time.Now(),
	}

	// This will fail to connect, but it should fail gracefully
	err := store.Save(ctx, event)
	if err == nil {
		t.Error("expected connection error to non-existent host")
	}
}
