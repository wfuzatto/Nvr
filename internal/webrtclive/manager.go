package webrtclive

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/wfuzatto/Nvr/internal/framebroker"
)

var (
	ErrUnsupportedCodec = errors.New("WebRTC low-latency currently requires H264")
	ErrDisabled         = errors.New("WebRTC is disabled")
)

type Config struct {
	Enabled  bool
	UDPPort  uint16
	PublicIP string
}

type Answer struct {
	SessionID string `json:"session_id"`
	Type      string `json:"type"`
	SDP       string `json:"sdp"`
}

type Stats struct {
	Enabled        bool `json:"enabled"`
	ActiveHubs     int  `json:"active_hubs"`
	ActiveSessions int  `json:"active_sessions"`
	UDPPort        int  `json:"udp_port"`
	PublicIPSet    bool `json:"public_ip_set"`
}

type Manager struct {
	ctx    context.Context
	broker *framebroker.Broker
	cfg    Config
	api    *webrtc.API
	udpMux ice.UDPMux

	mu       sync.Mutex
	hubs     map[string]*cameraHub
	sessions map[string]*session
}

type cameraHub struct {
	cameraID string
	track    *webrtc.TrackLocalStaticSample
	sub      framebroker.Subscription
	cancel   context.CancelFunc

	mu         sync.Mutex
	peers      int
	lastAccess time.Time
}

type session struct {
	id       string
	cameraID string
	pc       *webrtc.PeerConnection
	created  time.Time
	once     sync.Once
}

func New(ctx context.Context, broker *framebroker.Broker, cfg Config) (*Manager, error) {
	if ctx == nil { ctx = context.Background() }
	if broker == nil { return nil, errors.New("frame broker is required") }

	var setting webrtc.SettingEngine
	var udpMux ice.UDPMux
	if cfg.Enabled {
		if cfg.UDPPort == 0 {
			return nil, errors.New("invalid WebRTC UDP port")
		}
		mux, err := ice.NewMultiUDPMuxFromPort(
			int(cfg.UDPPort),
			ice.UDPMuxFromPortWithNetworks(ice.NetworkTypeUDP4),
		)
		if err != nil {
			return nil, fmt.Errorf("listen WebRTC UDP %d: %w", cfg.UDPPort, err)
		}
		udpMux = mux
		setting.SetICEUDPMux(udpMux)
		if publicIP:=strings.TrimSpace(cfg.PublicIP); publicIP!="" {
			if net.ParseIP(publicIP)==nil {
				_ = udpMux.Close()
				return nil, fmt.Errorf("invalid WebRTC public IP %q", publicIP)
			}
			setting.SetNAT1To1IPs([]string{publicIP}, webrtc.ICECandidateTypeHost)
		}
	}

	m := &Manager{
		ctx:ctx, broker:broker, cfg:cfg,
		api:webrtc.NewAPI(webrtc.WithSettingEngine(setting)),
		udpMux:udpMux,
		hubs:make(map[string]*cameraHub),
		sessions:make(map[string]*session),
	}
	go m.cleanupLoop()
	return m,nil
}

func (m *Manager) StartSession(ctx context.Context, cameraID, codec, offerSDP string) (Answer,error) {
	if !m.cfg.Enabled { return Answer{},ErrDisabled }
	if strings.TrimSpace(cameraID)=="" { return Answer{},errors.New("camera ID is required") }
	if strings.ToUpper(strings.TrimSpace(codec))!="H264" {
		return Answer{},ErrUnsupportedCodec
	}
	if strings.TrimSpace(offerSDP)=="" { return Answer{},errors.New("SDP offer is required") }

	hub,err:=m.ensureHub(cameraID)
	if err!=nil { return Answer{},err }

	pc,err:=m.api.NewPeerConnection(webrtc.Configuration{})
	if err!=nil { return Answer{},fmt.Errorf("create PeerConnection: %w",err) }

	sender,err:=pc.AddTrack(hub.track)
	if err!=nil {
		_ = pc.Close()
		return Answer{},fmt.Errorf("add H264 track: %w",err)
	}
	go drainRTCP(sender)

	id:=randomID()
	s:=&session{id:id,cameraID:cameraID,pc:pc,created:time.Now().UTC()}

	m.mu.Lock()
	m.sessions[id]=s
	m.mu.Unlock()
	hub.addPeer()

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			m.CloseSession(id)
		}
	})

	offer:=webrtc.SessionDescription{Type:webrtc.SDPTypeOffer,SDP:offerSDP}
	if err:=pc.SetRemoteDescription(offer); err!=nil {
		m.CloseSession(id)
		return Answer{},fmt.Errorf("set remote offer: %w",err)
	}
	gather:=webrtc.GatheringCompletePromise(pc)
	answer,err:=pc.CreateAnswer(nil)
	if err!=nil {
		m.CloseSession(id)
		return Answer{},fmt.Errorf("create answer: %w",err)
	}
	if err:=pc.SetLocalDescription(answer); err!=nil {
		m.CloseSession(id)
		return Answer{},fmt.Errorf("set local answer: %w",err)
	}

	select {
	case <-ctx.Done():
		m.CloseSession(id)
		return Answer{},ctx.Err()
	case <-gather:
	}

	local:=pc.LocalDescription()
	if local==nil {
		m.CloseSession(id)
		return Answer{},errors.New("WebRTC local description unavailable")
	}

	return Answer{SessionID:id,Type:"answer",SDP:local.SDP},nil
}

