package messenger_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"chanlistener/messenger"
)

const waitTimeout = 2 * time.Second

// collector is a concurrency-safe handler sink used across the tests.
type collector struct {
	mu   sync.Mutex
	got  []any
	done chan struct{}
	want int
}

func newCollector(want int) *collector {
	return &collector{done: make(chan struct{}), want: want}
}

func (c *collector) handle(_ context.Context, data any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, data)
	if len(c.got) == c.want {
		close(c.done)
	}
	return nil
}

func (c *collector) wait(t *testing.T) []any {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(waitTimeout):
		t.Fatalf("timed out waiting for %d messages", c.want)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]any(nil), c.got...)
}

// shutdown stops b and fails the test if the broker does not exit cleanly.
func shutdown(t *testing.T, b *messenger.Broker) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	if err := b.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func mustSubscribe(t *testing.T, b *messenger.Broker, topic string, h messenger.Handler) {
	t.Helper()
	if err := b.Subscribe(topic, h); err != nil {
		t.Fatalf("Subscribe(%q): %v", topic, err)
	}
}

// Regression: handlers used to receive the whole Message envelope instead of
// its payload.
func TestHandlerReceivesPayloadNotEnvelope(t *testing.T) {
	c := newCollector(1)
	b := messenger.New()
	mustSubscribe(t, b, "person", c.handle)
	b.Start(context.Background())
	defer shutdown(t, b)

	if err := b.Publish(messenger.Message{Topic: "person", Data: "yeison"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	got := c.wait(t)
	if len(got) != 1 || got[0] != "yeison" {
		t.Fatalf("handler got %#v, want payload \"yeison\"", got)
	}
}

// Regression: publishing before Start used to append to an unsynchronised
// buffer, racing with the dispatch loop.
func TestPublishBeforeStartIsDelivered(t *testing.T) {
	const n = 10
	c := newCollector(n)
	b := messenger.New()
	mustSubscribe(t, b, "early", c.handle)

	for i := 0; i < n; i++ {
		if err := b.Publish(messenger.Message{Topic: "early", Data: i}); err != nil {
			t.Fatalf("Publish(%d): %v", i, err)
		}
	}

	b.Start(context.Background())
	defer shutdown(t, b)

	if got := c.wait(t); len(got) != n {
		t.Fatalf("delivered %d messages, want %d", len(got), n)
	}
}

func TestFanOutToMultipleHandlers(t *testing.T) {
	var calls atomic.Int64
	done := make(chan struct{})
	count := func(_ context.Context, _ any) error {
		if calls.Add(1) == 3 {
			close(done)
		}
		return nil
	}

	b := messenger.New()
	for i := 0; i < 3; i++ {
		mustSubscribe(t, b, "topic", count)
	}
	b.Start(context.Background())
	defer shutdown(t, b)

	if err := b.Publish(messenger.Message{Topic: "topic", Data: 1}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatalf("only %d of 3 handlers ran", calls.Load())
	}
}

// Regression: an unknown topic produced a nil map lookup and panicked the
// dispatch goroutine.
func TestUnknownTopicReportsErrorWithoutPanicking(t *testing.T) {
	errs := make(chan error, 1)
	b := messenger.New(messenger.WithErrorHandler(func(_ messenger.Message, err error) {
		select {
		case errs <- err:
		default:
		}
	}))
	b.Start(context.Background())
	defer shutdown(t, b)

	if err := b.Publish(messenger.Message{Topic: "ghost", Data: 1}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case err := <-errs:
		if !errors.Is(err, messenger.ErrNoHandler) {
			t.Fatalf("got %v, want ErrNoHandler", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("no error reported for unknown topic")
	}
}

// Regression: a handler error used to call log.Panic and kill the process.
func TestHandlerErrorDoesNotStopTheBroker(t *testing.T) {
	boom := errors.New("boom")
	errs := make(chan error, 1)
	survived := newCollector(1)

	b := messenger.New(messenger.WithErrorHandler(func(_ messenger.Message, err error) {
		select {
		case errs <- err:
		default:
		}
	}))
	mustSubscribe(t, b, "bad", func(context.Context, any) error { return boom })
	mustSubscribe(t, b, "good", survived.handle)
	b.Start(context.Background())
	defer shutdown(t, b)

	if err := b.Publish(messenger.Message{Topic: "bad"}); err != nil {
		t.Fatalf("Publish(bad): %v", err)
	}
	if err := b.Publish(messenger.Message{Topic: "good", Data: "still alive"}); err != nil {
		t.Fatalf("Publish(good): %v", err)
	}

	select {
	case err := <-errs:
		if !errors.Is(err, boom) {
			t.Fatalf("got %v, want boom", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("handler error was never reported")
	}

	if got := survived.wait(t); got[0] != "still alive" {
		t.Fatalf("broker stopped after a handler error: %#v", got)
	}
}

// Regression: Stop closed the shared channel, so any in-flight producer
// panicked with "send on closed channel".
func TestPublishAfterShutdownReturnsErrClosed(t *testing.T) {
	b := messenger.New()
	mustSubscribe(t, b, "topic", func(context.Context, any) error { return nil })
	b.Start(context.Background())
	shutdown(t, b)

	if err := b.Publish(messenger.Message{Topic: "topic"}); !errors.Is(err, messenger.ErrClosed) {
		t.Fatalf("got %v, want ErrClosed", err)
	}
}

func TestShutdownDrainsQueuedMessages(t *testing.T) {
	const n = 50
	c := newCollector(n)
	release := make(chan struct{})

	b := messenger.New(messenger.WithBufferSize(n))
	mustSubscribe(t, b, "topic", func(ctx context.Context, data any) error {
		<-release // hold the dispatch loop until everything is queued
		return c.handle(ctx, data)
	})

	for i := 0; i < n; i++ {
		if err := b.Publish(messenger.Message{Topic: "topic", Data: i}); err != nil {
			t.Fatalf("Publish(%d): %v", i, err)
		}
	}
	b.Start(context.Background())
	close(release)

	shutdown(t, b)

	if got := c.wait(t); len(got) != n {
		t.Fatalf("drained %d messages, want %d", len(got), n)
	}
}

func TestShutdownIsIdempotentAndSafeBeforeStart(t *testing.T) {
	b := messenger.New()
	shutdown(t, b) // never started
	shutdown(t, b) // second call must not block or panic
}

func TestContextCancellationStopsTheBroker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := messenger.New()
	mustSubscribe(t, b, "topic", func(context.Context, any) error { return nil })
	b.Start(ctx)
	cancel()

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), waitTimeout)
	defer cancelShutdown()
	if err := b.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("broker did not stop after context cancellation: %v", err)
	}
}

func TestSubscribeValidation(t *testing.T) {
	b := messenger.New()
	tests := []struct {
		name    string
		topic   string
		handler messenger.Handler
		want    error
	}{
		{"empty topic", "", func(context.Context, any) error { return nil }, messenger.ErrEmptyTopic},
		{"nil handler", "topic", nil, messenger.ErrNilHandler},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := b.Subscribe(tt.topic, tt.handler); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}

	if err := b.Publish(messenger.Message{Topic: ""}); !errors.Is(err, messenger.ErrEmptyTopic) {
		t.Fatalf("Publish with empty topic: got %v, want ErrEmptyTopic", err)
	}
}

// Exercises the paths the race detector used to flag: concurrent Publish,
// concurrent Subscribe, and the dispatch loop reading both.
func TestConcurrentPublishAndSubscribe(t *testing.T) {
	const (
		producers   = 8
		perProducer = 50
	)
	var delivered atomic.Int64

	b := messenger.New(messenger.WithBufferSize(16))
	mustSubscribe(t, b, "topic", func(context.Context, any) error {
		delivered.Add(1)
		return nil
	})
	b.Start(context.Background())

	var wg sync.WaitGroup
	wg.Add(producers)
	for p := 0; p < producers; p++ {
		go func(p int) {
			defer wg.Done()
			for i := 0; i < perProducer; i++ {
				if err := b.Publish(messenger.Message{Topic: "topic", Data: p*perProducer + i}); err != nil {
					t.Errorf("Publish: %v", err)
					return
				}
			}
		}(p)
	}

	// Register handlers while the broker is hot to exercise copy-on-write.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			// t.Fatalf is illegal off the test goroutine; report and stop.
			if err := b.Subscribe("other", func(context.Context, any) error { return nil }); err != nil {
				t.Errorf("Subscribe: %v", err)
				return
			}
		}
	}()

	wg.Wait()
	shutdown(t, b)

	if got, want := delivered.Load(), int64(producers*perProducer); got != want {
		t.Fatalf("delivered %d messages, want %d", got, want)
	}
}
