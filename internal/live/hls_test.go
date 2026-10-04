package live

import (
	"context"
	"testing"
	"time"

	"github.com/wfuzatto/Nvr/internal/framebroker"
)

func TestLiveHubCreatesSegmentOnKeyframeBoundary(t *testing.T) {
	ctx,cancel:=context.WithCancel(context.Background())
	defer cancel()
	broker:=framebroker.New()
	manager:=NewManager(ctx,broker)
	manager.target=100*time.Millisecond
	manager.idle=time.Minute

	result:=make(chan []Segment,1)
	go func() {
		waitCtx,waitCancel:=context.WithTimeout(ctx,2*time.Second)
		defer waitCancel()
		segments,_:=manager.Playlist(waitCtx,"cam-1")
		result<-segments
	}()

	time.Sleep(20*time.Millisecond)
	now:=time.Now().UTC()
	bootstrap:=[][]byte{{0x67,1},{0x68,2}}
	broker.Publish(framebroker.EncodedFrame{CameraID:"cam-1",Codec:"H264",ClockRate:90000,Timestamp:1000,Keyframe:true,Received:now,Bootstrap:bootstrap,Data:[]byte{0,0,0,1,0x65,1}})
	broker.Publish(framebroker.EncodedFrame{CameraID:"cam-1",Codec:"H264",ClockRate:90000,Timestamp:4600,Received:now.Add(40*time.Millisecond),Data:[]byte{0,0,0,1,0x41,2}})
	broker.Publish(framebroker.EncodedFrame{CameraID:"cam-1",Codec:"H264",ClockRate:90000,Timestamp:10000,Keyframe:true,Received:now.Add(120*time.Millisecond),Bootstrap:bootstrap,Data:[]byte{0,0,0,1,0x65,3}})

	select {
	case segments:=<-result:
		if len(segments)!=1 { t.Fatalf("segments=%d",len(segments)) }
		if segments[0].Duration<0.1 { t.Fatalf("duration=%f",segments[0].Duration) }
		if _,ok:=manager.Segment("cam-1",segments[0].Sequence); !ok { t.Fatal("segment not found") }
	case <-time.After(2*time.Second):
		t.Fatal("live segment timeout")
	}
}
