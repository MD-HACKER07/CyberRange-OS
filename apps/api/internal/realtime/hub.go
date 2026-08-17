// Package realtime is the WebSocket fan-out layer. Every live surface in the
// platform (terminal stream, copilot tokens, SIEM alert feed, faculty class
// progress) publishes to a Redis channel; each API instance subscribes and
// pushes to its locally connected WebSocket clients, so the platform scales
// horizontally without sticky sessions.
package realtime

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type Event struct {
	Type      string          `json:"type"`
	Channel   string          `json:"channel"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp time.Time       `json:"ts"`
}

type Hub struct {
	rdb *redis.Client
	log zerolog.Logger

	mu   sync.RWMutex
	subs map[string]map[int64]chan Event
	next int64

	cancels map[string]context.CancelFunc
}

func NewHub(rdb *redis.Client, log zerolog.Logger) *Hub {
	return &Hub{
		rdb:     rdb,
		log:     log,
		subs:    map[string]map[int64]chan Event{},
		cancels: map[string]context.CancelFunc{},
	}
}

func ChannelTerminal(sessionID string) string { return "rt:session:" + sessionID + ":terminal" }
func ChannelCopilot(sessionID string) string  { return "rt:session:" + sessionID + ":copilot" }
func ChannelAlerts(scope string) string       { return "rt:alerts:" + scope }
func ChannelClass(batchID string) string      { return "rt:class:" + batchID }
func ChannelAISec() string                    { return "rt:aisec" }

// Publish sends an event to all platform instances.
func (h *Hub) Publish(ctx context.Context, channel, eventType string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		h.log.Error().Err(err).Str("channel", channel).Msg("realtime marshal failed")
		return
	}
	ev := Event{Type: eventType, Channel: channel, Payload: raw, Timestamp: time.Now().UTC()}
	blob, err := json.Marshal(ev)
	if err != nil {
		return
	}
	if err := h.rdb.Publish(ctx, channel, blob).Err(); err != nil {
		h.log.Warn().Err(err).Str("channel", channel).Msg("redis publish failed; delivering locally only")
		h.deliverLocal(ev)
	}
}

// Subscribe registers a local listener and lazily starts the Redis
// subscription for that channel.
func (h *Hub) Subscribe(ctx context.Context, channel string) (<-chan Event, func()) {
	ch := make(chan Event, 256)

	h.mu.Lock()
	id := h.next
	h.next++
	if h.subs[channel] == nil {
		h.subs[channel] = map[int64]chan Event{}
	}
	h.subs[channel][id] = ch
	needsPump := h.cancels[channel] == nil
	if needsPump {
		pumpCtx, cancel := context.WithCancel(context.Background())
		h.cancels[channel] = cancel
		go h.pump(pumpCtx, channel)
	}
	h.mu.Unlock()

	cleanup := func() {
		h.mu.Lock()
		if subs, ok := h.subs[channel]; ok {
			if c, ok := subs[id]; ok {
				delete(subs, id)
				close(c)
			}
			if len(subs) == 0 {
				delete(h.subs, channel)
				if cancel, ok := h.cancels[channel]; ok {
					cancel()
					delete(h.cancels, channel)
				}
			}
		}
		h.mu.Unlock()
	}
	return ch, cleanup
}

func (h *Hub) pump(ctx context.Context, channel string) {
	sub := h.rdb.Subscribe(ctx, channel)
	defer func() { _ = sub.Close() }()
	msgs := sub.Channel(redis.WithChannelSize(512))
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-msgs:
			if !ok {
				return
			}
			var ev Event
			if err := json.Unmarshal([]byte(m.Payload), &ev); err != nil {
				continue
			}
			h.deliverLocal(ev)
		}
	}
}

func (h *Hub) deliverLocal(ev Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.subs[ev.Channel] {
		select {
		case ch <- ev:
		default: // slow consumer: drop rather than block the pump
		}
	}
}
