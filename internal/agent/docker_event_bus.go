package agent

import (
	"fmt"
	"github.com/docker/docker/api/types/events"
	"sync"
)

type dockerEventBus struct {
	mu          sync.Mutex
	next        uint64
	subscribers map[uint64]chan events.Message
}

func (b *dockerEventBus) Subscribe() (<-chan events.Message, func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subscribers == nil {
		b.subscribers = map[uint64]chan events.Message{}
	}
	if len(b.subscribers) >= 10 {
		return nil, nil, fmt.Errorf("subscriber limit")
	}
	b.next++
	id := b.next
	ch := make(chan events.Message, 8)
	b.subscribers[id] = ch
	var once sync.Once
	return ch, func() { once.Do(func() { b.mu.Lock(); delete(b.subscribers, id); b.mu.Unlock() }) }, nil
}
func (b *dockerEventBus) Publish(event events.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}
