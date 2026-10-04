package live

import (
	"bytes"
	"context"
	"errors"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/wfuzatto/Nvr/internal/framebroker"
	"github.com/wfuzatto/Nvr/internal/playback"
)

type Segment struct {
	Sequence        uint64    `json:"sequence"`
	ProgramDateTime time.Time `json:"program_date_time"`
	Duration        float64   `json:"duration"`
	Data            []byte    `json:"-"`
}

type Manager struct {
	ctx         context.Context
	broker      *framebroker.Broker
	target      time.Duration
	idle        time.Duration
	keepSegments int

	mu   sync.Mutex
	hubs map[string]*cameraHub
}

type cameraHub struct {
	cameraID string
	sub      framebroker.Subscription
	cancel   context.CancelFunc

	mu         sync.RWMutex
	segments   []Segment
	nextSeq    uint64
	lastAccess time.Time
	updated    chan struct{}
}

func NewManager(ctx context.Context, broker *framebroker.Broker) *Manager {
	if ctx==nil { ctx=context.Background() }
	m:=&Manager{
		ctx:ctx, broker:broker, target:2*time.Second,
		idle:90*time.Second, keepSegments:8,
		hubs:make(map[string]*cameraHub),
	}
	go m.cleanupLoop()
	return m
}

func (m *Manager) Playlist(ctx context.Context, cameraID string) ([]Segment,error) {
	if cameraID=="" { return nil,errors.New("camera ID is required") }
	h:=m.ensureHub(cameraID)
	h.touch()

	for {
		segments:=h.snapshot()
		if len(segments)>0 { return segments,nil }
		select {
		case <-ctx.Done():
			return nil,ctx.Err()
		case <-h.updated:
		case <-time.After(5*time.Second):
			return nil,errors.New("live stream has not produced a segment yet")
		}
	}
}

func (m *Manager) Segment(cameraID string, sequence uint64) (Segment,bool) {
	m.mu.Lock()
	h:=m.hubs[cameraID]
	m.mu.Unlock()
	if h==nil { return Segment{},false }
	h.touch()
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _,segment:=range h.segments {
		if segment.Sequence==sequence {
			return segment,true
		}
	}
	return Segment{},false
}

func (m *Manager) ensureHub(cameraID string) *cameraHub {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h:=m.hubs[cameraID]; h!=nil { h.touch(); return h }

	ctx,cancel:=context.WithCancel(m.ctx)
	sub:=m.broker.Subscribe(cameraID,128)
	h:=&cameraHub{
		cameraID:cameraID,sub:sub,cancel:cancel,
		lastAccess:time.Now(),updated:make(chan struct{},1),
	}
	m.hubs[cameraID]=h
	go m.runHub(ctx,h)
	return h
}

func (m *Manager) runHub(ctx context.Context,h *cameraHub) {
	defer h.sub.Cancel()
	var frames []framebroker.EncodedFrame
	var start time.Time
	var bytesHeld int

	finalize:=func(boundary time.Time) {
		if len(frames)==0 { return }
		var out bytes.Buffer
		if err:=playback.MuxEncodedFramesTS(frames,&out); err!=nil {
			frames=nil
			bytesHeld=0
			start=time.Time{}
			return
		}
		duration:=boundary.Sub(start).Seconds()
		if duration<=0 {
			last:=frames[len(frames)-1].Received
			duration=last.Sub(start).Seconds()
		}
		if duration<=0 { duration=0.04 }

		h.mu.Lock()
		segment:=Segment{
			Sequence:h.nextSeq,ProgramDateTime:start.UTC(),
			Duration:duration,Data:out.Bytes(),
		}
		h.nextSeq++
		h.segments=append(h.segments,segment)
		if len(h.segments)>m.keepSegments {
			h.segments=append([]Segment(nil),h.segments[len(h.segments)-m.keepSegments:]...)
		}
		h.mu.Unlock()
		select { case h.updated<-struct{}{}: default: }
		frames=nil
		bytesHeld=0
		start=time.Time{}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case frame,ok:=<-h.sub.C:
			if !ok { return }
			when:=frame.Received
			if when.IsZero() { when=time.Now().UTC() }

			if len(frames)==0 {
				if !frame.Keyframe { continue }
				start=when
				frames=append(frames,frame)
				bytesHeld+=len(frame.Data)
				continue
			}

			if frame.Keyframe && when.Sub(start)>=m.target {
				finalize(when)
				start=when
				frames=append(frames,frame)
				bytesHeld=len(frame.Data)
				continue
			}

			frames=append(frames,frame)
			bytesHeld+=len(frame.Data)

			// A camera with a broken GOP must never consume unbounded memory.
			if bytesHeld>32<<20 || when.Sub(start)>15*time.Second {
				frames=nil
				bytesHeld=0
				start=time.Time{}
			}
		}
	}
}

func (m *Manager) cleanupLoop() {
	ticker:=time.NewTicker(30*time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			m.mu.Lock()
			for id,h:=range m.hubs {
				h.cancel()
				delete(m.hubs,id)
			}
			m.mu.Unlock()
			return
		case now:=<-ticker.C:
			m.mu.Lock()
			for id,h:=range m.hubs {
				h.mu.RLock()
				last:=h.lastAccess
				h.mu.RUnlock()
				if now.Sub(last)>m.idle {
					h.cancel()
					delete(m.hubs,id)
				}
			}
			m.mu.Unlock()
		}
	}
}

func (h *cameraHub) touch() {
	h.mu.Lock()
	h.lastAccess=time.Now()
	h.mu.Unlock()
}

func (h *cameraHub) snapshot() []Segment {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out:=make([]Segment,len(h.segments))
	for i,segment:=range h.segments {
		out[i]=segment
		out[i].Data=nil
	}
	sort.Slice(out,func(i,j int) bool { return out[i].Sequence<out[j].Sequence })
	return out
}

func TargetDuration(segments []Segment) int {
	maxDuration:=1.0
	for _,segment:=range segments {
		if segment.Duration>maxDuration { maxDuration=segment.Duration }
	}
	return int(math.Ceil(maxDuration))
}
