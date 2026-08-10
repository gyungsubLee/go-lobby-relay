package store

import (
	"errors"
	"math"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/gyungsubLee/go-game-relay/internal/protocol"
	"golang.org/x/time/rate"
)

func TestNewRejectsInvalidPreauthLimits(t *testing.T) {
	rateFields := []struct {
		name string
		max  rate.Limit
		set  func(*Limits, rate.Limit)
	}{
		{"source packet rate", HardMaxPreauthSourcePacketRate, func(l *Limits, value rate.Limit) { l.PreauthSourcePacketRate = value }},
		{"source byte rate", HardMaxPreauthSourceByteRate, func(l *Limits, value rate.Limit) { l.PreauthSourceByteRate = value }},
		{"global packet rate", HardMaxPreauthGlobalPacketRate, func(l *Limits, value rate.Limit) { l.PreauthGlobalPacketRate = value }},
		{"global byte rate", HardMaxPreauthGlobalByteRate, func(l *Limits, value rate.Limit) { l.PreauthGlobalByteRate = value }},
	}
	for _, field := range rateFields {
		for _, value := range []rate.Limit{0, -1, rate.Limit(math.Inf(1)), rate.Limit(math.NaN()), field.max + 1} {
			limits := DefaultLimits()
			field.set(&limits, value)
			if _, err := New(Config{Limits: limits}); !errors.Is(err, ErrInvalid) {
				t.Fatalf("New(%s=%v) error = %v, want ErrInvalid", field.name, value, err)
			}
		}
	}
	burstFields := []struct {
		name string
		max  int
		set  func(*Limits, int)
	}{
		{"source packet burst", HardMaxPreauthSourcePacketBurst, func(l *Limits, value int) { l.PreauthSourcePacketBurst = value }},
		{"source byte burst", HardMaxPreauthSourceByteBurst, func(l *Limits, value int) { l.PreauthSourceByteBurst = value }},
		{"global packet burst", HardMaxPreauthGlobalPacketBurst, func(l *Limits, value int) { l.PreauthGlobalPacketBurst = value }},
		{"global byte burst", HardMaxPreauthGlobalByteBurst, func(l *Limits, value int) { l.PreauthGlobalByteBurst = value }},
	}
	for _, field := range burstFields {
		for _, value := range []int{0, -1, field.max + 1} {
			limits := DefaultLimits()
			field.set(&limits, value)
			if _, err := New(Config{Limits: limits}); !errors.Is(err, ErrInvalid) {
				t.Fatalf("New(%s=%d) error = %v, want ErrInvalid", field.name, value, err)
			}
		}
	}
}

func TestReplayWindowMatrix(t *testing.T) {
	var window replayWindow
	for _, sequence := range []uint64{1, 2, 65, 3, math.MaxUint64} {
		if !window.accept(sequence) {
			t.Fatalf("fresh sequence %d was rejected", sequence)
		}
	}
	if window.accept(math.MaxUint64) {
		t.Fatal("duplicate MaxUint64 was accepted")
	}
	if !window.accept(math.MaxUint64 - 63) {
		t.Fatal("highest-63 unseen sequence was rejected")
	}
	if window.accept(math.MaxUint64 - 63) {
		t.Fatal("highest-63 duplicate was accepted")
	}
	if window.accept(math.MaxUint64 - 64) {
		t.Fatal("highest-64 sequence was accepted")
	}
	if window.highest != math.MaxUint64 {
		t.Fatalf("highest = %d, want MaxUint64", window.highest)
	}
}

func TestBeginChallengeValidatesAuthorityAndIsIdempotent(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
	endpoint := netip.MustParseAddrPort("192.0.2.10:4000")
	clientNonce := bytes16(0x31)
	fixture.random.reset(filled(0x41, 16), filled(0x51, 32))
	request := fixture.challengeRequest(0, clientNonce, endpoint)

	challenge, reason := fixture.store.BeginChallenge(request)
	if reason != RejectNone {
		t.Fatalf("BeginChallenge() reason = %q", reason)
	}
	if challenge.CandidateID != bytes16(0x41) || challenge.ServerNonce != bytes32(0x51) ||
		challenge.ExpiresUnixMS != testWall.Add(3*time.Second).UnixMilli() {
		t.Fatalf("challenge = %#v", challenge)
	}
	assertReads(t, fixture.random.calls, 16, 32)
	assertNoSecretFields(t, ChallengeResult{})
	if challenge.ServerNonce == fixture.secret(0) {
		t.Fatal("challenge exposed the grant secret")
	}

	duplicate, reason := fixture.store.BeginChallenge(request)
	if reason != RejectNone || duplicate != challenge {
		t.Fatalf("duplicate BeginChallenge() = (%#v, %q), want same challenge", duplicate, reason)
	}
	assertReads(t, fixture.random.calls, 16, 32)

	differentNonce := request
	differentNonce.ClientNonce = bytes16(0x32)
	if got, reason := fixture.store.BeginChallenge(differentNonce); reason != RejectAuthFailed || got != (ChallengeResult{}) {
		t.Fatalf("different nonce while pending = (%#v, %q), want auth_failed", got, reason)
	}
	differentEndpoint := request
	differentEndpoint.Endpoint = netip.MustParseAddrPort("192.0.2.10:4001")
	if got, reason := fixture.store.BeginChallenge(differentEndpoint); reason != RejectWrongEndpoint || got != (ChallengeResult{}) {
		t.Fatalf("different endpoint while pending = (%#v, %q), want wrong_endpoint", got, reason)
	}

	wrongRoom := request
	wrongRoom.RoomID = "other-room"
	if _, reason := fixture.store.BeginChallenge(wrongRoom); reason != RejectWrongRoom {
		t.Fatalf("wrong room reason = %q", reason)
	}
	wrongSession := request
	wrongSession.SessionID = "other-session"
	if _, reason := fixture.store.BeginChallenge(wrongSession); reason != RejectAuthFailed {
		t.Fatalf("wrong session reason = %q", reason)
	}
	unknown := request
	unknown.GrantID = bytes16(0xee)
	if _, reason := fixture.store.BeginChallenge(unknown); reason != RejectUnknownGrant {
		t.Fatalf("unknown grant reason = %q", reason)
	}
	assertStoreInvariants(t, fixture.store)
}

func TestBeginChallengeRejectsRememberedNonceAcrossEndpoints(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
	oldEndpoint := netip.MustParseAddrPort("192.0.2.14:4000")
	newEndpoint := netip.MustParseAddrPort("192.0.2.15:4000")
	nonce := bytes16(0x35)
	fixture.random.reset(filled(0x44, 16), filled(0x54, 32), filled(0x64, 16))
	challenge, reason := fixture.store.BeginChallenge(fixture.challengeRequest(0, nonce, oldEndpoint))
	if reason != RejectNone {
		t.Fatalf("BeginChallenge(): %q", reason)
	}
	if _, reason := fixture.store.Authenticate(fixture.authRequest(0, challenge, nonce, oldEndpoint)); reason != RejectNone {
		t.Fatalf("Authenticate(): %q", reason)
	}
	grant := fixture.grant(0)
	oldBinding, oldRecent := grant.binding, grant.recent
	oldBindingValue, oldRecentValue := *oldBinding, *oldRecent
	fixture.random.reset(filled(0x45, 16), filled(0x55, 32))

	if got, reason := fixture.store.BeginChallenge(fixture.challengeRequest(0, nonce, newEndpoint)); reason != RejectAuthFailed || got != (ChallengeResult{}) {
		t.Fatalf("remembered nonce from new endpoint = (%#v, %q), want auth_failed", got, reason)
	}
	if len(fixture.random.calls) != 0 || grant.pending != nil || grant.binding != oldBinding || grant.recent != oldRecent ||
		*oldBinding != oldBindingValue || *oldRecent != oldRecentValue {
		t.Fatal("remembered nonce rejection used randomness or changed current state")
	}
}

