package eventbus

import (
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog/log"
)

// natsLaneBuffer is how many deliveries each lane may hold, running or queued, before fetching pauses.
const natsLaneBuffer = 32

// natsLanes runs a keyed subscription: each key's messages in order, up to
// lanes keys at once, so a slow key holds one lane and never another key.
// JetStream times a delivery from when it was handed out, so every message
// held, queued or running, is kept alive with an in-progress heartbeat.
type natsLanes struct {
	slots    chan struct{}
	room     chan struct{}
	deliver  func(jetstream.Msg)
	halted   chan struct{}
	haltOnce sync.Once
	done     chan struct{}
	wg       sync.WaitGroup

	mu   sync.Mutex
	held map[jetstream.Msg]struct{}
	keys map[string][]jetstream.Msg
}

func newNATSLanes(lanes int, ackWait time.Duration, deliver func(jetstream.Msg)) *natsLanes {
	p := &natsLanes{
		slots:   make(chan struct{}, lanes),
		room:    make(chan struct{}, lanes*natsLaneBuffer),
		deliver: deliver,
		halted:  make(chan struct{}),
		done:    make(chan struct{}),
		held:    map[jetstream.Msg]struct{}{},
		keys:    map[string][]jetstream.Msg{},
	}
	go p.heartbeat(max(ackWait/3, 100*time.Millisecond))
	return p
}

// dispatch queues m behind every earlier message with the same key, waiting
// for room when the lanes already hold their share.
func (p *natsLanes) dispatch(m jetstream.Msg, key string) {
	// Held first, so the heartbeat keeps it alive while it waits for room.
	p.hold(m)
	select {
	case p.room <- struct{}{}:
	case <-p.halted:
		p.unhold(m)
		nak(m)
		return
	}
	if p.isHalted() {
		p.abandon(m)
		return
	}
	p.mu.Lock()
	if queue, running := p.keys[key]; running {
		p.keys[key] = append(queue, m)
		p.mu.Unlock()
		return
	}
	p.keys[key] = nil
	p.mu.Unlock()
	p.wg.Add(1)
	go p.run(key, m)
}

// run handles key's messages one after another, taking a lane for each.
func (p *natsLanes) run(key string, m jetstream.Msg) {
	defer p.wg.Done()
	for {
		acquired := false
		select {
		case p.slots <- struct{}{}:
			acquired = true
		case <-p.halted:
		}
		if p.isHalted() {
			if acquired {
				<-p.slots
			}
			p.abandonKey(key, m)
			return
		}
		p.deliver(m)
		<-p.slots
		p.release(m)

		p.mu.Lock()
		queue := p.keys[key]
		if len(queue) == 0 {
			delete(p.keys, key)
			p.mu.Unlock()
			return
		}
		m, p.keys[key] = queue[0], queue[1:]
		p.mu.Unlock()
	}
}

func (p *natsLanes) hold(m jetstream.Msg) {
	p.mu.Lock()
	p.held[m] = struct{}{}
	p.mu.Unlock()
}

func (p *natsLanes) release(m jetstream.Msg) {
	p.unhold(m)
	<-p.room
}

func (p *natsLanes) unhold(m jetstream.Msg) {
	p.mu.Lock()
	delete(p.held, m)
	p.mu.Unlock()
}

// abandon hands back a message that will not be handled here, so it is
// redelivered without waiting out its ack wait.
func (p *natsLanes) abandon(m jetstream.Msg) {
	p.release(m)
	nak(m)
}

// abandonKey hands back m and everything queued behind it.
func (p *natsLanes) abandonKey(key string, m jetstream.Msg) {
	p.mu.Lock()
	queue := p.keys[key]
	delete(p.keys, key)
	p.mu.Unlock()
	p.abandon(m)
	for _, q := range queue {
		p.abandon(q)
	}
}

func nak(m jetstream.Msg) {
	if err := m.Nak(); err != nil {
		log.Warn().Err(err).Msg("eventbus nats nak failed")
	}
}

func (p *natsLanes) heartbeat(every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
		}
		p.mu.Lock()
		msgs := make([]jetstream.Msg, 0, len(p.held))
		for m := range p.held {
			msgs = append(msgs, m)
		}
		p.mu.Unlock()
		for _, m := range msgs {
			// A message settled since the snapshot refuses this, which is fine.
			if err := m.InProgress(); err != nil {
				log.Debug().Err(err).Msg("eventbus nats in-progress heartbeat failed")
			}
		}
	}
}

func (p *natsLanes) isHalted() bool {
	select {
	case <-p.halted:
		return true
	default:
		return false
	}
}

// halt stops handling anything not already running; queued messages are handed back.
func (p *natsLanes) halt() {
	p.haltOnce.Do(func() { close(p.halted) })
}

// stop waits for the running handlers once no more messages can be dispatched.
func (p *natsLanes) stop() {
	p.wg.Wait()
	close(p.done)
}
