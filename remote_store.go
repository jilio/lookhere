package lookhere

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"
	eventbus "github.com/jilio/ebu"
	ebuv1 "github.com/jilio/lookhere/gen/ebu/v1"
	"github.com/jilio/lookhere/gen/ebu/v1/ebuv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// RemoteStore implements eventbus.EventStore by delegating to a remote LOOKHERE server
type RemoteStore struct {
	client ebuv1connect.EventServiceClient
	apiKey string
}

// NewRemoteStore creates a new remote EventStore client with secure defaults
// Deprecated: Use NewRemoteStoreWithClient to share HTTP client configuration
func NewRemoteStore(host, apiKey string) *RemoteStore {
	httpClient := &http.Client{
		Timeout: 30 * time.Second, // Overall request timeout
		Transport: &http.Transport{
			MaxIdleConns:          100,              // Maximum idle connections
			MaxIdleConnsPerHost:   10,               // Maximum idle connections per host
			IdleConnTimeout:       90 * time.Second, // How long idle connections stay open
			TLSHandshakeTimeout:   10 * time.Second, // TLS handshake timeout
			ResponseHeaderTimeout: 10 * time.Second, // Time to wait for response headers
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12, // Enforce TLS 1.2+
			},
		},
	}
	return NewRemoteStoreWithClient(httpClient, host, apiKey)
}

// NewRemoteStoreWithClient creates a new remote EventStore client with a provided HTTP client
func NewRemoteStoreWithClient(httpClient *http.Client, host, apiKey string) *RemoteStore {
	client := ebuv1connect.NewEventServiceClient(httpClient, host)

	return &RemoteStore{
		client: client,
		apiKey: apiKey,
	}
}

// Save saves a single event to the remote storage
func (r *RemoteStore) Save(ctx context.Context, event *eventbus.StoredEvent) error {
	req := connect.NewRequest(&ebuv1.SaveEventRequest{
		Event: &ebuv1.StoredEvent{
			Position:  event.Position,
			Type:      event.Type,
			Data:      event.Data,
			Timestamp: timestamppb.New(event.Timestamp),
		},
	})

	// Add authorization header
	req.Header().Set("Authorization", "Bearer "+r.apiKey)

	resp, err := r.client.SaveEvent(ctx, req)
	if err != nil {
		return fmt.Errorf("save event: %w", err)
	}

	// Update position from server response
	event.Position = resp.Msg.Position

	return nil
}

// Load loads events from remote storage within a position range
func (r *RemoteStore) Load(ctx context.Context, from, to int64) ([]*eventbus.StoredEvent, error) {
	req := connect.NewRequest(&ebuv1.LoadEventsRequest{
		From: from,
		To:   to,
	})

	// Add authorization header
	req.Header().Set("Authorization", "Bearer "+r.apiKey)

	resp, err := r.client.LoadEvents(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("load events: %w", err)
	}

	// Convert proto events to StoredEvents
	events := make([]*eventbus.StoredEvent, len(resp.Msg.Events))
	for i, protoEvent := range resp.Msg.Events {
		events[i] = &eventbus.StoredEvent{
			Position:  protoEvent.Position,
			Type:      protoEvent.Type,
			Data:      protoEvent.Data,
			Timestamp: protoEvent.Timestamp.AsTime(),
		}
	}

	return events, nil
}

// GetPosition returns the current position (highest event number) from remote storage
func (r *RemoteStore) GetPosition(ctx context.Context) (int64, error) {
	req := connect.NewRequest(&ebuv1.GetPositionRequest{})

	// Add authorization header
	req.Header().Set("Authorization", "Bearer "+r.apiKey)

	resp, err := r.client.GetPosition(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("get position: %w", err)
	}

	return resp.Msg.Position, nil
}

// SaveSubscriptionPosition saves a subscription's position to remote storage
func (r *RemoteStore) SaveSubscriptionPosition(ctx context.Context, subscriptionID string, position int64) error {
	req := connect.NewRequest(&ebuv1.SaveSubscriptionPositionRequest{
		SubscriptionId: subscriptionID,
		Position:       position,
	})

	// Add authorization header
	req.Header().Set("Authorization", "Bearer "+r.apiKey)

	_, err := r.client.SaveSubscriptionPosition(ctx, req)
	if err != nil {
		return fmt.Errorf("save subscription position: %w", err)
	}

	return nil
}

// LoadSubscriptionPosition loads a subscription's saved position from remote storage
func (r *RemoteStore) LoadSubscriptionPosition(ctx context.Context, subscriptionID string) (int64, error) {
	req := connect.NewRequest(&ebuv1.LoadSubscriptionPositionRequest{
		SubscriptionId: subscriptionID,
	})

	// Add authorization header
	req.Header().Set("Authorization", "Bearer "+r.apiKey)

	resp, err := r.client.LoadSubscriptionPosition(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("load subscription position: %w", err)
	}

	return resp.Msg.Position, nil
}

// Ensure RemoteStore implements eventbus.EventStore
var _ eventbus.EventStore = (*RemoteStore)(nil)
