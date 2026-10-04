package webrtclive

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/wfuzatto/Nvr/internal/framebroker"
)

func TestSampleDuration(t *testing.T) {
	a:=framebroker.EncodedFrame{ClockRate:90000,Timestamp:1000}
	b:=framebroker.EncodedFrame{ClockRate:90000,Timestamp:4600}
	if got:=sampleDuration(a,b); got!=40*time.Millisecond {
		t.Fatalf("duration=%s",got)
	}
}

func TestSamplePayloadPrependsBootstrapOnKeyframe(t *testing.T) {
	frame:=framebroker.EncodedFrame{
		Keyframe:true,
		Bootstrap:[][]byte{{0x67,1},{0x68,2}},
		Data:[]byte{0,0,0,1,0x65,3},
	}
	got:=samplePayload(frame)
	if len(got)<=len(frame.Data) { t.Fatalf("payload not expanded: %x",got) }
	if !strings.Contains(string(got),string([]byte{0x67,1})) { t.Fatalf("SPS missing: %x",got) }
}

func TestWebRTCSessionDeliversH264RTP(t *testing.T) {
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
	defer cancel()

	broker:=framebroker.New()
	manager,err:=New(ctx,broker,Config{Enabled:true,UDPMin:51000,UDPMax:51100})
	if err!=nil { t.Fatal(err) }

	client,err:=webrtc.NewPeerConnection(webrtc.Configuration{})
	if err!=nil { t.Fatal(err) }
	defer client.Close()

	if _,err:=client.AddTransceiverFromKind(
		webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction:webrtc.RTPTransceiverDirectionRecvonly},
	); err!=nil { t.Fatal(err) }

	rtpReceived:=make(chan struct{},1)
	client.OnTrack(func(track *webrtc.TrackRemote,_ *webrtc.RTPReceiver) {
		go func() {
			if _,_,readErr:=track.ReadRTP(); readErr==nil {
				select { case rtpReceived<-struct{}{}: default: }
			}
		}()
	})

	offer,err:=client.CreateOffer(nil)
	if err!=nil { t.Fatal(err) }
	gather:=webrtc.GatheringCompletePromise(client)
	if err:=client.SetLocalDescription(offer); err!=nil { t.Fatal(err) }
	select {
	case <-ctx.Done(): t.Fatal("client ICE gather timeout")
	case <-gather:
	}

	answer,err:=manager.StartSession(ctx,"cam-1","H264",client.LocalDescription().SDP)
	if err!=nil { t.Fatal(err) }
	defer manager.CloseSession(answer.SessionID)
	if answer.Type!="answer" || answer.SDP=="" { t.Fatalf("invalid answer: %+v",answer) }

	if err:=client.SetRemoteDescription(webrtc.SessionDescription{Type:webrtc.SDPTypeAnswer,SDP:answer.SDP}); err!=nil {
		t.Fatal(err)
	}

	connected:=make(chan struct{},1)
	client.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state==webrtc.PeerConnectionStateConnected {
			select { case connected<-struct{}{}: default: }
		}
	})
	select {
	case <-ctx.Done(): t.Fatal("WebRTC connect timeout")
	case <-connected:
	}

	now:=time.Now().UTC()
	bootstrap:=[][]byte{{0x67,0x42,0x00,0x1f},{0x68,0xce,0x06,0xe2}}
	for i:=0;i<5;i++ {
		base:=uint32(1000+i*7200)
		broker.Publish(framebroker.EncodedFrame{
			CameraID:"cam-1",Codec:"H264",ClockRate:90000,
			Timestamp:base,Keyframe:i==0,Received:now.Add(time.Duration(i)*80*time.Millisecond),
			Bootstrap:bootstrap,Data:[]byte{0,0,0,1,0x65,0x88,0x84},
		})
	}

	select {
	case <-ctx.Done(): t.Fatal("no RTP received over DTLS-SRTP")
	case <-rtpReceived:
	}
}

func TestWebRTCRejectsH265WithoutTranscoding(t *testing.T) {
	ctx,cancel:=context.WithCancel(context.Background())
	defer cancel()
	manager,err:=New(ctx,framebroker.New(),Config{Enabled:true,UDPMin:52000,UDPMax:52100})
	if err!=nil { t.Fatal(err) }
	_,err=manager.StartSession(ctx,"cam","H265","v=0")
	if !errors.Is(err,ErrUnsupportedCodec) {
		t.Fatalf("err=%v",err)
	}
}