func TestChallengeExactDeadlineAndTerminalGrantPaths(t *testing.T) {
	t.Run("challenge deadline", func(t *testing.T) {
		fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
		endpoint := netip.MustParseAddrPort("192.0.2.11:4000")
		fixture.random.reset(filled(0x42, 16), filled(0x52, 32))
		challenge, reason := fixture.store.BeginChallenge(fixture.challengeRequest(0, bytes16(0x33), endpoint))
		if reason != RejectNone {
			t.Fatalf("BeginChallenge(): %q", reason)
		}
		fixture.clock.reading = ClockReading{Wall: testWall.Add(3 * time.Second), Mono: 3 * time.Second}
		if _, reason := fixture.store.Authenticate(fixture.authRequest(0, challenge, bytes16(0x33), endpoint)); reason != RejectExpired {
			t.Fatalf("Authenticate(exact challenge deadline) reason = %q", reason)
		}
		fixture.store.mu.RLock()
		defer fixture.store.mu.RUnlock()
		if fixture.grant(0).pending != nil || fixture.store.candidatesByID[challenge.CandidateID] != nil {
			t.Fatal("expired candidate remained indexed")
		}
	})

	t.Run("grant deadline", func(t *testing.T) {
		fixture := newHandshakeFixture(t, time.Hour, 2*time.Second, 1)
		fixture.clock.reading = ClockReading{Wall: testWall.Add(2 * time.Second), Mono: 2 * time.Second}
		if _, reason := fixture.store.BeginChallenge(fixture.challengeRequest(0, bytes16(1), netip.MustParseAddrPort("192.0.2.12:4000"))); reason != RejectExpired {
			t.Fatalf("BeginChallenge(exact grant deadline) reason = %q", reason)
		}
		if fixture.grant(0).secret != nil || fixture.grant(0).state != GrantStateExpired {
			t.Fatal("expired grant retained authority")
		}
	})

	t.Run("revoked challenge", func(t *testing.T) {
		fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
		endpoint := netip.MustParseAddrPort("192.0.2.13:4000")
		fixture.random.reset(filled(0x43, 16), filled(0x53, 32))
		challenge, reason := fixture.store.BeginChallenge(fixture.challengeRequest(0, bytes16(0x34), endpoint))
		if reason != RejectNone {
			t.Fatalf("BeginChallenge(): %q", reason)
		}
		if err := fixture.store.EndRoom("room"); err != nil {
			t.Fatalf("EndRoom(): %v", err)
		}
		if _, reason := fixture.store.Authenticate(fixture.authRequest(0, challenge, bytes16(0x34), endpoint)); reason != RejectAuthFailed {
			t.Fatalf("Authenticate(revoked candidate) reason = %q, want auth_failed", reason)
		}
		assertStoreInvariants(t, fixture.store)
	})
}

func TestChallengeRandomnessIsStagedAndCollisionBounded(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 3)
	endpoint := netip.MustParseAddrPort("198.51.100.1:5000")
	collision := bytes16(0x61)
	fixture.random.reset(filled(0x61, 16), filled(0x71, 32))
	if _, reason := fixture.store.BeginChallenge(fixture.challengeRequest(0, bytes16(1), endpoint)); reason != RejectNone {
		t.Fatalf("first challenge reason = %q", reason)
	}

	chunks := make([][]byte, 0, 10)
	for range 8 {
		chunks = append(chunks, filled(0x61, 16))
	}
	chunks = append(chunks, filled(0x62, 16), filled(0x72, 32))
	fixture.random.reset(chunks...)
	second, reason := fixture.store.BeginChallenge(fixture.challengeRequest(1, bytes16(2), netip.MustParseAddrPort("198.51.100.2:5000")))
	if reason != RejectNone || second.CandidateID != bytes16(0x62) {
		t.Fatalf("ninth-draw collision success = (%#v, %q)", second, reason)
	}
	assertReads(t, fixture.random.calls, 16, 16, 16, 16, 16, 16, 16, 16, 16, 32)

	chunks = chunks[:0]
	for range 9 {
		chunks = append(chunks, filled(collision[0], 16))
	}
	fixture.random.reset(chunks...)
	beforeCandidates := len(fixture.store.candidatesByID)
	if got, reason := fixture.store.BeginChallenge(fixture.challengeRequest(2, bytes16(3), netip.MustParseAddrPort("198.51.100.3:5000"))); reason != RejectFatalRandom || got != (ChallengeResult{}) {
		t.Fatalf("collision exhaustion = (%#v, %q)", got, reason)
	}
	if len(fixture.store.candidatesByID) != beforeCandidates || fixture.grant(2).pending != nil {
		t.Fatal("collision exhaustion committed partial candidate state")
	}

	for _, tt := range []struct {
		name   string
		chunks [][]byte
		failAt int
	}{
		{name: "short candidate", chunks: [][]byte{filled(1, 15)}},
		{name: "short nonce", chunks: [][]byte{filled(0x63, 16), filled(1, 31)}},
		{name: "reader error", failAt: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fresh := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
			fresh.random.reset(tt.chunks...)
			fresh.random.failAt = tt.failAt
			fresh.random.failure = errors.New("random failed")
			if _, reason := fresh.store.BeginChallenge(fresh.challengeRequest(0, bytes16(4), endpoint)); reason != RejectFatalRandom {
				t.Fatalf("BeginChallenge() reason = %q", reason)
			}
			if len(fresh.store.candidatesByID) != 0 || fresh.grant(0).pending != nil {
				t.Fatal("random failure committed partial challenge")
			}
		})
	}
}

func TestAuthenticateBindsAndReplaysCurrentBound(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
	endpoint := netip.MustParseAddrPort("203.0.113.10:6000")
	clientNonce := bytes16(0x81)
	fixture.random.reset(filled(0x82, 16), filled(0x83, 32), filled(0x84, 16))
	challenge, reason := fixture.store.BeginChallenge(fixture.challengeRequest(0, clientNonce, endpoint))
	if reason != RejectNone {
		t.Fatalf("BeginChallenge(): %q", reason)
	}
	auth := fixture.authRequest(0, challenge, clientNonce, endpoint)

	wrongEndpoint := auth
	wrongEndpoint.Endpoint = netip.MustParseAddrPort("203.0.113.10:6001")
	if _, reason := fixture.store.Authenticate(wrongEndpoint); reason != RejectWrongEndpoint {
		t.Fatalf("wrong endpoint reason = %q", reason)
	}
	badTag := auth
	badTag.AuthTag[0] ^= 1
	if _, reason := fixture.store.Authenticate(badTag); reason != RejectAuthFailed {
		t.Fatalf("bad HMAC reason = %q", reason)
	}
	if fixture.grant(0).pending == nil {
		t.Fatal("failed AUTH consumed candidate")
	}

	bound, reason := fixture.store.Authenticate(auth)
	if reason != RejectNone {
		t.Fatalf("Authenticate() reason = %q", reason)
	}
	wantKey := protocol.BindingKey(fixture.secret(0), protocol.Revision, "room", fixture.session(0),
		fixture.grantID(0), challenge.CandidateID, clientNonce, challenge.ServerNonce)
	wantTag := protocol.BoundTag(wantKey, protocol.Revision, "room", fixture.session(0),
		challenge.CandidateID, bytes16(0x84), testWall.Add(60*time.Second).UnixMilli())
	if bound.BindingID != bytes16(0x84) || bound.ExpiresUnixMS != testWall.Add(60*time.Second).UnixMilli() || bound.AuthTag != wantTag {
		t.Fatalf("BOUND = %#v, want id=%x expiry=%d tag=%x", bound, bytes16(0x84), testWall.Add(60*time.Second).UnixMilli(), wantTag)
	}
	assertNoSecretFields(t, BoundResult{})
	grant := fixture.grant(0)
	if grant.pending != nil || grant.recent == nil || grant.binding == nil || grant.binding.key != wantKey ||
		grant.binding.endpoint != endpoint || grant.binding.replay != (replayWindow{}) || grant.state != GrantStateBound {
		t.Fatalf("bound grant state = %#v", grant)
	}
	if fixture.store.bindingsByID[bound.BindingID] != grant || fixture.store.candidatesByID[challenge.CandidateID] != grant {
		t.Fatal("current binding/recent completion indexes are inconsistent")
	}

	reads := append([]int(nil), fixture.random.calls...)
	duplicate, reason := fixture.store.Authenticate(auth)
	if reason != RejectNone || duplicate != bound {
		t.Fatalf("duplicate AUTH = (%#v, %q), want same BOUND", duplicate, reason)
	}
	if !reflect.DeepEqual(fixture.random.calls, reads) {
		t.Fatalf("duplicate AUTH used randomness: %v -> %v", reads, fixture.random.calls)
	}
	assertStoreInvariants(t, fixture.store)
}

