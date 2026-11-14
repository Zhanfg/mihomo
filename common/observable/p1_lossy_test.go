package observable

import (
	"testing"
	"time"
)

func TestP1LossyObservableDoesNotBlockOnSlowSubscriber(t *testing.T) {
	input := make(chan int)
	stream := NewLossyObservable[int](input)
	sub, err := stream.Subscribe()
	if err != nil { t.Fatal(err) }
	defer stream.UnSubscribe(sub)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ { input <- i }
		close(input)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("observable producer blocked on slow UI reader")
	}
}
