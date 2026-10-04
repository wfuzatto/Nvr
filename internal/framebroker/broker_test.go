package framebroker

import "testing"

func TestBrokerDoesNotBlockOnSlowSubscriber(t *testing.T) {
	b := New()
	sub := b.Subscribe("cam-1", 1)
	defer sub.Cancel()

	b.Publish(EncodedFrame{CameraID: "cam-1", Data: []byte{1}})
	b.Publish(EncodedFrame{CameraID: "cam-1", Data: []byte{2}})

	stats := b.Stats()
	if stats["published"] != 2 { t.Fatalf("published=%d", stats["published"]) }
	if stats["dropped"] != 1 { t.Fatalf("dropped=%d", stats["dropped"]) }
}
