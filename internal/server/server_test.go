package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	relayv1 "github.com/gyungsubLee/go-lobby-relay/gen/go/relay/v1"
	"github.com/gyungsubLee/go-lobby-relay/internal/protocol"
	"google.golang.org/protobuf/proto"
)

var serverTestToken = func() (token [32]byte) {
	for index := range token {
		token[index] = 0x42
	}
	return token
}()

func TestNewValidatesBeforeBinding(t *testing.T) {
	valid := testServerConfig()
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"empty management listen", func(config *Config) { config.ManagementListen = "" }},
		{"invalid management listen", func(config *Config) { config.ManagementListen = "127.0.0.1" }},
		{"invalid relay network", func(config *Config) { config.RelayNetwork = "udp" }},
		{"empty relay listen", func(config *Config) { config.RelayListen = "" }},
		{"invalid relay listen", func(config *Config) { config.RelayListen = "127.0.0.1" }},
		{"relay family mismatch", func(config *Config) { config.RelayListen = "[::1]:0" }},
		{"empty advertised host", func(config *Config) { config.AdvertisedHost = "" }},
		{"zero advertised port for fixed relay port", func(config *Config) {
			config.RelayListen = "127.0.0.1:30000"
			config.AdvertisedPort = 0
		}},
		{"all-zero operator token", func(config *Config) { config.OperatorToken = [32]byte{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := valid
			tt.mutate(&config)
			var tcpCalls, udpCalls int
			deps := defaultDependencies()
			deps.listenTCP = func(string, string) (net.Listener, error) {
				tcpCalls++
				return nil, errors.New("must not bind")
			}
			deps.listenUDP = func(string, *net.UDPAddr) (*net.UDPConn, error) {
				udpCalls++
				return nil, errors.New("must not bind")
			}
			if server, err := newWithDependencies(config, deps); err == nil || server != nil {
				t.Fatalf("newWithDependencies() = (%#v, %v), want nil/error", server, err)
			}
			if tcpCalls != 0 || udpCalls != 0 {
				t.Fatalf("invalid config opened sockets: tcp=%d udp=%d", tcpCalls, udpCalls)
			}
		})
	}
}

func TestNewRollsBackTCPWhenUDPBindFails(t *testing.T) {
	config := testServerConfig()
	deps := defaultDependencies()
	var managementAddr string
	deps.listenTCP = func(network, address string) (net.Listener, error) {
		listener, err := net.Listen(network, address)
		if err == nil {
			managementAddr = listener.Addr().String()
		}
		return listener, err
	}
	deps.listenUDP = func(string, *net.UDPAddr) (*net.UDPConn, error) {
		return nil, errors.New("injected UDP bind failure")
	}

	if server, err := newWithDependencies(config, deps); err == nil || server != nil {
		t.Fatalf("newWithDependencies() = (%#v, %v), want nil/error", server, err)
	}
	if managementAddr == "" {
		t.Fatal("TCP listener was not opened before the UDP attempt")
	}
	rebound, err := net.Listen("tcp", managementAddr)
	if err != nil {
		t.Fatalf("TCP listener was not rolled back: %v", err)
	}
	_ = rebound.Close()
}

