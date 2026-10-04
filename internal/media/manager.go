package media

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/wfuzatto/Nvr/internal/config"
	"github.com/wfuzatto/Nvr/internal/framebroker"
	"github.com/wfuzatto/Nvr/internal/model"
	"github.com/wfuzatto/Nvr/internal/rtsp"
	"github.com/wfuzatto/Nvr/internal/security"
	"github.com/wfuzatto/Nvr/internal/store"
)

type CameraStatus struct {
	CameraID        string    `json:"camera_id"`
	State           string    `json:"state"`
	Codec           string    `json:"codec,omitempty"`
	ConnectedAt     time.Time `json:"connected_at,omitempty"`
	LastPacketAt    time.Time `json:"last_packet_at,omitempty"`
	LastFrameAt     time.Time `json:"last_frame_at,omitempty"`
	LastSegmentAt   time.Time `json:"last_segment_at,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
	Reconnects      uint64    `json:"reconnects"`
	FramesPublished uint64    `json:"frames_published"`
	SegmentsWritten uint64    `json:"segments_written"`
	BytesWritten    int64     `json:"bytes_written"`
	ActiveSegment   *Segment  `json:"active_segment,omitempty"`
}

type workerHandle struct {
	cancel    context.CancelFunc
	updatedAt time.Time
}

type Manager struct {
	cfg     config.Config
	cameras store.CameraStore
	box     *security.SecretBox
	broker  *framebroker.Broker

	mu       sync.RWMutex
	workers  map[string]workerHandle
	statuses map[string]CameraStatus
}

func NewManager(cfg config.Config, cameras store.CameraStore, box *security.SecretBox, broker *framebroker.Broker) *Manager {
	if broker == nil { broker = framebroker.New() }
	return &Manager{
		cfg: cfg, cameras: cameras, box: box, broker: broker,
		workers: make(map[string]workerHandle),
		statuses: make(map[string]CameraStatus),
	}
}

func (m *Manager) Start(ctx context.Context) {
	_, _ = RunRetention(m.cfg.StorageDir, m.cfg.RetentionDays, m.cfg.StorageMaxBytes)
	m.syncWorkers(ctx)

	go func() {
		ticker := time.NewTicker(m.cfg.SupervisorInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				m.stopAll()
				return
			case <-ticker.C:
				m.syncWorkers(ctx)
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(m.cfg.RetentionInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = RunRetention(m.cfg.StorageDir, m.cfg.RetentionDays, m.cfg.StorageMaxBytes)
			}
		}
	}()
}

func (m *Manager) Status(cameraID string) (CameraStatus, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	status, ok := m.statuses[cameraID]
	return status, ok
}

func (m *Manager) Statuses() []CameraStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]CameraStatus, 0, len(m.statuses))
	for _, status := range m.statuses { out = append(out, status) }
	sort.Slice(out, func(i, j int) bool { return out[i].CameraID < out[j].CameraID })
	return out
}

func (m *Manager) Timeline(cameraID string, from, to time.Time, limit int) ([]Segment, error) {
	return ListSegments(m.cfg.StorageDir, cameraID, from, to, limit)
}

func (m *Manager) RecentSegments(cameraID string) ([]Segment, error) {
	now := time.Now().UTC()
	items, err := ListSegments(m.cfg.StorageDir, cameraID, now.Add(-m.cfg.PreEventWindow), now, 500)
	if err != nil { return nil, err }
	if status, ok := m.Status(cameraID); ok && status.ActiveSegment != nil {
		active := *status.ActiveSegment
		if active.End.IsZero() { active.End = now }
		if !active.End.Before(now.Add(-m.cfg.PreEventWindow)) {
			items = append(items, active)
		}
	}
	return items, nil
}

func (m *Manager) RunRetentionNow() (RetentionReport, error) {
	return RunRetention(m.cfg.StorageDir, m.cfg.RetentionDays, m.cfg.StorageMaxBytes)
}

func (m *Manager) Protect(relativePath string, protect bool) error {
	return ProtectSegment(m.cfg.StorageDir, relativePath, protect)
}

func (m *Manager) BrokerStats() map[string]uint64 { return m.broker.Stats() }

func (m *Manager) syncWorkers(parent context.Context) {
	cameras := m.cameras.List()
	desired := make(map[string]model.Camera)
	present := make(map[string]bool)
	for _, camera := range cameras {
		present[camera.ID] = true
		if camera.Enabled { desired[camera.ID] = camera }
	}

	m.mu.Lock()
	for id, handle := range m.workers {
		camera, wanted := desired[id]
		if wanted && camera.UpdatedAt.Equal(handle.updatedAt) { continue }
		handle.cancel()
		delete(m.workers, id)
		if !wanted {
			status := m.statuses[id]
			status.CameraID = id
			status.State = "stopped"
			m.statuses[id] = status
		}
	}
	for id := range m.statuses {
		if !present[id] { delete(m.statuses, id) }
	}
	for id, camera := range desired {
		if _, exists := m.workers[id]; exists { continue }
		ctx, cancel := context.WithCancel(parent)
		m.workers[id] = workerHandle{cancel: cancel, updatedAt: camera.UpdatedAt}
		status := m.statuses[id]
		status.CameraID = id
		status.State = "starting"
		status.LastError = ""
		m.statuses[id] = status
		go m.runCamera(ctx, camera)
	}
	m.mu.Unlock()
}

func (m *Manager) stopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, handle := range m.workers {
		handle.cancel()
		delete(m.workers, id)
		status := m.statuses[id]
		status.State = "stopped"
		m.statuses[id] = status
	}
}

func (m *Manager) runCamera(ctx context.Context, camera model.Camera) {
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			m.updateStatus(camera.ID, func(s *CameraStatus) { s.State = "stopped" })
			return
		default:
		}

		rawURL, err := m.box.Decrypt(camera.RTSPURLCipher)
		if err != nil {
			m.setFailure(camera.ID, "decrypt RTSP URL: "+err.Error())
			return
		}

		sessionStarted := time.Now()
		err = m.recordSession(ctx, camera.ID, rawURL)
		if time.Since(sessionStarted) >= 30*time.Second { backoff = time.Second }
		if ctx.Err() != nil {
			m.updateStatus(camera.ID, func(s *CameraStatus) { s.State = "stopped" })
			return
		}

		if err != nil { m.setFailure(camera.ID, err.Error()) }
		m.updateStatus(camera.ID, func(s *CameraStatus) {
			s.State = "reconnecting"
			s.Reconnects++
		})

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			m.updateStatus(camera.ID, func(s *CameraStatus) { s.State = "stopped" })
			return
		case <-timer.C:
		}
		if backoff < 30*time.Second { backoff *= 2 }
		if backoff > 30*time.Second { backoff = 30*time.Second }
	}
}

func (m *Manager) recordSession(ctx context.Context, cameraID, rawURL string) error {
	session, err := rtsp.OpenSession(ctx, rawURL, m.cfg.RTSPReadTimeout)
	if err != nil { return err }
	track := session.Track()

	depacketizer, err := rtsp.NewDepacketizer(track.Codec, track.PayloadType)
	if err != nil { _ = session.Close(); return err }
	recorder, err := NewRecorder(m.cfg.StorageDir, cameraID, track.Codec, m.cfg.SegmentDuration, track.Bootstrap)
	if err != nil { _ = session.Close(); return err }

	sessionDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
		case <-sessionDone:
		}
	}()
	defer close(sessionDone)
	defer session.Close()

	m.updateStatus(cameraID, func(s *CameraStatus) {
		s.State = "recording"
		s.Codec = track.Codec
		s.ConnectedAt = time.Now().UTC()
		s.LastError = ""
	})

	for {
		packet, err := session.ReadRTP()
		if err != nil {
			if completed, closeErr := recorder.Close(); closeErr == nil && completed != nil {
				m.noteSegment(cameraID, completed)
			}
			m.updateStatus(cameraID, func(s *CameraStatus) { s.ActiveSegment = nil })
			if ctx.Err() != nil { return ctx.Err() }
			return err
		}
		now := time.Now().UTC()
		m.updateStatus(cameraID, func(s *CameraStatus) { s.LastPacketAt = now })

		au, err := depacketizer.Push(packet)
		if err != nil {
			m.updateStatus(cameraID, func(s *CameraStatus) { s.LastError = "depacketize: "+err.Error() })
			continue
		}
		if au == nil { continue }

		m.broker.Publish(framebroker.EncodedFrame{
			CameraID: cameraID, Codec: au.Codec, Timestamp: au.Timestamp,
			Keyframe: au.Keyframe, Received: au.ReceivedAt, Data: au.Data,
		})
		m.updateStatus(cameraID, func(s *CameraStatus) {
			s.LastFrameAt = au.ReceivedAt
			s.FramesPublished++
		})

		completed, err := recorder.Write(*au)
		if err != nil {
			_, _ = recorder.Close()
			m.updateStatus(cameraID, func(s *CameraStatus) { s.ActiveSegment = nil })
			return err
		}
		active := recorder.Current()
		m.updateStatus(cameraID, func(s *CameraStatus) { s.ActiveSegment = active })
		if completed != nil { m.noteSegment(cameraID, completed) }
	}
}

func (m *Manager) noteSegment(cameraID string, segment *Segment) {
	if segment == nil { return }
	m.updateStatus(cameraID, func(s *CameraStatus) {
		s.LastSegmentAt = segment.End
		s.SegmentsWritten++
		s.BytesWritten += segment.Bytes
	})
}

func (m *Manager) setFailure(cameraID, message string) {
	m.updateStatus(cameraID, func(s *CameraStatus) {
		s.State = "error"
		s.LastError = message
	})
}

func (m *Manager) updateStatus(cameraID string, mutate func(*CameraStatus)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := m.statuses[cameraID]
	status.CameraID = cameraID
	mutate(&status)
	m.statuses[cameraID] = status
}

