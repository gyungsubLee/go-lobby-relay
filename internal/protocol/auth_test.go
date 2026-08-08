package protocol

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

type authGolden struct {
	Revision              uint32 `json:"revision"`
	RoomID                string `json:"room_id"`
	SessionID             string `json:"session_id"`
	GrantIDHex            string `json:"grant_id_hex"`
	GrantSecretHex        string `json:"grant_secret_hex"`
	CandidateIDHex        string `json:"candidate_id_hex"`
	ClientNonceHex        string `json:"client_nonce_hex"`
	ServerNonceHex        string `json:"server_nonce_hex"`
	BindingIDHex          string `json:"binding_id_hex"`
	ExpiryUnixMS          int64  `json:"expiry_unix_ms"`
	Sequence              uint64 `json:"sequence"`
	PayloadHex            string `json:"payload_hex"`
	AuthFrameHex          string `json:"auth_frame_hex"`
	AuthTagHex            string `json:"auth_tag_hex"`
	BindingFrameHex       string `json:"binding_frame_hex"`
	BindingKeyHex         string `json:"binding_key_hex"`
	BoundFrameHex         string `json:"bound_frame_hex"`
	BoundTagHex           string `json:"bound_tag_hex"`
	ClientDataFrameHex    string `json:"client_data_frame_hex"`
	ClientDataTagHex      string `json:"client_data_tag_hex"`
	PingFrameHex          string `json:"ping_frame_hex"`
	PingTagHex            string `json:"ping_tag_hex"`
	ClientDataEnvelopeHex string `json:"client_data_envelope_hex"`
	ServerDataEnvelopeHex string `json:"server_data_envelope_hex"`
}

// These assignments make fixed-width IDs, nonces, secrets, keys, and outputs
// part of the public API. Invalid 15/17-byte IDs and 31/33-byte secrets cannot
// be passed to these functions as byte slices.
var (
	_ func(Bytes32, uint32, string, string, Bytes16, Bytes16, Bytes16, Bytes32) Bytes32 = AuthTag
	_ func(Bytes32, uint32, string, string, Bytes16, Bytes16, Bytes16, Bytes32) Bytes32 = BindingKey
	_ func(Bytes32, uint32, string, string, Bytes16, Bytes16, int64) Bytes32            = BoundTag
	_ func(Bytes32, uint32, string, string, Bytes16, uint64, []byte) Bytes32            = ClientDataTag
	_ func(Bytes32, uint32, string, string, Bytes16, uint64) Bytes32                    = PingTag
	_ func(Bytes32, []byte) bool                                                        = EqualTag
)

func TestFixedByteWidths(t *testing.T) {
	if got := len(Bytes16{}); got != 16 {
		t.Fatalf("len(Bytes16) = %d, want 16", got)
	}
	if got := len(Bytes32{}); got != 32 {
		t.Fatalf("len(Bytes32) = %d, want 32", got)
	}
}

func TestGoldenFramesAndHMACs(t *testing.T) {
	golden := loadAuthGolden(t)
	grantID := goldenBytes16(t, golden.GrantIDHex)
	candidateID := goldenBytes16(t, golden.CandidateIDHex)
	clientNonce := goldenBytes16(t, golden.ClientNonceHex)
	serverNonce := goldenBytes32(t, golden.ServerNonceHex)
	bindingID := goldenBytes16(t, golden.BindingIDHex)
	grantSecret := goldenBytes32(t, golden.GrantSecretHex)
	bindingKey := goldenBytes32(t, golden.BindingKeyHex)
	revision := goldenUint32(golden.Revision)
	expiry := goldenInt64(golden.ExpiryUnixMS)
	sequence := goldenUint64(golden.Sequence)
	payload := goldenHex(t, golden.PayloadHex)

	tests := []struct {
		name   string
		domain string
		key    Bytes32
		fields [][]byte
		frame  string
		output string
	}{
		{
			name:   "AUTH",
			domain: "relay-auth-v1",
			key:    grantSecret,
			fields: [][]byte{
				revision[:], []byte(golden.RoomID), []byte(golden.SessionID), grantID[:],
				candidateID[:], clientNonce[:], serverNonce[:],
			},
			frame:  golden.AuthFrameHex,
			output: golden.AuthTagHex,
		},
		{
			name:   "binding key",
			domain: "relay-binding-key-v1",
			key:    grantSecret,
			fields: [][]byte{
				revision[:], []byte(golden.RoomID), []byte(golden.SessionID), grantID[:],
				candidateID[:], clientNonce[:], serverNonce[:],
			},
			frame:  golden.BindingFrameHex,
			output: golden.BindingKeyHex,
		},
		{
			name:   "BOUND",
			domain: "relay-bound-v1",
			key:    bindingKey,
			fields: [][]byte{
				revision[:], []byte(golden.RoomID), []byte(golden.SessionID), candidateID[:],
				bindingID[:], expiry[:],
			},
			frame:  golden.BoundFrameHex,
			output: golden.BoundTagHex,
		},
		{
			name:   "ClientData",
			domain: "relay-client-data-v1",
			key:    bindingKey,
			fields: [][]byte{
				revision[:], []byte(golden.RoomID), []byte(golden.SessionID), bindingID[:],
				sequence[:], payload,
			},
			frame:  golden.ClientDataFrameHex,
			output: golden.ClientDataTagHex,
		},
		{
			name:   "Ping",
			domain: "relay-ping-v1",
			key:    bindingKey,
			fields: [][]byte{
				revision[:], []byte(golden.RoomID), []byte(golden.SessionID), bindingID[:], sequence[:],
			},
			frame:  golden.PingFrameHex,
			output: golden.PingTagHex,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frame := independentFrame(test.domain, test.fields...)
			if got := hex.EncodeToString(frame); got != test.frame {
				t.Fatalf("frame = %s, want %s", got, test.frame)
			}

			mac := hmac.New(sha256.New, test.key[:])
			_, _ = mac.Write(frame)
			if got := hex.EncodeToString(mac.Sum(nil)); got != test.output {
				t.Fatalf("HMAC = %s, want %s", got, test.output)
			}
		})
	}
}