func TestRunSharesHTTPStoreWithRelayAndCancelsCleanly(t *testing.T) {
	config := testServerConfig()
	server, err := New(config)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	managementAddr, relayAddr := server.ManagementAddr(), server.RelayAddr()
	if managementAddr == nil || relayAddr == nil {
		t.Fatalf("bound addresses = %v/%v", managementAddr, relayAddr)
	}
	if server.runStarted {
		t.Fatal("New started work before Run")
	}
	assertBoundButNotServing(t, managementAddr.String(), relayAddr.(*net.UDPAddr), config.RelayNetwork)

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- server.Run(ctx) }()
	waitForManagement(t, managementAddr.String(), serverTestToken)

	allocation := createRoom(t, managementAddr.String(), serverTestToken, "room", 2)
	actualPort := uint16(relayAddr.(*net.UDPAddr).Port)
	if allocation.RelayEndpoint.Host != config.AdvertisedHost || allocation.RelayEndpoint.Port != actualPort {
		t.Fatalf("advertised endpoint = %#v, want %s:%d", allocation.RelayEndpoint, config.AdvertisedHost, actualPort)
	}
	if allocation.ProtocolRevision != protocol.Revision || allocation.MaxDatagramBytes != protocol.MaxDatagramBytes ||
		allocation.MaxPayloadBytes != protocol.MaxPayloadBytes || len(allocation.Grants) != 2 {
		t.Fatalf("allocation response = %#v", allocation)
	}

	relayEndpoint := relayAddr.(*net.UDPAddr).AddrPort()
	first := newAllocatedClient(t)
	second := newAllocatedClient(t)
	first.bind(t, relayEndpoint, "room", allocation.Grants[0], 0x71)
	second.bind(t, relayEndpoint, "room", allocation.Grants[1], 0x72)
	payload := []byte{0, 1, 2, 0xfe, 0xff, 'x'}
	first.sendData(t, relayEndpoint, 1, payload)
	second.expectData(t, first.participantID, 1, payload)
	first.expectSilence(t)
	_ = first.conn.Close()
	_ = second.conn.Close()

	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run() after context cancellation = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not join after context cancellation")
	}
	if err := server.Close(); err != nil {
		t.Fatalf("Close() after Run: %v", err)
	}
	assertAddressesRebind(t, managementAddr, relayAddr, config.RelayNetwork)
}

func TestCloseBeforeDuringAndAfterRunIsIdempotent(t *testing.T) {
	t.Run("before Run", func(t *testing.T) {
		config := testServerConfig()
		server, err := New(config)
		if err != nil {
			t.Fatalf("New(): %v", err)
		}
		managementAddr, relayAddr := server.ManagementAddr(), server.RelayAddr()
		var wait sync.WaitGroup
		for range 16 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				if err := server.Close(); err != nil {
					t.Errorf("Close(): %v", err)
				}
			}()
		}
		wait.Wait()
		if err := server.Run(context.Background()); err != nil {
			t.Fatalf("Run() after pre-Close = %v", err)
		}
		if server.runStarted {
			t.Fatal("Run after pre-Close started owned work")
		}
		assertAddressesRebind(t, managementAddr, relayAddr, config.RelayNetwork)
	})

	t.Run("during and after Run", func(t *testing.T) {
		config := testServerConfig()
		server, err := New(config)
		if err != nil {
			t.Fatalf("New(): %v", err)
		}
		managementAddr, relayAddr := server.ManagementAddr(), server.RelayAddr()
		runDone := make(chan error, 1)
		go func() { runDone <- server.Run(context.Background()) }()
		waitForManagement(t, managementAddr.String(), serverTestToken)

		var wait sync.WaitGroup
		for range 16 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				if err := server.Close(); err != nil {
					t.Errorf("Close(): %v", err)
				}
			}()
		}
		wait.Wait()
		select {
		case err := <-runDone:
			if err != nil {
				t.Fatalf("Run() after local Close = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not join after local Close")
		}
		if err := server.Close(); err != nil {
			t.Fatalf("Close() after join: %v", err)
		}
		assertAddressesRebind(t, managementAddr, relayAddr, config.RelayNetwork)
	})
}

