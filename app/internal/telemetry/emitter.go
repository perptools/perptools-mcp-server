// Package telemetry emits one usage event per MCP tool invocation to the
// perptools backend's existing /v1/event endpoint. Emission is asynchronous
// and best-effort: a failed or dropped event never blocks, fails, or slows a
// tool call.
package telemetry

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"mcp-server/app/internal/clients/perptools"
)

// EventType is the backend event-type identifier for MCP tool invocations.
const EventType = "mcp_tool_called"

// SentinelPublicKey (the Solana system program address) is used as public_key
// for pre-auth calls where no wallet is known (e.g. get_markets, health).
const SentinelPublicKey = "11111111111111111111111111111111"

// EventData is the privacy-safe payload of an mcp_tool_called event. It must
// never carry tool arguments, signatures, keys, transaction blobs, or raw
// error text — only the closed set of fields below. Deliberately minimal:
// the event type identifies the source, created_at in the DB covers timing.
type EventData struct {
	Tool          string `json:"tool"`
	Success       bool   `json:"success"`
	ErrorCategory string `json:"error_category,omitempty"`
	DurationMS    int64  `json:"duration_ms"`
}

// Event is one usage event queued for delivery.
type Event struct {
	PublicKey string
	Data      EventData
}

// Config configures the emitter.
type Config struct {
	// BaseURL of the perptools API, e.g. "https://app.perptools.ai/api".
	BaseURL string
	// Disabled constructs a no-op emitter (TELEMETRY_DISABLED=1).
	Disabled bool
}

// sendFunc delivers one event; injectable for tests.
type sendFunc func(ctx context.Context, ev Event) error

const (
	defaultBuffer = 256
	sendTimeout   = 5 * time.Second
	closeTimeout  = 2 * time.Second
	// logEveryN throttles failure logging: log the 1st failure, then every Nth.
	logEveryN = 50
)

// Emitter queues usage events and delivers them in the background. Emit never
// blocks: when the buffer is full the event is dropped and counted.
type Emitter struct {
	cfg  Config
	send sendFunc

	mu     sync.RWMutex // guards ch against send-after-close
	ch     chan Event
	closed bool
	done   chan struct{}

	dropped  atomic.Uint64
	failures atomic.Uint64
}

// NewEmitter builds an emitter with its own unsigned perptools client. The
// client is deliberately NOT shared with the service: the service's client is
// replaced on authentication, and event delivery needs no signature.
func NewEmitter(cfg Config) *Emitter {
	if cfg.Disabled {
		return &Emitter{cfg: cfg, closed: true}
	}
	pt := perptools.NewClient(cfg.BaseURL)
	send := func(ctx context.Context, ev Event) error {
		raw, err := json.Marshal(ev.Data)
		if err != nil {
			return err
		}
		return pt.CreateEvent(ctx, perptools.CreateEventRequest{
			PublicKey: ev.PublicKey,
			Type:      EventType,
			Data:      raw,
		})
	}
	return newEmitter(cfg, send, defaultBuffer)
}

// newEmitter is the test-injectable constructor.
func newEmitter(cfg Config, send sendFunc, buffer int) *Emitter {
	e := &Emitter{
		cfg:  cfg,
		send: send,
		ch:   make(chan Event, buffer),
		done: make(chan struct{}),
	}
	go e.drain()
	return e
}

// Emit enqueues an event without blocking. Full buffer or closed emitter
// means the event is dropped (and counted).
func (e *Emitter) Emit(ev Event) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed {
		e.dropped.Add(1)
		return
	}
	select {
	case e.ch <- ev:
	default:
		e.dropped.Add(1)
	}
}

// Close stops accepting events and drains what is already queued, bounded by
// closeTimeout. Safe to call on a disabled emitter and more than once.
func (e *Emitter) Close() {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.closed = true
	close(e.ch)
	e.mu.Unlock()

	select {
	case <-e.done:
	case <-time.After(closeTimeout):
	}
}

// Dropped reports how many events were discarded due to a full buffer or a
// closed emitter.
func (e *Emitter) Dropped() uint64 {
	return e.dropped.Load()
}

func (e *Emitter) drain() {
	defer close(e.done)
	for ev := range e.ch {
		// Deliberately context.Background(): the tool's context is canceled
		// as soon as its handler returns.
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		err := e.send(ctx, ev)
		cancel()
		if err != nil {
			e.logFailure(err)
		}
	}
}

// logFailure logs send failures, throttled to the first and then every Nth,
// so a down backend cannot flood the log. Never logs event contents.
func (e *Emitter) logFailure(err error) {
	n := e.failures.Add(1)
	if n == 1 || n%logEveryN == 0 {
		log.Printf("telemetry: event send failed (count=%d, dropped=%d): %v", n, e.Dropped(), err)
	}
}
