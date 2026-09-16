// Package messenger provides a small, concurrency-safe in-process
// publish/subscribe broker built on a single buffered channel.
//
// The broker owns one dispatch goroutine. Producers hand messages over with
// Publish and never block on handler execution; handlers run sequentially on
// the dispatch goroutine, so a slow handler applies natural back-pressure to
// producers once the queue fills up.
package messenger

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
)

// Sentinel errors, comparable with errors.Is.
var (
	// ErrClosed is returned by Publish once the broker has been shut down.
	ErrClosed = errors.New("messenger: broker is closed")
	// ErrEmptyTopic is returned when a topic is the empty string.
	ErrEmptyTopic = errors.New("messenger: empty topic")
	// ErrNilHandler is returned when Subscribe is given a nil handler.
	ErrNilHandler = errors.New("messenger: nil handler")
	// ErrNoHandler is reported to the ErrorHandler when a message is
	// published on a topic nobody subscribed to.
	ErrNoHandler = errors.New("messenger: no handler registered for topic")
)

// defaultBufferSize is the queue depth used when WithBufferSize is not given.
const defaultBufferSize = 64

// Handler consumes the payload of a message. It receives Message.Data, never
// the envelope, so handlers stay decoupled from the transport shape.
type Handler func(ctx context.Context, data any) error

// ErrorHandler observes delivery failures: a handler returning an error, or a
// message published on a topic with no subscribers. It must not panic; a
// failing handler never takes the broker down.
type ErrorHandler func(msg Message, err error)

// Message is the envelope carried over the broker.
type Message struct {
	Topic string
	Data  any
}

// Broker routes messages to the handlers subscribed to their topic.
// The zero value is not usable; construct one with New.
type Broker struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
	started  bool

	queue   chan Message
	onError ErrorHandler

	done    chan struct{} // closed by Shutdown: stop accepting new publishes
	stopped chan struct{} // closed by the dispatch loop once it has exited

	startOnce sync.Once
	closeOnce sync.Once
}

// Option customises a Broker at construction time.
type Option func(*Broker)

// WithBufferSize sets the queue depth. Non-positive values are ignored.
func WithBufferSize(n int) Option {
	return func(b *Broker) {
		if n > 0 {
			b.queue = make(chan Message, n)
		}
	}
}

// WithErrorHandler replaces the default logging error handler. A nil handler
// is ignored so the broker always has somewhere to report failures.
func WithErrorHandler(h ErrorHandler) Option {
	return func(b *Broker) {
		if h != nil {
			b.onError = h
		}
	}
}

// New builds a Broker. Messages may be published before Start; they sit in the
// queue and are delivered as soon as the dispatch loop is running.
func New(opts ...Option) *Broker {
	b := &Broker{
		handlers: make(map[string][]Handler),
		queue:    make(chan Message, defaultBufferSize),
		onError: func(msg Message, err error) {
			log.Printf("messenger: topic %q: %v", msg.Topic, err)
		},
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Subscribe registers h for topic. Several handlers may share a topic; they
// are invoked in registration order. Safe to call while the broker is running.
func (b *Broker) Subscribe(topic string, h Handler) error {
	if topic == "" {
		return ErrEmptyTopic
	}
	if h == nil {
		return fmt.Errorf("%w: topic %q", ErrNilHandler, topic)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Copy-on-write: the dispatch loop reads the slice under RLock and must
	// never observe an append mutating the backing array underneath it.
	current := b.handlers[topic]
	next := make([]Handler, len(current), len(current)+1)
	copy(next, current)
	b.handlers[topic] = append(next, h)
	return nil
}

// Publish enqueues msg for delivery. It blocks while the queue is full and
// returns ErrClosed once the broker is shutting down.
func (b *Broker) Publish(msg Message) error {
	if msg.Topic == "" {
		return ErrEmptyTopic
	}

	// Prefer the closed signal when both are ready, so a shut-down broker
	// reports ErrClosed deterministically instead of silently accepting.
	select {
	case <-b.done:
		return ErrClosed
	default:
	}

	select {
	case b.queue <- msg:
		return nil
	case <-b.done:
		return ErrClosed
	}
}

// Start launches the dispatch loop and returns immediately. It is idempotent.
// Cancelling ctx stops the broker without draining the queue; use Shutdown for
// a graceful stop.
func (b *Broker) Start(ctx context.Context) {
	b.startOnce.Do(func() {
		b.mu.Lock()
		b.started = true
		b.mu.Unlock()
		go b.dispatch(ctx)
	})
}

// Shutdown stops accepting publishes, waits for the queue to drain, and blocks
// until the dispatch loop exits or ctx is done. It is idempotent and returns
// ctx.Err() if the deadline passes before the loop finishes.
func (b *Broker) Shutdown(ctx context.Context) error {
	b.closeOnce.Do(func() { close(b.done) })

	b.mu.RLock()
	started := b.started
	b.mu.RUnlock()
	if !started {
		return nil // nothing is running, so nothing to wait for
	}

	select {
	case <-b.stopped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// dispatch is the single consumer of the queue.
func (b *Broker) dispatch(ctx context.Context) {
	defer close(b.stopped)

	for {
		select {
		case msg := <-b.queue:
			b.deliver(ctx, msg)
		case <-b.done:
			b.drain(ctx)
			return
		case <-ctx.Done():
			return // hard stop: the caller cancelled, drop what is queued
		}
	}
}

// drain flushes messages already queued at shutdown time.
func (b *Broker) drain(ctx context.Context) {
	for {
		select {
		case msg := <-b.queue:
			b.deliver(ctx, msg)
		default:
			return
		}
	}
}

// deliver fans msg out to its handlers, reporting every failure instead of
// aborting: one bad handler must not stop the broker or its siblings.
func (b *Broker) deliver(ctx context.Context, msg Message) {
	b.mu.RLock()
	handlers := b.handlers[msg.Topic]
	b.mu.RUnlock()

	if len(handlers) == 0 {
		b.onError(msg, fmt.Errorf("%w: %q", ErrNoHandler, msg.Topic))
		return
	}

	for _, h := range handlers {
		if err := h(ctx, msg.Data); err != nil {
			b.onError(msg, err)
		}
	}
}