func TestUnexpectedOwnedLoopFailureCancelsSiblings(t *testing.T) {
	config := testServerConfig()
	server, err := New(config)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	managementAddr, relayAddr := server.ManagementAddr(), server.RelayAddr()
	runDone := make(chan error, 1)
	go func() { runDone <- server.Run(context.Background()) }()
	waitForManagement(t, managementAddr.String(), serverTestToken)

	if err := server.managementListener.Close(); err != nil {
		t.Fatalf("close owned listener: %v", err)
	}
	select {
	case err := <-runDone:
		if err == nil {
			t.Fatal("Run() unexpected listener failure = nil")
		}
		encoded := base64.RawURLEncoding.EncodeToString(serverTestToken[:])
		if strings.Contains(err.Error(), encoded) || strings.Contains(err.Error(), string(serverTestToken[:])) {
			t.Fatalf("Run error disclosed operator token: %q", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unexpected loop failure did not join siblings")
	}
	if err := server.Close(); err != nil {
		t.Fatalf("Close() after unexpected failure: %v", err)
	}
	assertAddressesRebind(t, managementAddr, relayAddr, config.RelayNetwork)
}

func TestQueuedUnexpectedLoopFailureWinsLaterCancellation(t *testing.T) {
	server := &Server{closeSignal: make(chan struct{})}
	runContext, cancel := context.WithCancel(context.Background())
	results := make(chan loopResult, 3)
	results <- server.classifyLoopResult(runContext, "management", errors.New("sensitive cause"))
	cancel()
	results <- server.classifyLoopResult(runContext, "relay", net.ErrClosed)
	results <- server.classifyLoopResult(runContext, "sweeper", nil)

	err := coordinateLoopResults(runContext, server.closeSignal, make(chan struct{}), results, func() {})
	if err != errOwnedLoop {
		t.Fatalf("queued unexpected result after cancellation = %v, want generic %v", err, errOwnedLoop)
	}
}

func TestHTTPFatalRandomStopsServerAndJoinsSiblings(t *testing.T) {
	config := testServerConfig()
	deps := defaultDependencies()
	deps.random = failingReader{}
	server, err := newWithDependencies(config, deps)
	if err != nil {
		t.Fatalf("newWithDependencies(): %v", err)
	}
	managementAddr, relayAddr := server.ManagementAddr(), server.RelayAddr()
	if server.runStarted {
		t.Fatal("New started work before Run")
	}
	assertBoundButNotServing(t, managementAddr.String(), relayAddr.(*net.UDPAddr), config.RelayNetwork)

	runDone := make(chan error, 1)
	go func() { runDone <- server.Run(context.Background()) }()
	waitForManagement(t, managementAddr.String(), serverTestToken)
	now := time.Now().UTC()
	body := []byte(`{"capacity":1,"expires_at":"` + now.Add(time.Hour).Format(time.RFC3339Nano) +
		`","participants":[{"participant_id":"participant","session_id":"session","grant_expires_at":"` +
		now.Add(30*time.Minute).Format(time.RFC3339Nano) + `"}]}`)
	request, err := http.NewRequest(http.MethodPut, "http://"+managementAddr.String()+"/v1/rooms/room", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest(): %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(serverTestToken[:]))
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: time.Second}).Do(request)
	if err != nil {
		t.Fatalf("fatal PUT room: %v", err)
	}
	responseBody, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatalf("read fatal response: %v", readErr)
	}
	wantBody := "{\"error\":{\"code\":\"internal_error\",\"message\":\"internal server error\"}}\n"
	if response.StatusCode != http.StatusInternalServerError || string(responseBody) != wantBody {
		t.Fatalf("fatal PUT room = %d %q", response.StatusCode, responseBody)
	}

	select {
	case err := <-runDone:
		if err != errOwnedLoop {
			t.Fatalf("Run() after HTTP fatal random = %v, want %v", err, errOwnedLoop)
		}
		encoded := base64.RawURLEncoding.EncodeToString(serverTestToken[:])
		if strings.Contains(err.Error(), encoded) || strings.Contains(err.Error(), string(serverTestToken[:])) {
			t.Fatalf("Run error disclosed operator token: %q", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP fatal random did not join owned loops")
	}
	if err := server.Close(); err != nil {
		t.Fatalf("Close() after HTTP fatal random: %v", err)
	}
	assertAddressesRebind(t, managementAddr, relayAddr, config.RelayNetwork)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("injected random failure") }

func testServerConfig() Config {
	return Config{
		ManagementListen: "127.0.0.1:0",
		RelayNetwork:     "udp4",
		RelayListen:      "127.0.0.1:0",
		AdvertisedHost:   "relay.test",
		AdvertisedPort:   0,
		OperatorToken:    serverTestToken,
	}
}

func assertBoundButNotServing(t *testing.T, management string, relayAddress *net.UDPAddr, relayNetwork string) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", management, time.Second)
	if err != nil {
		t.Fatalf("management listener was not bound: %v", err)
	}
	if _, err := io.WriteString(connection, "GET /v1/rooms/missing HTTP/1.1\r\nHost: "+management+"\r\n\r\n"); err != nil {
		t.Fatalf("write pre-Run request: %v", err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	var one [1]byte
	if _, err := connection.Read(one[:]); err == nil {
		t.Fatal("management served a response before Run")
	} else if networkError, ok := err.(net.Error); !ok || !networkError.Timeout() {
		t.Fatalf("pre-Run read = %v, want timeout", err)
	}
	_ = connection.Close()

	if listener, err := net.Listen("tcp", management); err == nil {
		_ = listener.Close()
		t.Fatal("management address was not held after New")
	}
	if socket, err := net.ListenUDP(relayNetwork, relayAddress); err == nil {
		_ = socket.Close()
		t.Fatal("relay address was not held after New")
	}
}

func waitForManagement(t *testing.T, address string, token [32]byte) {
	t.Helper()
	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		request, err := http.NewRequest(http.MethodGet, "http://"+address+"/v1/rooms/missing", nil)
		if err != nil {
			t.Fatalf("NewRequest(): %v", err)
		}
		request.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(token[:]))
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusNotFound {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("management listener did not start serving")
}

type testAllocationResponse struct {
	RelayEndpoint struct {
		Host string `json:"host"`
		Port uint16 `json:"port"`
	} `json:"relay_endpoint"`
	ProtocolRevision uint32              `json:"protocol_revision"`
	MaxDatagramBytes uint32              `json:"max_datagram_bytes"`
	MaxPayloadBytes  uint32              `json:"max_payload_bytes"`
	Grants           []testGrantResponse `json:"grants"`
}

type testGrantResponse struct {
	ParticipantID string  `json:"participant_id"`
	SessionID     string  `json:"session_id"`
	GrantID       string  `json:"grant_id"`
	GrantSecret   *string `json:"grant_secret"`
}

func createRoom(t *testing.T, address string, token [32]byte, roomID string, participants int) testAllocationResponse {
	t.Helper()
	now := time.Now().UTC()
	requestBody := struct {
		Capacity     uint32 `json:"capacity"`
		ExpiresAt    string `json:"expires_at"`
		Participants []struct {
			ParticipantID  string `json:"participant_id"`
			SessionID      string `json:"session_id"`
			GrantExpiresAt string `json:"grant_expires_at"`
		} `json:"participants"`
	}{Capacity: uint32(participants), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339Nano)}
	requestBody.Participants = make([]struct {
		ParticipantID  string `json:"participant_id"`
		SessionID      string `json:"session_id"`
		GrantExpiresAt string `json:"grant_expires_at"`
	}, participants)
	for index := range requestBody.Participants {
		requestBody.Participants[index].ParticipantID = "participant-" + string(rune('a'+index))
		requestBody.Participants[index].SessionID = "session-" + string(rune('a'+index))
		requestBody.Participants[index].GrantExpiresAt = now.Add(30 * time.Minute).Format(time.RFC3339Nano)
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("json.Marshal(): %v", err)
	}
	request, err := http.NewRequest(http.MethodPut, "http://"+address+"/v1/rooms/"+roomID, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest(): %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(token[:]))
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: time.Second}).Do(request)
	if err != nil {
		t.Fatalf("PUT room: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		responseBody, _ := io.ReadAll(response.Body)
		t.Fatalf("PUT room = %d %q", response.StatusCode, responseBody)
	}
	var allocation testAllocationResponse
	if err := json.NewDecoder(response.Body).Decode(&allocation); err != nil {
		t.Fatalf("decode allocation: %v", err)
	}
	return allocation
}

