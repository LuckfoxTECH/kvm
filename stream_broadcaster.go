package kvm

import (
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
)

type VideoFrame struct {
	data []byte
	refs atomic.Int32
	pool *sync.Pool
}

func (f *VideoFrame) Data() []byte {
	return f.data
}

func (f *VideoFrame) Release() {
	if f.refs.Add(-1) == 0 {
		f.data = f.data[:cap(f.data)]
		f.pool.Put(f.data)
	}
}

var framePool = sync.Pool{
	New: func() interface{} {
		return make([]byte, maxFrameSize)
	},
}

type VideoBroadcaster struct {
	subscribers       map[string]chan *VideoFrame
	subscriberList    []chan *VideoFrame // cached flat slice, rebuilt on Subscribe/Unsubscribe
	count             atomic.Int32       // len(subscribers) as atomic for fast Broadcast check
	lock              sync.RWMutex
	onFirstSubscribe  func()
	onLastUnsubscribe func()
}

var videoBroadcaster = &VideoBroadcaster{
	subscribers: make(map[string]chan *VideoFrame),
}

func (b *VideoBroadcaster) rebuildList() {
	list := make([]chan *VideoFrame, 0, len(b.subscribers))
	for _, ch := range b.subscribers {
		list = append(list, ch)
	}
	b.subscriberList = list
}

func (b *VideoBroadcaster) Subscribe() (string, chan *VideoFrame) {
	b.lock.Lock()
	defer b.lock.Unlock()
	id := uuid.New().String()
	ch := make(chan *VideoFrame, 200)
	wasEmpty := len(b.subscribers) == 0
	b.subscribers[id] = ch
	b.rebuildList()
	b.count.Store(int32(len(b.subscribers)))
	if wasEmpty && b.onFirstSubscribe != nil {
		b.onFirstSubscribe()
	}
	return id, ch
}

func (b *VideoBroadcaster) Unsubscribe(id string) {
	b.lock.Lock()
	defer b.lock.Unlock()
	if ch, ok := b.subscribers[id]; ok {
		close(ch)
		delete(b.subscribers, id)
		b.rebuildList()
		b.count.Store(int32(len(b.subscribers)))
		if len(b.subscribers) == 0 && b.onLastUnsubscribe != nil {
			b.onLastUnsubscribe()
		}
	}
}

// SubscriberCount returns the number of active video stream subscribers.
func (b *VideoBroadcaster) SubscriberCount() int {
	return int(b.count.Load())
}

func (b *VideoBroadcaster) Broadcast(data []byte) {
	// atomic check avoids acquiring RLock on every video frame when no HTTP clients are connected
	if b.count.Load() == 0 {
		return
	}

	b.lock.RLock()
	subscribers := b.subscriberList
	subscriberCount := len(subscribers)
	if subscriberCount == 0 {
		b.lock.RUnlock()
		return
	}

	buf := framePool.Get().([]byte)
	if cap(buf) < len(data) {
		buf = make([]byte, len(data))
	}
	n := copy(buf, data)

	frame := &VideoFrame{
		data: buf[:n],
		pool: &framePool,
	}
	frame.refs.Store(int32(subscriberCount + 1))

	for _, ch := range subscribers {
		select {
		case ch <- frame:
		default:
			frame.Release()
		}
	}
	b.lock.RUnlock()
	frame.Release()
}