func (m *Manager) CloseSession(id string) {
	m.mu.Lock()
	s:=m.sessions[id]
	if s!=nil { delete(m.sessions,id) }
	h:=(*cameraHub)(nil)
	if s!=nil { h=m.hubs[s.cameraID] }
	m.mu.Unlock()
	if s==nil { return }

	s.once.Do(func(){ _=s.pc.Close() })
	if h!=nil { h.removePeer() }
}

func (m *Manager) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Stats{
		Enabled:m.cfg.Enabled,
		ActiveHubs:len(m.hubs),
		ActiveSessions:len(m.sessions),
		UDPPort:int(m.cfg.UDPPort),
		PublicIPSet:strings.TrimSpace(m.cfg.PublicIP)!="",
	}
}

func (m *Manager) ensureHub(cameraID string) (*cameraHub,error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h:=m.hubs[cameraID]; h!=nil {
		h.touch()
		return h,nil
	}

	track,err:=webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:webrtc.MimeTypeH264,
			ClockRate:90000,
			SDPFmtpLine:"level-asymmetry-allowed=1;packetization-mode=1",
		},
		"video",
		cameraID,
	)
	if err!=nil { return nil,err }

	ctx,cancel:=context.WithCancel(m.ctx)
	h:=&cameraHub{
		cameraID:cameraID,track:track,
		sub:m.broker.Subscribe(cameraID,128),
		cancel:cancel,lastAccess:time.Now(),
	}
	m.hubs[cameraID]=h
	go m.runHub(ctx,h)
	return h,nil
}

func (m *Manager) runHub(ctx context.Context,h *cameraHub) {
	defer h.sub.Cancel()
	var prev *framebroker.EncodedFrame
	for {
		select {
		case <-ctx.Done():
			return
		case frame,ok:=<-h.sub.C:
			if !ok { return }
			if strings.ToUpper(frame.Codec)!="H264" {
				prev=nil
				continue
			}
			if prev==nil {
				if !frame.Keyframe { continue }
				copyFrame:=frame
				prev=&copyFrame
				continue
			}

			duration:=sampleDuration(*prev,frame)
			payload:=samplePayload(*prev)
			if len(payload)>0 {
				_ = h.track.WriteSample(media.Sample{Data:payload,Duration:duration})
			}
			copyFrame:=frame
			prev=&copyFrame
		}
	}
}

func sampleDuration(current,next framebroker.EncodedFrame) time.Duration {
	clock:=current.ClockRate
	if clock<=0 { clock=90000 }
	delta:=uint32(next.Timestamp-current.Timestamp)
	if delta==0 { return 40*time.Millisecond }
	seconds:=float64(delta)/float64(clock)
	if seconds<=0 || seconds>2 { return 40*time.Millisecond }
	d:=time.Duration(seconds*float64(time.Second))
	if d<time.Millisecond { return time.Millisecond }
	return d
}

func samplePayload(frame framebroker.EncodedFrame) []byte {
	if len(frame.Data)==0 { return nil }
	if !frame.Keyframe || len(frame.Bootstrap)==0 {
		return frame.Data
	}
	extra:=0
	for _,nal:=range frame.Bootstrap { if len(nal)>0 { extra+=4+len(nal) } }
	out:=make([]byte,0,extra+len(frame.Data))
	for _,nal:=range frame.Bootstrap {
		if len(nal)==0 { continue }
		out=append(out,0,0,0,1)
		out=append(out,nal...)
	}
	out=append(out,frame.Data...)
	return out
}

func (m *Manager) cleanupLoop() {
	ticker:=time.NewTicker(15*time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			m.mu.Lock()
			sessions:=make([]*session,0,len(m.sessions))
			for _,s:=range m.sessions { sessions=append(sessions,s) }
			hubs:=make([]*cameraHub,0,len(m.hubs))
			for _,h:=range m.hubs { hubs=append(hubs,h) }
			m.sessions=make(map[string]*session)
			m.hubs=make(map[string]*cameraHub)
			m.mu.Unlock()
			for _,s:=range sessions { s.once.Do(func(){ _=s.pc.Close() }) }
			for _,h:=range hubs { h.cancel() }
			if m.udpMux!=nil { _=m.udpMux.Close() }
			return
		case now:=<-ticker.C:
			m.mu.Lock()
			for id,h:=range m.hubs {
				h.mu.Lock()
				idle:=h.peers==0 && now.Sub(h.lastAccess)>30*time.Second
				h.mu.Unlock()
				if idle {
					h.cancel()
					delete(m.hubs,id)
				}
			}
			m.mu.Unlock()
		}
	}
}

func (h *cameraHub) addPeer() {
	h.mu.Lock()
	h.peers++
	h.lastAccess=time.Now()
	h.mu.Unlock()
}
func (h *cameraHub) removePeer() {
	h.mu.Lock()
	if h.peers>0 { h.peers-- }
	h.lastAccess=time.Now()
	h.mu.Unlock()
}
func (h *cameraHub) touch() {
	h.mu.Lock()
	h.lastAccess=time.Now()
	h.mu.Unlock()
}

func drainRTCP(sender *webrtc.RTPSender) {
	buf:=make([]byte,1500)
	for {
		if _,_,err:=sender.Read(buf); err!=nil {
			if !errors.Is(err,io.EOF) { return }
			return
		}
	}
}

func randomID() string {
	var b [16]byte
	if _,err:=rand.Read(b[:]); err!=nil {
		return fmt.Sprintf("%d",time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
