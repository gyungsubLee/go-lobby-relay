package protocol

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

func FuzzDecodeClient(f *testing.F) {
	hello, _ := fittedHello(f, MinHelloBytes)
	for _, envelope := range []proto.Message{
		hello,
		validAuth(),
		validClientData(),
		validPing(),
	} {
		f.Add(mustMarshal(f, envelope))
	}
	f.Add([]byte{})
	f.Add([]byte{0x80})
	f.Add(make([]byte, MaxDatagramBytes+1))

	f.Fuzz(func(t *testing.T, datagram []byte) {
		envelope, err := DecodeClient(datagram)
		if err == nil {
			if envelope == nil {
				t.Fatal("DecodeClient returned a nil envelope without an error")
			}
			if len(datagram) > MaxDatagramBytes {
				t.Fatalf("accepted %d-byte datagram", len(datagram))
			}
			return
		}

		switch ReasonOf(err) {
		case ReasonMalformed, ReasonOversized, ReasonUnsupportedVersion:
		default:
			t.Fatalf("DecodeClient returned unclassified error: %v", err)
		}
	})
}