type allocatedClient struct {
	conn                             *net.UDPConn
	roomID, sessionID, participantID string
	bindingID                        protocol.Bytes16
	key                              protocol.Bytes32
}

func newAllocatedClient(t *testing.T) *allocatedClient {
	t.Helper()
	connection, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:0")))
	if err != nil {
		t.Fatalf("ListenUDP(client): %v", err)
	}
	return &allocatedClient{conn: connection}
}

func (client *allocatedClient) bind(t *testing.T, relayEndpoint netip.AddrPort, roomID string, grant testGrantResponse, nonceByte byte) {
	t.Helper()
	grantID := decode16(t, grant.GrantID)
	if grant.GrantSecret == nil {
		t.Fatal("allocation omitted grant secret")
	}
	secret := decode32(t, *grant.GrantSecret)
	var nonce protocol.Bytes16
	for index := range nonce {
		nonce[index] = nonceByte
	}
	hello := &relayv1.Envelope{
		ProtocolRevision: protocol.Revision, RoomId: roomID, SessionId: grant.SessionID,
		Body: &relayv1.Envelope_Hello{Hello: &relayv1.Hello{
			GrantId: grantID[:], ClientNonce: nonce[:], Padding: make([]byte, protocol.MinHelloBytes),
		}},
	}
	client.send(t, relayEndpoint, marshalEnvelope(t, hello))
	challengeEnvelope := client.receive(t)
	challenge := challengeEnvelope.GetChallenge()
	if challenge == nil {
		t.Fatalf("HELLO response body = %T", challengeEnvelope.Body)
	}
	candidateID := bytesTo16(t, challenge.CandidateId)
	serverNonce := bytesTo32(t, challenge.ServerNonce)
	authTag := protocol.AuthTag(secret, protocol.Revision, roomID, grant.SessionID, grantID, candidateID, nonce, serverNonce)
	client.send(t, relayEndpoint, marshalEnvelope(t, &relayv1.Envelope{
		ProtocolRevision: protocol.Revision, RoomId: roomID, SessionId: grant.SessionID, AuthTag: authTag[:],
		Body: &relayv1.Envelope_Auth{Auth: &relayv1.Auth{CandidateId: candidateID[:]}},
	}))
	boundEnvelope := client.receive(t)
	bound := boundEnvelope.GetBound()
	if bound == nil {
		t.Fatalf("AUTH response body = %T", boundEnvelope.Body)
	}
	bindingID := bytesTo16(t, bound.BindingId)
	key := protocol.BindingKey(secret, protocol.Revision, roomID, grant.SessionID, grantID, candidateID, nonce, serverNonce)
	wantTag := protocol.BoundTag(key, protocol.Revision, roomID, grant.SessionID, candidateID, bindingID, bound.ExpiresUnixMs)
	if !protocol.EqualTag(wantTag, boundEnvelope.AuthTag) {
		t.Fatal("BOUND tag mismatch")
	}
	client.roomID, client.sessionID, client.participantID = roomID, grant.SessionID, grant.ParticipantID
	client.bindingID, client.key = bindingID, key
}