func TestBindingsAllowSessionsToShareOneNATEndpoint(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 2)
	endpoint := netip.MustParseAddrPort("203.0.113.15:6000")
	for index := range 2 {
		nonce := bytes16(byte(0x85 + index))
		fixture.random.reset(filled(byte(0x87+index), 16), filled(byte(0x89+index), 32), filled(byte(0x8b+index), 16))
		challenge, reason := fixture.store.BeginChallenge(fixture.challengeRequest(index, nonce, endpoint))
		if reason != RejectNone {
			t.Fatalf("BeginChallenge(%d): %q", index, reason)
		}
		if _, reason := fixture.store.Authenticate(fixture.authRequest(index, challenge, nonce, endpoint)); reason != RejectNone {
			t.Fatalf("Authenticate(%d): %q", index, reason)
		}
	}
	if fixture.grant(0).binding.endpoint != endpoint || fixture.grant(1).binding.endpoint != endpoint || len(fixture.store.bindingsByID) != 2 {
		t.Fatal("shared NAT endpoint displaced one session binding")
	}
}

func TestAuthenticateBindingDeadlineUsesMinimumAuthority(t *testing.T) {
	tests := []struct {
		name       string
		roomTTL    time.Duration
		grantTTL   time.Duration
		wantExpiry time.Duration
	}{
		{name: "binding TTL", roomTTL: time.Hour, grantTTL: 30 * time.Minute, wantExpiry: 60 * time.Second},
		{name: "grant TTL", roomTTL: time.Hour, grantTTL: 30 * time.Second, wantExpiry: 30 * time.Second},
		{name: "room TTL", roomTTL: 20 * time.Second, grantTTL: 20 * time.Second, wantExpiry: 20 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newHandshakeFixture(t, tt.roomTTL, tt.grantTTL, 1)
			if tt.name == "grant TTL" {
				fixture.limits.BindingTTL = 60 * time.Second
			}
			endpoint := netip.MustParseAddrPort("203.0.113.20:6000")
			nonce := bytes16(0x91)
			fixture.random.reset(filled(0x92, 16), filled(0x93, 32), filled(0x94, 16))
			challenge, reason := fixture.store.BeginChallenge(fixture.challengeRequest(0, nonce, endpoint))
			if reason != RejectNone {
				t.Fatalf("BeginChallenge(): %q", reason)
			}
			bound, reason := fixture.store.Authenticate(fixture.authRequest(0, challenge, nonce, endpoint))
			if reason != RejectNone || bound.ExpiresUnixMS != testWall.Add(tt.wantExpiry).UnixMilli() {
				t.Fatalf("Authenticate() = (%#v, %q), want expiry %v", bound, reason, tt.wantExpiry)
			}
		})
	}
}

func TestAuthenticateBindingRandomFailureRollsBack(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 3)
	firstEndpoint := netip.MustParseAddrPort("203.0.113.30:6000")
	firstNonce := bytes16(0xa1)
	fixture.random.reset(filled(0xa2, 16), filled(0xa3, 32), filled(0xa4, 16))
	firstChallenge, _ := fixture.store.BeginChallenge(fixture.challengeRequest(0, firstNonce, firstEndpoint))
	firstBound, reason := fixture.store.Authenticate(fixture.authRequest(0, firstChallenge, firstNonce, firstEndpoint))
	if reason != RejectNone {
		t.Fatalf("first Authenticate(): %q", reason)
	}

	secondEndpoint := netip.MustParseAddrPort("203.0.113.31:6000")
	secondNonce := bytes16(0xb1)
	fixture.random.reset(filled(0xb2, 16), filled(0xb3, 32))
	secondChallenge, _ := fixture.store.BeginChallenge(fixture.challengeRequest(1, secondNonce, secondEndpoint))
	chunks := make([][]byte, 0, 9)
	for range 8 {
		chunks = append(chunks, filled(0xa4, 16))
	}
	chunks = append(chunks, filled(0xb4, 16))
	fixture.random.reset(chunks...)
	secondBound, reason := fixture.store.Authenticate(fixture.authRequest(1, secondChallenge, secondNonce, secondEndpoint))
	if reason != RejectNone || secondBound.BindingID != bytes16(0xb4) {
		t.Fatalf("ninth-draw binding collision success = (%#v, %q)", secondBound, reason)
	}
	assertReads(t, fixture.random.calls, 16, 16, 16, 16, 16, 16, 16, 16, 16)

	thirdEndpoint := netip.MustParseAddrPort("203.0.113.32:6000")
	thirdNonce := bytes16(0xc1)
	fixture.random.reset(filled(0xc2, 16), filled(0xc3, 32))
	thirdChallenge, _ := fixture.store.BeginChallenge(fixture.challengeRequest(2, thirdNonce, thirdEndpoint))
	chunks = make([][]byte, 9)
	for index := range chunks {
		chunks[index] = filled(0xa4, 16)
	}
	fixture.random.reset(chunks...)
	if _, reason := fixture.store.Authenticate(fixture.authRequest(2, thirdChallenge, thirdNonce, thirdEndpoint)); reason != RejectFatalRandom {
		t.Fatalf("binding collision exhaustion reason = %q", reason)
	}
	if fixture.store.bindingsByID[firstBound.BindingID] != fixture.grant(0) ||
		fixture.store.bindingsByID[secondBound.BindingID] != fixture.grant(1) || fixture.grant(2).binding != nil || fixture.grant(2).pending == nil {
		t.Fatal("binding collision failure changed existing or pending state")
	}

	fixture.random.reset(filled(0xb4, 15))
	if _, reason := fixture.store.Authenticate(fixture.authRequest(2, thirdChallenge, thirdNonce, thirdEndpoint)); reason != RejectFatalRandom {
		t.Fatalf("short binding ID reason = %q", reason)
	}
	if fixture.grant(2).binding != nil || fixture.grant(2).pending == nil {
		t.Fatal("short binding read partially committed state")
	}
}