func TestAuthenticationTranscriptsMatchGolden(t *testing.T) {
	golden := loadAuthGolden(t)
	grantID := goldenBytes16(t, golden.GrantIDHex)
	grantSecret := goldenBytes32(t, golden.GrantSecretHex)
	candidateID := goldenBytes16(t, golden.CandidateIDHex)
	clientNonce := goldenBytes16(t, golden.ClientNonceHex)
	serverNonce := goldenBytes32(t, golden.ServerNonceHex)
	bindingID := goldenBytes16(t, golden.BindingIDHex)
	bindingKey := goldenBytes32(t, golden.BindingKeyHex)
	payload := goldenHex(t, golden.PayloadHex)

	tests := []struct {
		name string
		got  Bytes32
		want string
	}{
		{
			name: "AUTH tag",
			got: AuthTag(
				grantSecret, golden.Revision, golden.RoomID, golden.SessionID,
				grantID, candidateID, clientNonce, serverNonce,
			),
			want: golden.AuthTagHex,
		},
		{
			name: "binding key",
			got: BindingKey(
				grantSecret, golden.Revision, golden.RoomID, golden.SessionID,
				grantID, candidateID, clientNonce, serverNonce,
			),
			want: golden.BindingKeyHex,
		},
		{
			name: "BOUND tag",
			got: BoundTag(
				bindingKey, golden.Revision, golden.RoomID, golden.SessionID,
				candidateID, bindingID, golden.ExpiryUnixMS,
			),
			want: golden.BoundTagHex,
		},
		{
			name: "ClientData tag",
			got: ClientDataTag(
				bindingKey, golden.Revision, golden.RoomID, golden.SessionID,
				bindingID, golden.Sequence, payload,
			),
			want: golden.ClientDataTagHex,
		},
		{
			name: "Ping tag",
			got: PingTag(
				bindingKey, golden.Revision, golden.RoomID, golden.SessionID,
				bindingID, golden.Sequence,
			),
			want: golden.PingTagHex,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hex.EncodeToString(test.got[:]); got != test.want {
				t.Fatalf("output = %s, want %s", got, test.want)
			}
		})
	}
}

func TestEqualTag(t *testing.T) {
	want := goldenBytes32(t, loadAuthGolden(t).AuthTagHex)
	exact := append([]byte(nil), want[:]...)
	changed := append([]byte(nil), exact...)
	changed[0] ^= 0xff

	tests := []struct {
		name string
		tag  []byte
		want bool
	}{
		{name: "exact", tag: exact, want: true},
		{name: "one byte changed", tag: changed, want: false},
		{name: "short", tag: exact[:len(exact)-1], want: false},
		{name: "long", tag: append(exact, 0), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := EqualTag(want, test.tag); got != test.want {
				t.Fatalf("EqualTag() = %t, want %t", got, test.want)
			}
		})
	}
}

func loadAuthGolden(t *testing.T) authGolden {
	t.Helper()
	contents, err := os.ReadFile("testdata/v1-golden.json")
	if err != nil {
		t.Fatal(err)
	}

	var golden authGolden
	if err := json.Unmarshal(contents, &golden); err != nil {
		t.Fatal(err)
	}
	return golden
}

func goldenBytes16(t *testing.T, encoded string) Bytes16 {
	t.Helper()
	raw := goldenHex(t, encoded)
	if len(raw) != len(Bytes16{}) {
		t.Fatalf("decoded length = %d, want %d", len(raw), len(Bytes16{}))
	}
	return Bytes16(raw)
}

func goldenBytes32(t *testing.T, encoded string) Bytes32 {
	t.Helper()
	raw := goldenHex(t, encoded)
	if len(raw) != len(Bytes32{}) {
		t.Fatalf("decoded length = %d, want %d", len(raw), len(Bytes32{}))
	}
	return Bytes32(raw)
}

func goldenHex(t *testing.T, encoded string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode %q: %v", encoded, err)
	}
	return raw
}

func independentFrame(domain string, fields ...[]byte) []byte {
	frame := make([]byte, 2, 2+len(domain))
	binary.BigEndian.PutUint16(frame, uint16(len(domain)))
	frame = append(frame, domain...)

	var fieldLength [4]byte
	for _, field := range fields {
		binary.BigEndian.PutUint32(fieldLength[:], uint32(len(field)))
		frame = append(frame, fieldLength[:]...)
		frame = append(frame, field...)
	}
	return frame
}

func goldenUint32(value uint32) [4]byte {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], value)
	return encoded
}

func goldenUint64(value uint64) [8]byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	return encoded
}

func goldenInt64(value int64) [8]byte {
	return goldenUint64(uint64(value))
}