func (client *allocatedClient) sendData(t *testing.T, relayEndpoint netip.AddrPort, sequence uint64, payload []byte) {
	t.Helper()
	tag := protocol.ClientDataTag(client.key, protocol.Revision, client.roomID, client.sessionID, client.bindingID, sequence, payload)
	client.send(t, relayEndpoint, marshalEnvelope(t, &relayv1.Envelope{
		ProtocolRevision: protocol.Revision, Sequence: sequence, RoomId: client.roomID, SessionId: client.sessionID,
		AuthTag: tag[:], Body: &relayv1.Envelope_ClientData{ClientData: &relayv1.ClientData{
			BindingId: client.bindingID[:], Payload: append([]byte(nil), payload...),
		}},
	}))
}

func (client *allocatedClient) send(t *testing.T, endpoint netip.AddrPort, datagram []byte) {
	t.Helper()
	_ = client.conn.SetWriteDeadline(time.Now().Add(time.Second))
	written, err := client.conn.WriteToUDPAddrPort(datagram, endpoint)
	if err != nil || written != len(datagram) {
		t.Fatalf("WriteToUDPAddrPort() = (%d, %v), want %d", written, err, len(datagram))
	}
}

func (client *allocatedClient) receive(t *testing.T) *relayv1.Envelope {
	t.Helper()
	_ = client.conn.SetReadDeadline(time.Now().Add(time.Second))
	buffer := make([]byte, protocol.MaxDatagramBytes+1)
	read, _, err := client.conn.ReadFromUDPAddrPort(buffer)
	if err != nil {
		t.Fatalf("ReadFromUDPAddrPort(): %v", err)
	}
	envelope := new(relayv1.Envelope)
	if err := proto.Unmarshal(buffer[:read], envelope); err != nil {
		t.Fatalf("proto.Unmarshal(): %v", err)
	}
	return envelope
}