func TestRebindRotatesBindingAtomicallyAndKeepsRecentUntilReplacement(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
	oldEndpoint := netip.MustParseAddrPort("203.0.113.40:6000")
	oldNonce := bytes16(0xc1)
	fixture.random.reset(filled(0xc2, 16), filled(0xc3, 32), filled(0xc4, 16))
	oldChallenge, _ := fixture.store.BeginChallenge(fixture.challengeRequest(0, oldNonce, oldEndpoint))
	oldAuth := fixture.authRequest(0, oldChallenge, oldNonce, oldEndpoint)
	oldBound, reason := fixture.store.Authenticate(oldAuth)
	if reason != RejectNone {
		t.Fatalf("old Authenticate(): %q", reason)
	}
	oldBinding := fixture.grant(0).binding
	oldBinding.replay.accept(99)
	oldID, oldKey, oldEndpointValue := oldBinding.id, oldBinding.key, oldBinding.endpoint
	oldDeadline, oldGeneration, oldReplay := oldBinding.deadline, oldBinding.generation, oldBinding.replay
	if snapshot, err := fixture.store.GetRoom("room"); err != nil || snapshot.Participants[0].BindingState != BindingStateBound {
		t.Fatalf("bound snapshot = (%#v, %v)", snapshot, err)
	}

	newEndpoint := netip.MustParseAddrPort("203.0.113.41:6000")
	newNonce := bytes16(0xd1)
	fixture.random.reset(filled(0xd2, 16), filled(0xd3, 32), filled(0xd4, 16))
	newChallenge, reason := fixture.store.BeginChallenge(fixture.challengeRequest(0, newNonce, newEndpoint))
	if reason != RejectNone {
		t.Fatalf("rebind BeginChallenge(): %q", reason)
	}
	if fixture.grant(0).binding != oldBinding || fixture.store.bindingsByID[oldBound.BindingID] != fixture.grant(0) ||
		fixture.grant(0).recent == nil || fixture.grant(0).bindingState != BindingStateRebindPending {
		t.Fatal("pending rebind invalidated the old current binding/recent completion")
	}
	if snapshot, err := fixture.store.GetRoom("room"); err != nil || snapshot.Participants[0].BindingState != BindingStateRebindPending {
		t.Fatalf("rebind-pending snapshot = (%#v, %v)", snapshot, err)
	}
	if replay, reason := fixture.store.Authenticate(oldAuth); reason != RejectNone || replay != oldBound {
		t.Fatalf("old duplicate AUTH during pending rebind = (%#v, %q)", replay, reason)
	}
	pending := fixture.grant(0).pending
	assertOldBinding := func(label string) {
		t.Helper()
		grant := fixture.grant(0)
		if grant.binding != oldBinding || fixture.store.bindingsByID[oldID] != grant || oldBinding.id != oldID ||
			oldBinding.key != oldKey || oldBinding.endpoint != oldEndpointValue || oldBinding.deadline != oldDeadline ||
			oldBinding.generation != oldGeneration || oldBinding.replay != oldReplay || grant.state != GrantStateBound ||
			grant.bindingState != BindingStateRebindPending || grant.pending != pending {
			t.Fatalf("%s changed the old binding or pending rebind", label)
		}
	}
	collisions := make([][]byte, 9)
	for index := range collisions {
		collisions[index] = append([]byte(nil), oldID[:]...)
	}
	fixture.random.reset(collisions...)
	if _, reason := fixture.store.Authenticate(fixture.authRequest(0, newChallenge, newNonce, newEndpoint)); reason != RejectFatalRandom {
		t.Fatalf("rebind collision exhaustion reason = %q", reason)
	}
	assertOldBinding("collision exhaustion")
	fixture.random.reset(filled(0xee, 15))
	if _, reason := fixture.store.Authenticate(fixture.authRequest(0, newChallenge, newNonce, newEndpoint)); reason != RejectFatalRandom {
		t.Fatalf("rebind short read reason = %q", reason)
	}
	assertOldBinding("short read")

	fixture.random.reset(filled(0xd4, 16))
	newBound, reason := fixture.store.Authenticate(fixture.authRequest(0, newChallenge, newNonce, newEndpoint))
	if reason != RejectNone {
		t.Fatalf("new Authenticate(): %q", reason)
	}
	current := fixture.grant(0).binding
	if current == oldBinding || current.id != newBound.BindingID || current.id == oldID || current.endpoint != newEndpoint ||
		current.endpoint == oldEndpointValue || current.key == oldKey || current.replay != (replayWindow{}) ||
		current.generation != oldGeneration+1 {
		t.Fatalf("new binding did not rotate all binding-scoped state: old=%#v new=%#v", oldBinding, current)
	}
	if _, exists := fixture.store.bindingsByID[oldID]; exists || oldBinding.id != (protocol.Bytes16{}) ||
		oldBinding.key != (protocol.Bytes32{}) || oldBinding.endpoint.IsValid() || oldBinding.replay != (replayWindow{}) {
		t.Fatal("old binding remained usable or retained secret/index state")
	}
	if _, reason := fixture.store.Authenticate(oldAuth); reason != RejectAuthFailed {
		t.Fatalf("old duplicate AUTH after newer completion reason = %q", reason)
	}
	if fixture.grant(0).recent == nil || fixture.grant(0).recent.result != newBound || fixture.grant(0).bindingState != BindingStateBound {
		t.Fatal("new completion was not current")
	}
	if snapshot, err := fixture.store.GetRoom("room"); err != nil || snapshot.Participants[0].BindingState != BindingStateBound {
		t.Fatalf("rebound snapshot = (%#v, %v)", snapshot, err)
	}
	assertStoreInvariants(t, fixture.store)
}

