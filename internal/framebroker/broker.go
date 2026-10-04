package framebroker

import (
	"sync"
	"sync/atomic"
	"time"
)

type EncodedFrame struct {
	CameraID  string    `json:"camera_id"`
	Codec     string    `json:"codec"`
	Timestamp uint32    `json:"timestamp"`
	Keyframe  bool      `json:"keyframe"`
	Received  time.Time `json:"received_at"`
	Data      []byte    `json:"-"`
}

type Subscription struct {
	ID       uint64
	CameraID string
	C        <-chan EncodedFrame
	Cancel   func()
}

type subscriber struct {
	cameraID string
	ch       chan EncodedFrame
}

type Broker struct {
	mu          sync.RWMutex
	nextID      atomic.Uint64
	subscribers map[uint64]*subscriber
	published   atomic.Uint64
	dropped     atomic.Uint64
}

func New() *Broker {
	return &Broker{subscribers: make(map[uint64]*subscriber)}
}

func (b *Broker) Subscribe(cameraID string, buffer int) Subscription {
	if buffer < 1 { buffer = 1 }
	id := b.nextID.Add(1)
	ch := make(chan EncodedFrame, buffer)
	b.mu.Lock()
	b.subscribers[id] = &subscriber{cameraID: cameraID, ch: ch}
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			if sub, ok := b.subscribers[id]; ok {
				delete(b.subscribers, id)
				close(sub.ch)
			}
			b.mu.Unlock()
		})
	}
	return Subscription{ID: id, CameraID: cameraID, C: ch, Cancel: cancel}
}

func (b *Broker) Publish(frame EncodedFrame) {
	b.published.Add(1)
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, sub := range b.subscribers {
		if sub.cameraID != "" && sub.cameraID != frame.CameraID { continue }
		copyFrame := frame
		copyFrame.Data = append([]byte(nil), frame.Data...)
		select {
		case sub.ch <- copyFrame:
		default:
			b.dropped.Add(1)
		}
	}
}

func (b *Broker) Stats() map[string]uint64 {
	return map[string]uint64{
		"published": b.published.Load(),
		"dropped": b.dropped.Load(),
	}
}
