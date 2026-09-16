// Command chan-listener demonstrates the messenger broker: two topics, one
// bounded producer, and a graceful shutdown that drains what is still queued.
package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os/signal"
	"syscall"
	"time"

	"chanlistener/messenger"
)

const (
	messageCount    = 20
	produceInterval = 50 * time.Millisecond
	shutdownTimeout = 5 * time.Second
)

// Person is the payload published on the "person" topic.
type Person struct {
	ID   int
	Name string
}

// Animal is the payload published on the "animal" topic.
type Animal struct {
	ID   int
	Name string
	Type string
}

func logPerson(_ context.Context, data any) error {
	p, ok := data.(Person)
	if !ok {
		return fmt.Errorf("person handler: unexpected payload %T", data)
	}
	log.Println("person received:", p)
	return nil
}

func logAnimal(_ context.Context, data any) error {
	a, ok := data.(Animal)
	if !ok {
		return fmt.Errorf("animal handler: unexpected payload %T", data)
	}
	log.Println("animal received:", a)
	return nil
}

func main() {
	// Cancelled on SIGINT/SIGTERM so Ctrl-C triggers the graceful path.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	broker := messenger.New()
	for topic, handler := range map[string]messenger.Handler{
		"person": logPerson,
		"animal": logAnimal,
	} {
		if err := broker.Subscribe(topic, handler); err != nil {
			log.Fatalf("subscribe %q: %v", topic, err)
		}
	}

	broker.Start(ctx)
	produce(ctx, broker, messageCount)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := broker.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	log.Println("broker stopped cleanly")
}

// produce publishes n messages, alternating topics on a random draw, and
// returns early if ctx is cancelled or the broker closes.
func produce(ctx context.Context, broker *messenger.Broker, n int) {
	// Seed once: re-seeding inside the loop is both wasteful and, in the
	// original code, a no-op because the new source was discarded.
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	ticker := time.NewTicker(produceInterval)
	defer ticker.Stop()

	for i := 0; i < n; i++ {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		if err := broker.Publish(newMessage(rng.Intn(100))); err != nil {
			log.Printf("publish: %v", err)
			return
		}
	}
}

// newMessage routes even numbers to "person" and odd numbers to "animal".
func newMessage(num int) messenger.Message {
	if num%2 == 0 {
		return messenger.Message{
			Topic: "person",
			Data:  Person{ID: num, Name: fmt.Sprintf("Person: %d", num)},
		}
	}
	return messenger.Message{
		Topic: "animal",
		Data:  Animal{ID: num, Name: fmt.Sprintf("Animal: %d", num), Type: "dog"},
	}
}