func TestGetRoomProjectsBindingAndPendingDeadlinesBeforeSweep(t *testing.T) {
	t.Run("binding", func(t *testing.T) {
		fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
		endpoint := netip.MustParseAddrPort("203.0.113.44:6000")
		nonce := bytes16(0xdd)
		fixture.random.reset(filled(0xde, 16), filled(0xdf, 32), filled(0xe0, 16))
		challenge, _ := fixture.store.BeginChallenge(fixture.challengeRequest(0, nonce, endpoint))
		bound, reason := fixture.store.Authenticate(fixture.authRequest(0, challenge, nonce, endpoint))
		if reason != RejectNone {
			t.Fatalf("Authenticate(): %q", reason)
		}
		binding := fixture.grant(0).binding
		for _, tt := range []struct {
			name string
			at   time.Duration
			want BindingState
		}{
			{name: "before", at: 60*time.Second - time.Nanosecond, want: BindingStateBound},
			{name: "exact", at: 60 * time.Second, want: BindingStateExpired},
			{name: "after", at: 60*time.Second + time.Nanosecond, want: BindingStateExpired},
		} {
			fixture.clock.reading = ClockReading{Wall: testWall.Add(tt.at), Mono: tt.at}
			snapshot, err := fixture.store.GetRoom("room")
			if err != nil || snapshot.Participants[0].BindingState != tt.want {
				t.Fatalf("GetRoom(%s) = (%#v, %v), want binding %q", tt.name, snapshot, err, tt.want)
			}
			wantGrant := GrantStateBound
			if tt.want == BindingStateExpired {
				wantGrant = GrantStateIssued
			}
			if snapshot.Participants[0].GrantState != wantGrant {
				t.Fatalf("GetRoom(%s) grant = %q, want %q", tt.name, snapshot.Participants[0].GrantState, wantGrant)
			}
		}
		if fixture.grant(0).binding != binding || fixture.store.bindingsByID[bound.BindingID] != fixture.grant(0) {
			t.Fatal("deadline-aware snapshot mutated store under read access")
		}
		fixture.store.Expire()
		if fixture.grant(0).binding != nil || fixture.store.bindingsByID[bound.BindingID] != nil || binding.key != (protocol.Bytes32{}) {
			t.Fatal("Expire did not clear the projected-expired binding")
		}
	})

	t.Run("rebind pending", func(t *testing.T) {
		fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
		oldEndpoint := netip.MustParseAddrPort("203.0.113.45:6000")
		newEndpoint := netip.MustParseAddrPort("203.0.113.46:6000")
		oldNonce, newNonce := bytes16(0xe1), bytes16(0xe2)
		fixture.random.reset(filled(0xe3, 16), filled(0xe4, 32), filled(0xe5, 16))
		oldChallenge, _ := fixture.store.BeginChallenge(fixture.challengeRequest(0, oldNonce, oldEndpoint))
		if _, reason := fixture.store.Authenticate(fixture.authRequest(0, oldChallenge, oldNonce, oldEndpoint)); reason != RejectNone {
			t.Fatalf("Authenticate(): %q", reason)
		}
		fixture.random.reset(filled(0xe6, 16), filled(0xe7, 32))
		newChallenge, reason := fixture.store.BeginChallenge(fixture.challengeRequest(0, newNonce, newEndpoint))
		if reason != RejectNone {
			t.Fatalf("rebind BeginChallenge(): %q", reason)
		}
		for _, tt := range []struct {
			name string
			at   time.Duration
			want BindingState
		}{
			{name: "before", at: 3*time.Second - time.Nanosecond, want: BindingStateRebindPending},
			{name: "exact", at: 3 * time.Second, want: BindingStateBound},
			{name: "after", at: 3*time.Second + time.Nanosecond, want: BindingStateBound},
		} {
			fixture.clock.reading = ClockReading{Wall: testWall.Add(tt.at), Mono: tt.at}
			snapshot, err := fixture.store.GetRoom("room")
			if err != nil || snapshot.Participants[0].BindingState != tt.want || snapshot.Participants[0].GrantState != GrantStateBound {
				t.Fatalf("GetRoom(%s) = (%#v, %v), want bound/%q", tt.name, snapshot, err, tt.want)
			}
		}
		if fixture.grant(0).pending == nil || fixture.store.candidatesByID[newChallenge.CandidateID] != fixture.grant(0) {
			t.Fatal("deadline-aware snapshot mutated pending state")
		}
		fixture.store.Expire()
		if fixture.grant(0).pending != nil || fixture.store.candidatesByID[newChallenge.CandidateID] != nil ||
			fixture.grant(0).binding == nil || fixture.grant(0).bindingState != BindingStateBound {
			t.Fatal("Expire did not clear projected-expired pending state")
		}
	})
}

func TestIdempotentCreateRoomProjectsBindingDeadlineBeforeSweep(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
	endpoint := netip.MustParseAddrPort("203.0.113.47:6000")
	nonce := bytes16(0xe8)
	fixture.random.reset(filled(0xe9, 16), filled(0xea, 32), filled(0xeb, 16))
	challenge, _ := fixture.store.BeginChallenge(fixture.challengeRequest(0, nonce, endpoint))
	if _, reason := fixture.store.Authenticate(fixture.authRequest(0, challenge, nonce, endpoint)); reason != RejectNone {
		t.Fatalf("Authenticate(): %q", reason)
	}
	definition := validDefinition(testWall, 1)
	for _, tt := range []struct {
		name string
		at   time.Duration
		want GrantState
	}{
		{name: "before", at: 60*time.Second - time.Nanosecond, want: GrantStateBound},
		{name: "exact", at: 60 * time.Second, want: GrantStateIssued},
		{name: "after", at: 60*time.Second + time.Nanosecond, want: GrantStateIssued},
	} {
		fixture.clock.reading = ClockReading{Wall: testWall.Add(tt.at), Mono: tt.at}
		allocation, created, err := fixture.store.CreateRoom("room", definition)
		if err != nil || created || allocation.Grants[0].State != tt.want || allocation.Grants[0].GrantSecret == nil {
			t.Fatalf("CreateRoom(%s) = (%#v, %t, %v), want state %q with live secret", tt.name, allocation, created, err, tt.want)
		}
	}
}

func TestExpiredBindingRemainsExpiredWithLivePendingRebind(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
	fixture.store.limits.BindingTTL = 2 * time.Second
	oldEndpoint := netip.MustParseAddrPort("203.0.113.48:6000")
	newEndpoint := netip.MustParseAddrPort("203.0.113.49:6000")
	oldNonce, newNonce := bytes16(0xec), bytes16(0xed)
	fixture.random.reset(filled(0xee, 16), filled(0xef, 32), filled(0xf0, 16))
	oldChallenge, _ := fixture.store.BeginChallenge(fixture.challengeRequest(0, oldNonce, oldEndpoint))
	oldBound, reason := fixture.store.Authenticate(fixture.authRequest(0, oldChallenge, oldNonce, oldEndpoint))
	if reason != RejectNone {
		t.Fatalf("old Authenticate(): %q", reason)
	}
	oldBinding := fixture.grant(0).binding

	fixture.clock.reading = ClockReading{Wall: testWall.Add(time.Second), Mono: time.Second}
	fixture.random.reset(filled(0xf1, 16), filled(0xf2, 32))
	newChallenge, reason := fixture.store.BeginChallenge(fixture.challengeRequest(0, newNonce, newEndpoint))
	if reason != RejectNone {
		t.Fatalf("rebind BeginChallenge(): %q", reason)
	}
	pending := fixture.grant(0).pending
	fixture.clock.reading = ClockReading{Wall: testWall.Add(2 * time.Second), Mono: 2 * time.Second}
	before, err := fixture.store.GetRoom("room")
	if err != nil || before.Participants[0].GrantState != GrantStateIssued || before.Participants[0].BindingState != BindingStateExpired {
		t.Fatalf("pre-sweep snapshot = (%#v, %v), want issued/expired", before, err)
	}
	fixture.store.Expire()
	after, err := fixture.store.GetRoom("room")
	if err != nil || after.Participants[0].GrantState != GrantStateIssued || after.Participants[0].BindingState != BindingStateExpired {
		t.Fatalf("post-sweep snapshot = (%#v, %v), want issued/expired", after, err)
	}
	if fixture.grant(0).pending != pending || fixture.store.candidatesByID[newChallenge.CandidateID] != fixture.grant(0) ||
		fixture.grant(0).binding != nil || fixture.store.bindingsByID[oldBound.BindingID] != nil || oldBinding.key != (protocol.Bytes32{}) {
		t.Fatal("binding expiry removed the live pending rebind or retained old authority")
	}

	fixture.random.reset(filled(0xf3, 16))
	if _, reason := fixture.store.Authenticate(fixture.authRequest(0, newChallenge, newNonce, newEndpoint)); reason != RejectNone {
		t.Fatalf("pending Authenticate after old binding expiry: %q", reason)
	}
	rebound, err := fixture.store.GetRoom("room")
	if err != nil || rebound.Participants[0].GrantState != GrantStateBound || rebound.Participants[0].BindingState != BindingStateBound {
		t.Fatalf("rebound snapshot = (%#v, %v), want bound/bound", rebound, err)
	}
}