func (client *allocatedClient) expectData(t *testing.T, participantID string, sequence uint64, payload []byte) {
	t.Helper()
	envelope := client.receive(t)
	data := envelope.GetServerData()
	if data == nil || data.SenderParticipantId != participantID || envelope.Sequence != sequence || !bytes.Equal(data.Payload, payload) {
		t.Fatalf("ServerData = %#v", envelope)
	}
}

func (client *allocatedClient) expectSilence(t *testing.T) {
	t.Helper()
	_ = client.conn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	var buffer [protocol.MaxDatagramBytes + 1]byte
	if read, endpoint, err := client.conn.ReadFromUDPAddrPort(buffer[:]); err == nil {
		t.Fatalf("unexpected datagram: %d bytes from %v", read, endpoint)
	} else if networkError, ok := err.(net.Error); !ok || !networkError.Timeout() {
		t.Fatalf("ReadFromUDPAddrPort() = %v, want timeout", err)
	}
}

func marshalEnvelope(t *testing.T, envelope *relayv1.Envelope) []byte {
	t.Helper()
	datagram, err := (proto.MarshalOptions{Deterministic: true}).Marshal(envelope)
	if err != nil {
		t.Fatalf("proto.Marshal(): %v", err)
	}
	return datagram
}

func decode16(t *testing.T, encoded string) protocol.Bytes16 {
	t.Helper()
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode 16 bytes: %v", err)
	}
	return bytesTo16(t, decoded)
}

func decode32(t *testing.T, encoded string) protocol.Bytes32 {
	t.Helper()
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode 32 bytes: %v", err)
	}
	return bytesTo32(t, decoded)
}

func bytesTo16(t *testing.T, decoded []byte) (value protocol.Bytes16) {
	t.Helper()
	if len(decoded) != len(value) {
		t.Fatalf("decoded length = %d, want %d", len(decoded), len(value))
	}
	copy(value[:], decoded)
	return value
}

func bytesTo32(t *testing.T, decoded []byte) (value protocol.Bytes32) {
	t.Helper()
	if len(decoded) != len(value) {
		t.Fatalf("decoded length = %d, want %d", len(decoded), len(value))
	}
	copy(value[:], decoded)
	return value
}

func assertAddressesRebind(t *testing.T, managementAddr, relayAddr net.Addr, relayNetwork string) {
	t.Helper()
	management, err := net.Listen("tcp", managementAddr.String())
	if err != nil {
		t.Fatalf("rebind management %s: %v", managementAddr, err)
	}
	_ = management.Close()
	relay, err := net.ListenUDP(relayNetwork, relayAddr.(*net.UDPAddr))
	if err != nil {
		t.Fatalf("rebind relay %s: %v", relayAddr, err)
	}
	_ = relay.Close()
}