func TestRecentCompletionExpiresExactlyAtChallengeTTL(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
	endpoint := netip.MustParseAddrPort("203.0.113.42:6000")
	nonce := bytes16(0xd5)
	fixture.random.reset(filled(0xd6, 16), filled(0xd7, 32), filled(0xd8, 16))
	challenge, _ := fixture.store.BeginChallenge(fixture.challengeRequest(0, nonce, endpoint))
	auth := fixture.authRequest(0, challenge, nonce, endpoint)
	bound, reason := fixture.store.Authenticate(auth)
	if reason != RejectNone {
		t.Fatalf("Authenticate(): %q", reason)
	}
	fixture.clock.reading = ClockReading{Wall: testWall.Add(3*time.Second - time.Nanosecond), Mono: 3*time.Second - time.Nanosecond}
	if duplicate, reason := fixture.store.Authenticate(auth); reason != RejectNone || duplicate != bound {
		t.Fatalf("duplicate before recent deadline = (%#v, %q)", duplicate, reason)
	}
	fixture.clock.reading = ClockReading{Wall: testWall.Add(3 * time.Second), Mono: 3 * time.Second}
	if _, reason := fixture.store.Authenticate(auth); reason != RejectExpired {
		t.Fatalf("duplicate at recent deadline reason = %q", reason)
	}
	if fixture.grant(0).recent != nil || fixture.store.candidatesByID[challenge.CandidateID] != nil || fixture.grant(0).binding == nil {
		t.Fatal("recent expiry removed the current binding or retained candidate state")
	}
}

func TestDuplicateAuthAtBindingDeadlineClearsCurrentAuthority(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
	fixture.store.limits.BindingTTL = 2 * time.Second
	endpoint := netip.MustParseAddrPort("203.0.113.43:6000")
	nonce := bytes16(0xd9)
	fixture.random.reset(filled(0xda, 16), filled(0xdb, 32), filled(0xdc, 16))
	challenge, _ := fixture.store.BeginChallenge(fixture.challengeRequest(0, nonce, endpoint))
	auth := fixture.authRequest(0, challenge, nonce, endpoint)
	bound, reason := fixture.store.Authenticate(auth)
	if reason != RejectNone {
		t.Fatalf("Authenticate(): %q", reason)
	}
	binding := fixture.grant(0).binding
	fixture.clock.reading = ClockReading{Wall: testWall.Add(2 * time.Second), Mono: 2 * time.Second}
	if _, reason := fixture.store.Authenticate(auth); reason != RejectExpired {
		t.Fatalf("duplicate at binding deadline reason = %q", reason)
	}
	if fixture.grant(0).binding != nil || fixture.grant(0).recent != nil || fixture.store.bindingsByID[bound.BindingID] != nil ||
		binding.key != (protocol.Bytes32{}) {
		t.Fatal("duplicate AUTH at binding deadline retained current authority")
	}
}

func TestPreauthSourceKeysAndAtomicAdmission(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
	v4a := netip.MustParseAddrPort("192.0.2.1:1000")
	v4b := netip.MustParseAddrPort("192.0.2.1:2000")
	v6a := netip.MustParseAddrPort("[2001:db8:1:2::1]:1000")
	v6b := netip.MustParseAddrPort("[2001:db8:1:2:ffff::2]:2000")
	if sourceKey(v4a) != sourceKey(v4b) {
		t.Fatal("IPv4 source key included the port")
	}
	if sourceKey(v6a) != sourceKey(v6b) {
		t.Fatal("IPv6 source key was not a /64 with port excluded")
	}
	if sourceKey(v4a).Bits() != 32 || sourceKey(v6a).Bits() != 64 {
		t.Fatalf("source key widths = %d/%d, want 32/64", sourceKey(v4a).Bits(), sourceKey(v6a).Bits())
	}

	for range fixture.limits.PreauthSourcePacketBurst {
		if reason := fixture.store.AdmitPreauth(PreauthRequest{Endpoint: v4a, InputBytes: 1}); reason != RejectNone {
			t.Fatalf("burst admission reason = %q", reason)
		}
	}
	fixture.clock.reading = ClockReading{Wall: testWall.Add(time.Nanosecond), Mono: time.Nanosecond}
	now := limiterTime(fixture.clock.reading.Mono)
	globalBefore := fixture.store.preauthGlobalPackets.TokensAt(now)
	record := fixture.store.preauthSources[sourceKey(v4a)]
	if reason := fixture.store.AdmitPreauth(PreauthRequest{Endpoint: v4a, InputBytes: 1}); reason != RejectRateLimited {
		t.Fatalf("one-over source burst reason = %q", reason)
	}
	if got := fixture.store.preauthGlobalPackets.TokensAt(now); got != globalBefore {
		t.Fatalf("source rejection partially consumed global tokens: %v -> %v", globalBefore, got)
	}
	if fixture.store.preauthSources[sourceKey(v4a)] != record || record.lastObserved != fixture.clock.reading.Mono {
		t.Fatal("rate-limited existing source was recreated or not refreshed")
	}
}

func TestPreauthGlobalBoundaryDoesNotPartiallyCommitSource(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
	for index := range fixture.limits.PreauthGlobalPacketBurst {
		endpoint := netip.AddrPortFrom(netip.AddrFrom4([4]byte{172, 16, byte(index >> 8), byte(index)}), 1000)
		if reason := fixture.store.AdmitPreauth(PreauthRequest{Endpoint: endpoint, InputBytes: 1}); reason != RejectNone {
			t.Fatalf("global burst packet %d reason = %q", index, reason)
		}
	}
	newEndpoint := netip.MustParseAddrPort("172.17.0.1:1000")
	if reason := fixture.store.AdmitPreauth(PreauthRequest{Endpoint: newEndpoint, InputBytes: 1}); reason != RejectRateLimited {
		t.Fatalf("one-over global burst reason = %q", reason)
	}
	if fixture.store.preauthSources[sourceKey(newEndpoint)] != nil || len(fixture.store.preauthSources) != fixture.limits.PreauthGlobalPacketBurst {
		t.Fatal("global rejection partially committed a new source record")
	}
}

func TestPreauthByteBoundariesAreAtomicAcrossAllFourLimiters(t *testing.T) {
	t.Run("source byte", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			finalCost int
			want      RejectReason
		}{
			{name: "equality", finalCost: 1_041, want: RejectNone},
			{name: "one over", finalCost: 1_042, want: RejectRateLimited},
		} {
			t.Run(tt.name, func(t *testing.T) {
				fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
				endpoint := netip.MustParseAddrPort("192.0.2.80:8000")
				for index := range 159 {
					if reason := fixture.store.AdmitPreauth(PreauthRequest{Endpoint: endpoint, InputBytes: 1_201}); reason != RejectNone {
						t.Fatalf("setup packet %d reason = %q", index, reason)
					}
				}
				now := limiterTime(fixture.clock.reading.Mono)
				source := fixture.store.preauthSources[sourceKey(endpoint)]
				before := preauthBalancesAt(fixture.store, source, now)
				if reason := fixture.store.AdmitPreauth(PreauthRequest{Endpoint: endpoint, InputBytes: tt.finalCost}); reason != tt.want {
					t.Fatalf("final admission reason = %q, want %q", reason, tt.want)
				}
				after := preauthBalancesAt(fixture.store, source, now)
				if tt.want == RejectRateLimited {
					if after != before {
						t.Fatalf("source-byte block partially consumed: before=%#v after=%#v", before, after)
					}
				} else if after != (preauthBalances{before.sourcePackets - 1, before.sourceBytes - float64(tt.finalCost), before.globalPackets - 1, before.globalBytes - float64(tt.finalCost)}) {
					t.Fatalf("source-byte equality balances: before=%#v after=%#v", before, after)
				}
			})
		}
	})

	t.Run("global byte", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			finalCost int
			want      RejectReason
		}{
			{name: "equality", finalCost: 1_122, want: RejectNone},
			{name: "one over", finalCost: 1_123, want: RejectRateLimited},
		} {
			t.Run(tt.name, func(t *testing.T) {
				fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
				var target netip.AddrPort
				for index := range 1_278 {
					endpoint := netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, 20, byte(index >> 8), byte(index)}), 8000)
					if index == 0 {
						target = endpoint
					}
					if reason := fixture.store.AdmitPreauth(PreauthRequest{Endpoint: endpoint, InputBytes: 1_201}); reason != RejectNone {
						t.Fatalf("setup packet %d reason = %q", index, reason)
					}
				}
				now := limiterTime(fixture.clock.reading.Mono)
				source := fixture.store.preauthSources[sourceKey(target)]
				before := preauthBalancesAt(fixture.store, source, now)
				if reason := fixture.store.AdmitPreauth(PreauthRequest{Endpoint: target, InputBytes: tt.finalCost}); reason != tt.want {
					t.Fatalf("final admission reason = %q, want %q", reason, tt.want)
				}
				after := preauthBalancesAt(fixture.store, source, now)
				if tt.want == RejectRateLimited {
					if after != before {
						t.Fatalf("global-byte block partially consumed: before=%#v after=%#v", before, after)
					}
				} else if after != (preauthBalances{before.sourcePackets - 1, before.sourceBytes - float64(tt.finalCost), before.globalPackets - 1, before.globalBytes - float64(tt.finalCost)}) {
					t.Fatalf("global-byte equality balances: before=%#v after=%#v", before, after)
				}
			})
		}
	})
}

func TestHandshakeDeadlinesSaturateNearMaxMonotonicTime(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
	maxDeadline := time.Duration(math.MaxInt64)
	now := maxDeadline - 2*time.Second
	fixture.store.roomsByID["room"].monoDeadline = maxDeadline
	fixture.grant(0).monoDeadline = maxDeadline
	fixture.clock.reading = ClockReading{Wall: testWall, Mono: now}
	endpoint := netip.MustParseAddrPort("192.0.2.90:9000")
	nonce := bytes16(0xf9)
	fixture.random.reset(filled(0xfa, 16), filled(0xfb, 32), filled(0xfc, 16))
	challenge, reason := fixture.store.BeginChallenge(fixture.challengeRequest(0, nonce, endpoint))
	if reason != RejectNone || fixture.grant(0).pending == nil || fixture.grant(0).pending.deadline != maxDeadline ||
		challenge.ExpiresUnixMS != testWall.Add(2*time.Second).UnixMilli() {
		t.Fatalf("near-max challenge = (%#v, %q, %#v)", challenge, reason, fixture.grant(0).pending)
	}
	bound, reason := fixture.store.Authenticate(fixture.authRequest(0, challenge, nonce, endpoint))
	if reason != RejectNone || fixture.grant(0).binding == nil || fixture.grant(0).binding.deadline != maxDeadline ||
		fixture.grant(0).recent == nil || fixture.grant(0).recent.deadline != maxDeadline ||
		bound.ExpiresUnixMS != testWall.Add(2*time.Second).UnixMilli() {
		t.Fatalf("near-max bound = (%#v, %q, binding=%#v recent=%#v)", bound, reason, fixture.grant(0).binding, fixture.grant(0).recent)
	}
}

func TestPreauthIdleBoundaryLazilyReplacesOnlyAtExactDeadline(t *testing.T) {
	for _, tt := range []struct {
		name     string
		at       time.Duration
		wantSame bool
	}{
		{name: "before", at: 60*time.Second - time.Nanosecond, wantSame: true},
		{name: "exact", at: 60 * time.Second, wantSame: false},
		{name: "after", at: 60*time.Second + time.Nanosecond, wantSame: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
			endpoint := netip.MustParseAddrPort("192.0.2.50:7000")
			if reason := fixture.store.AdmitPreauth(PreauthRequest{Endpoint: endpoint, InputBytes: 1}); reason != RejectNone {
				t.Fatalf("initial admission = %q", reason)
			}
			old := fixture.store.preauthSources[sourceKey(endpoint)]
			fixture.clock.reading = ClockReading{Wall: testWall.Add(tt.at), Mono: tt.at}
			if reason := fixture.store.AdmitPreauth(PreauthRequest{Endpoint: endpoint, InputBytes: 1}); reason != RejectNone {
				t.Fatalf("boundary admission = %q", reason)
			}
			current := fixture.store.preauthSources[sourceKey(endpoint)]
			if (current == old) != tt.wantSame || current.lastObserved != tt.at {
				t.Fatalf("source identity/observation = same:%t last:%v", current == old, current.lastObserved)
			}
		})
	}
}

func TestPreauthRejectedHelloRefreshesExistingSource(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
	endpoint := netip.MustParseAddrPort("192.0.2.60:7000")
	request := fixture.challengeRequest(0, bytes16(0xe1), endpoint)
	request.GrantID = bytes16(0xff)
	if _, reason := fixture.store.BeginChallenge(request); reason != RejectUnknownGrant {
		t.Fatalf("unknown grant reason = %q", reason)
	}
	record := fixture.store.preauthSources[sourceKey(endpoint)]
	fixture.clock.reading = ClockReading{Wall: testWall.Add(time.Second), Mono: time.Second}
	if _, reason := fixture.store.BeginChallenge(request); reason != RejectUnknownGrant {
		t.Fatalf("second unknown grant reason = %q", reason)
	}
	if fixture.store.preauthSources[sourceKey(endpoint)] != record || record.lastObserved != time.Second {
		t.Fatal("rejected HELLO did not refresh the existing record")
	}
}

func TestPreauthFullTableUsesProcessOnlyAndCreatesNoRecord(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
	for index := range HardMaxPreauthSources {
		endpoint := netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, byte(index >> 16), byte(index >> 8), byte(index)}), 1000)
		if reason := fixture.store.AdmitPreauth(PreauthRequest{Endpoint: endpoint, InputBytes: 1}); reason != RejectNone {
			t.Fatalf("fill source %d reason = %q", index, reason)
		}
		if index >= fixture.limits.PreauthGlobalPacketBurst-1 {
			fixture.clock.reading.Mono += time.Second / time.Duration(fixture.limits.PreauthGlobalPacketRate)
			fixture.clock.reading.Wall = testWall.Add(fixture.clock.reading.Mono)
		}
	}
	if len(fixture.store.preauthSources) != HardMaxPreauthSources {
		t.Fatalf("source table size = %d", len(fixture.store.preauthSources))
	}
	fixture.clock.reading.Mono += time.Second
	fixture.clock.reading.Wall = testWall.Add(fixture.clock.reading.Mono)
	newEndpoint := netip.MustParseAddrPort("11.0.0.1:1000")
	now := limiterTime(fixture.clock.reading.Mono)
	before := fixture.store.preauthGlobalPackets.TokensAt(now)
	if reason := fixture.store.AdmitPreauth(PreauthRequest{Endpoint: newEndpoint, InputBytes: 1}); reason != RejectRateLimited {
		t.Fatalf("full-table new source reason = %q", reason)
	}
	if len(fixture.store.preauthSources) != HardMaxPreauthSources || fixture.store.preauthSources[sourceKey(newEndpoint)] != nil {
		t.Fatal("full-table new source created state")
	}
	if got := fixture.store.preauthGlobalPackets.TokensAt(now); got != before-1 {
		t.Fatalf("full-table process-only tokens = %v, want %v", got, before-1)
	}
	existing := netip.MustParseAddrPort("10.0.0.1:2000")
	record := fixture.store.preauthSources[sourceKey(existing)]
	if reason := fixture.store.AdmitPreauth(PreauthRequest{Endpoint: existing, InputBytes: 1}); reason != RejectNone {
		t.Fatalf("full-table existing source reason = %q", reason)
	}
	if fixture.store.preauthSources[sourceKey(existing)] != record || len(fixture.store.preauthSources) != HardMaxPreauthSources {
		t.Fatal("full-table existing source was replaced")
	}
}

func TestExpireRemovesIdleSourcesAndBindingAtExactDeadlines(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
	source := netip.MustParseAddrPort("192.0.2.71:7000")
	if reason := fixture.store.AdmitPreauth(PreauthRequest{Endpoint: source, InputBytes: 1}); reason != RejectNone {
		t.Fatalf("AdmitPreauth(): %q", reason)
	}
	endpoint := netip.MustParseAddrPort("192.0.2.72:7000")
	nonce := bytes16(0xf5)
	fixture.random.reset(filled(0xf6, 16), filled(0xf7, 32), filled(0xf8, 16))
	challenge, _ := fixture.store.BeginChallenge(fixture.challengeRequest(0, nonce, endpoint))
	bound, reason := fixture.store.Authenticate(fixture.authRequest(0, challenge, nonce, endpoint))
	if reason != RejectNone {
		t.Fatalf("Authenticate(): %q", reason)
	}
	binding := fixture.grant(0).binding
	fixture.clock.reading = ClockReading{Wall: testWall.Add(60*time.Second - time.Nanosecond), Mono: 60*time.Second - time.Nanosecond}
	fixture.store.Expire()
	if fixture.store.preauthSources[sourceKey(source)] == nil || fixture.store.bindingsByID[bound.BindingID] == nil {
		t.Fatal("relay state expired before its exact deadline")
	}
	fixture.clock.reading = ClockReading{Wall: testWall.Add(60 * time.Second), Mono: 60 * time.Second}
	fixture.store.Expire()
	if fixture.store.preauthSources[sourceKey(source)] != nil || fixture.store.bindingsByID[bound.BindingID] != nil ||
		fixture.grant(0).binding != nil || binding.key != (protocol.Bytes32{}) {
		t.Fatal("relay state survived its exact deadline")
	}
	if snapshot, err := fixture.store.GetRoom("room"); err != nil || snapshot.Participants[0].GrantState != GrantStateIssued ||
		snapshot.Participants[0].BindingState != BindingStateExpired {
		t.Fatalf("binding-expired snapshot = (%#v, %v)", snapshot, err)
	}
	assertStoreInvariants(t, fixture.store)
}

func TestExpireAndEndRoomClearRelaySecretsAndIndexes(t *testing.T) {
	fixture := newHandshakeFixture(t, time.Hour, 30*time.Minute, 1)
	endpoint := netip.MustParseAddrPort("192.0.2.70:7000")
	nonce := bytes16(0xf1)
	fixture.random.reset(filled(0xf2, 16), filled(0xf3, 32), filled(0xf4, 16))
	challenge, _ := fixture.store.BeginChallenge(fixture.challengeRequest(0, nonce, endpoint))
	bound, reason := fixture.store.Authenticate(fixture.authRequest(0, challenge, nonce, endpoint))
	if reason != RejectNone {
		t.Fatalf("Authenticate(): %q", reason)
	}
	binding := fixture.grant(0).binding
	recent := fixture.grant(0).recent
	if err := fixture.store.EndRoom("room"); err != nil {
		t.Fatalf("EndRoom(): %v", err)
	}
	if len(fixture.store.candidatesByID) != 0 || len(fixture.store.bindingsByID) != 0 ||
		binding.key != (protocol.Bytes32{}) || binding.id != (protocol.Bytes16{}) || binding.endpoint.IsValid() ||
		recent.candidateID != (protocol.Bytes16{}) || recent.clientNonce != (protocol.Bytes16{}) || recent.serverNonce != (protocol.Bytes32{}) {
		t.Fatalf("terminal cleanup retained relay material: bound=%x binding=%#v recent=%#v", bound.BindingID, binding, recent)
	}
	assertStoreInvariants(t, fixture.store)
}

type preauthBalances struct {
	sourcePackets, sourceBytes, globalPackets, globalBytes float64
}

func preauthBalancesAt(store *Store, source *preauthSource, now time.Time) preauthBalances {
	return preauthBalances{
		sourcePackets: source.packets.TokensAt(now),
		sourceBytes:   source.bytes.TokensAt(now),
		globalPackets: store.preauthGlobalPackets.TokensAt(now),
		globalBytes:   store.preauthGlobalBytes.TokensAt(now),
	}
}

type handshakeFixture struct {
	store      *Store
	clock      *manualClock
	random     *scriptedReader
	allocation Allocation
	secrets    []protocol.Bytes32
	limits     Limits
}

func newHandshakeFixture(t *testing.T, roomTTL, grantTTL time.Duration, participants int) *handshakeFixture {
	t.Helper()
	clock := &manualClock{reading: ClockReading{Wall: testWall, Mono: 0}}
	random := newScriptedReader()
	chunks := make([][]byte, 0, participants*2)
	secrets := make([]protocol.Bytes32, participants)
	for index := range participants {
		chunks = append(chunks, filled(byte(0x10+index), 16), filled(byte(0x20+index), 32))
		secrets[index] = bytes32(byte(0x20 + index))
	}
	random.reset(chunks...)
	limits := DefaultLimits()
	store := newTestStore(t, limits, clock, random)
	definition := validDefinition(testWall, participants)
	definition.ExpiresAt = testWall.Add(roomTTL)
	for index := range definition.Participants {
		definition.Participants[index].GrantExpiresAt = testWall.Add(grantTTL)
	}
	allocation, _, err := store.CreateRoom("room", definition)
	if err != nil {
		t.Fatalf("CreateRoom(): %v", err)
	}
	return &handshakeFixture{store: store, clock: clock, random: random, allocation: allocation, secrets: secrets, limits: limits}
}

func (fixture *handshakeFixture) grant(index int) *grantRecord {
	return fixture.store.roomsByID["room"].grants[index]
}

func (fixture *handshakeFixture) grantID(index int) protocol.Bytes16 {
	return fixture.allocation.Grants[index].GrantID
}

func (fixture *handshakeFixture) secret(index int) protocol.Bytes32 {
	return fixture.secrets[index]
}

func (fixture *handshakeFixture) session(index int) string {
	return fixture.allocation.Grants[index].SessionID
}

func (fixture *handshakeFixture) challengeRequest(index int, nonce protocol.Bytes16, endpoint netip.AddrPort) ChallengeRequest {
	return ChallengeRequest{
		RoomID:      "room",
		SessionID:   fixture.session(index),
		GrantID:     fixture.grantID(index),
		ClientNonce: nonce,
		Endpoint:    endpoint,
		InputBytes:  300,
	}
}

func (fixture *handshakeFixture) authRequest(
	index int,
	challenge ChallengeResult,
	clientNonce protocol.Bytes16,
	endpoint netip.AddrPort,
) AuthenticateRequest {
	tag := protocol.AuthTag(fixture.secret(index), protocol.Revision, "room", fixture.session(index),
		fixture.grantID(index), challenge.CandidateID, clientNonce, challenge.ServerNonce)
	return AuthenticateRequest{
		RoomID:      "room",
		SessionID:   fixture.session(index),
		CandidateID: challenge.CandidateID,
		Endpoint:    endpoint,
		AuthTag:     tag,
		InputBytes:  100,
	}
}

func assertNoSecretFields(t *testing.T, value any) {
	t.Helper()
	forbidden := []string{"secret", "key", "grant"}
	typeOf := reflect.TypeOf(value)
	for index := range typeOf.NumField() {
		name := typeOf.Field(index).Name
		for _, fragment := range forbidden {
			if containsFold(name, fragment) {
				t.Fatalf("%s exposes forbidden field %s", typeOf.Name(), name)
			}
		}
	}
}

func containsFold(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		match := true
		for offset := range len(fragment) {
			left, right := value[index+offset], fragment[offset]
			if left >= 'A' && left <= 'Z' {
				left += 'a' - 'A'
			}
			if left != right {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
