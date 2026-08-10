package store

import (
	"io"
	"math"
	"net/netip"
	"time"

	"github.com/gyungsubLee/go-game-relay/internal/protocol"
	"golang.org/x/time/rate"
)

const preauthSourceIdleTTL = 60 * time.Second

type RejectReason string

const (
	RejectNone               RejectReason = ""
	RejectMalformed          RejectReason = "malformed"
	RejectOversized          RejectReason = "oversized"
	RejectUnsupportedVersion RejectReason = "unsupported_version"
	RejectUnknownGrant       RejectReason = "unknown_grant"
	RejectAuthFailed         RejectReason = "auth_failed"
	RejectReplay             RejectReason = "replay"
	RejectExpired            RejectReason = "expired"
	RejectRevoked            RejectReason = "revoked"
	RejectWrongRoom          RejectReason = "wrong_room"
	RejectWrongEndpoint      RejectReason = "wrong_endpoint"
	RejectNotBound           RejectReason = "not_bound"
	RejectRateLimited        RejectReason = "rate_limited"
	RejectFanoutLimited      RejectReason = "fanout_limited"
	RejectDraining           RejectReason = "draining"
	RejectFatalRandom        RejectReason = "fatal_random"
)

type PreauthRequest struct {
	Endpoint   netip.AddrPort
	InputBytes int
}

type ChallengeRequest struct {
	RoomID, SessionID string
	GrantID           protocol.Bytes16
	ClientNonce       protocol.Bytes16
	Endpoint          netip.AddrPort
	InputBytes        int
}

type ChallengeResult struct {
	CandidateID   protocol.Bytes16
	ServerNonce   protocol.Bytes32
	ExpiresUnixMS int64
}

type AuthenticateRequest struct {
	RoomID, SessionID string
	CandidateID       protocol.Bytes16
	Endpoint          netip.AddrPort
	AuthTag           protocol.Bytes32
	InputBytes        int
}

type BoundResult struct {
	BindingID     protocol.Bytes16
	ExpiresUnixMS int64
	AuthTag       protocol.Bytes32
}

type ClientDataRequest struct {
	RoomID, SessionID string
	BindingID         protocol.Bytes16
	Sequence          uint64
	Payload           []byte
	Endpoint          netip.AddrPort
	AuthTag           protocol.Bytes32
}

type PingRequest struct {
	RoomID, SessionID string
	BindingID         protocol.Bytes16
	Sequence          uint64
	Endpoint          netip.AddrPort
	AuthTag           protocol.Bytes32
}

type AdmittedClientData struct {
	store               *Store
	roomID              string
	sessionID           string
	senderParticipantID string
	bindingID           protocol.Bytes16
	bindingGeneration   uint64
	sequence            uint64
}

func (admitted AdmittedClientData) RoomID() string              { return admitted.roomID }
func (admitted AdmittedClientData) SessionID() string           { return admitted.sessionID }
func (admitted AdmittedClientData) SenderParticipantID() string { return admitted.senderParticipantID }
func (admitted AdmittedClientData) Sequence() uint64            { return admitted.sequence }

type RelayPlan struct {
	RoomID, SessionID   string
	SenderParticipantID string
	Sequence            uint64
	Recipients          []netip.AddrPort
}

type replayWindow struct {
	highest     uint64
	bitmap      uint64
	initialized bool
}

func (window *replayWindow) accept(sequence uint64) bool {
	if !window.initialized {
		window.highest = sequence
		window.bitmap = 1
		window.initialized = true
		return true
	}
	if sequence > window.highest {
		shift := sequence - window.highest
		if shift >= 64 {
			window.bitmap = 1
		} else {
			window.bitmap = window.bitmap<<shift | 1
		}
		window.highest = sequence
		return true
	}
	delta := window.highest - sequence
	if delta >= 64 {
		return false
	}
	bit := uint64(1) << delta
	if window.bitmap&bit != 0 {
		return false
	}
	window.bitmap |= bit
	return true
}

type challengeRecord struct {
	candidateID protocol.Bytes16
	clientNonce protocol.Bytes16
	serverNonce protocol.Bytes32
	endpoint    netip.AddrPort
	deadline    time.Duration
	result      ChallengeResult
}

type completedHandshake struct {
	candidateID protocol.Bytes16
	clientNonce protocol.Bytes16
	serverNonce protocol.Bytes32
	endpoint    netip.AddrPort
	deadline    time.Duration
	generation  uint64
	result      BoundResult
}

type bindingRecord struct {
	id         protocol.Bytes16
	key        protocol.Bytes32
	endpoint   netip.AddrPort
	deadline   time.Duration
	generation uint64
	replay     replayWindow
}

type preauthSource struct {
	lastObserved time.Duration
	packets      *rate.Limiter
	bytes        *rate.Limiter
}

func (store *Store) AdmitPreauth(request PreauthRequest) RejectReason {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.admitPreauthLocked(request.Endpoint, request.InputBytes, store.now().Mono)
}

func (store *Store) AdmitClientIngress(request ClientDataRequest, inputBytes int) (AdmittedClientData, RejectReason) {
	store.mu.Lock()
	defer store.mu.Unlock()
	reading := store.now()
	grant, binding, reason := store.admitAuthenticatedLocked(
		request.RoomID, request.SessionID, request.BindingID, request.Sequence, request.Endpoint,
		request.AuthTag, inputBytes, reading.Mono,
		func(key protocol.Bytes32) protocol.Bytes32 {
			return protocol.ClientDataTag(key, protocol.Revision, request.RoomID, request.SessionID,
				request.BindingID, request.Sequence, request.Payload)
		},
	)
	if reason != RejectNone {
		return AdmittedClientData{}, reason
	}
	return AdmittedClientData{
		store: store, roomID: grant.roomID, sessionID: grant.sessionID,
		senderParticipantID: grant.participantID, bindingID: binding.id,
		bindingGeneration: binding.generation, sequence: request.Sequence,
	}, RejectNone
}

func (store *Store) AdmitPing(request PingRequest, inputBytes int) RejectReason {
	store.mu.Lock()
	defer store.mu.Unlock()
	reading := store.now()
	_, _, reason := store.admitAuthenticatedLocked(
		request.RoomID, request.SessionID, request.BindingID, request.Sequence, request.Endpoint,
		request.AuthTag, inputBytes, reading.Mono,
		func(key protocol.Bytes32) protocol.Bytes32 {
			return protocol.PingTag(key, protocol.Revision, request.RoomID, request.SessionID,
				request.BindingID, request.Sequence)
		},
	)
	return reason
}

func (store *Store) AdmitFanout(admitted AdmittedClientData, outputBytes int) (RelayPlan, RejectReason) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if admitted.store != store {
		return RelayPlan{}, RejectNotBound
	}
	reading := store.now()
	grant := store.bindingsByID[admitted.bindingID]
	if grant == nil || grant.binding == nil || grant.binding.id != admitted.bindingID ||
		grant.binding.generation != admitted.bindingGeneration || grant.roomID != admitted.roomID ||
		grant.sessionID != admitted.sessionID || grant.participantID != admitted.senderParticipantID {
		return RelayPlan{}, RejectNotBound
	}
	room, reason := store.liveAuthority(grant, reading.Mono)
	if reason != RejectNone {
		return RelayPlan{}, reason
	}
	binding := grant.binding
	if binding == nil || reading.Mono >= binding.deadline {
		store.clearBinding(grant)
		return RelayPlan{}, RejectExpired
	}
	if outputBytes < 0 || outputBytes > protocol.MaxDatagramBytes {
		return RelayPlan{}, RejectOversized
	}

	recipients := make([]netip.AddrPort, 0, len(room.grants)-1)
	for _, recipient := range room.grants {
		if recipient == grant || !grantLive(recipient) {
			continue
		}
		if reading.Mono >= recipient.monoDeadline {
			store.terminalGrant(recipient, GrantStateExpired)
			continue
		}
		store.expireRelay(recipient, reading.Mono)
		if recipient.binding != nil {
			recipients = append(recipients, recipient.binding.endpoint)
		}
	}
	plan := RelayPlan{
		RoomID: grant.roomID, SessionID: grant.sessionID, SenderParticipantID: grant.participantID,
		Sequence: admitted.sequence, Recipients: recipients,
	}
	if len(recipients) == 0 {
		return plan, RejectNone
	}
	if outputBytes > math.MaxInt/len(recipients) {
		return RelayPlan{}, RejectOversized
	}
	plannedBytes := outputBytes * len(recipients)
	if !allowAtomic(limiterTime(reading.Mono),
		limiterCharge{room.fanoutWrites, len(recipients)},
		limiterCharge{room.fanoutBytes, plannedBytes},
		limiterCharge{store.globalFanoutWrites, len(recipients)},
		limiterCharge{store.globalFanoutBytes, plannedBytes},
	) {
		return RelayPlan{}, RejectFanoutLimited
	}
	return plan, RejectNone
}

func (store *Store) admitAuthenticatedLocked(
	roomID, sessionID string,
	bindingID protocol.Bytes16,
	sequence uint64,
	endpoint netip.AddrPort,
	authTag protocol.Bytes32,
	inputBytes int,
	now time.Duration,
	expectedTag func(protocol.Bytes32) protocol.Bytes32,
) (*grantRecord, *bindingRecord, RejectReason) {
	rejectPreauth := func(reason RejectReason) RejectReason {
		if store.admitPreauthLocked(endpoint, inputBytes, now) != RejectNone {
			return RejectRateLimited
		}
		return reason
	}
	if sequence == 0 || inputBytes < 0 {
		return nil, nil, rejectPreauth(RejectMalformed)
	}
	grant := store.bindingsByID[bindingID]
	if grant == nil || grant.binding == nil || grant.binding.id != bindingID {
		return nil, nil, rejectPreauth(RejectNotBound)
	}
	if grant.state == GrantStateRevoked {
		return nil, nil, rejectPreauth(RejectRevoked)
	}
	if grant.state == GrantStateExpired {
		return nil, nil, rejectPreauth(RejectExpired)
	}
	room := store.roomsByID[grant.roomID]
	if room == nil || room.state != roomStateOpen {
		return nil, nil, rejectPreauth(RejectRevoked)
	}
	if now >= room.monoDeadline {
		reason := rejectPreauth(RejectExpired)
		if reason == RejectExpired {
			store.tombstoneRoom(room, now, GrantStateExpired)
		}
		return nil, nil, reason
	}
	if now >= grant.monoDeadline {
		reason := rejectPreauth(RejectExpired)
		if reason == RejectExpired {
			store.terminalGrant(grant, GrantStateExpired)
		}
		return nil, nil, reason
	}
	if roomID != grant.roomID {
		return nil, nil, rejectPreauth(RejectWrongRoom)
	}
	if sessionID != grant.sessionID {
		return nil, nil, rejectPreauth(RejectAuthFailed)
	}
	binding := grant.binding
	if binding == nil || binding.id != bindingID {
		return nil, nil, rejectPreauth(RejectNotBound)
	}
	if now >= binding.deadline {
		reason := rejectPreauth(RejectExpired)
		if reason == RejectExpired {
			store.clearBinding(grant)
		}
		return nil, nil, reason
	}
	if !endpoint.IsValid() || binding.endpoint != endpoint {
		return nil, nil, rejectPreauth(RejectWrongEndpoint)
	}
	if !protocol.EqualTag(expectedTag(binding.key), authTag[:]) {
		return nil, nil, rejectPreauth(RejectAuthFailed)
	}

	fresh := binding.replay.accept(sequence)
	if !allowAtomic(limiterTime(now),
		limiterCharge{grant.ingressPackets, 1}, limiterCharge{grant.ingressBytes, inputBytes},
		limiterCharge{room.ingressPackets, 1}, limiterCharge{room.ingressBytes, inputBytes},
		limiterCharge{store.authenticatedGlobalPackets, 1}, limiterCharge{store.authenticatedGlobalBytes, inputBytes},
	) {
		return nil, nil, RejectRateLimited
	}
	if !fresh {
		return nil, nil, RejectReplay
	}
	return grant, binding, RejectNone
}

func (store *Store) BeginChallenge(request ChallengeRequest) (ChallengeResult, RejectReason) {
	store.mu.Lock()
	defer store.mu.Unlock()
	reading := store.now()
	if reason := store.admitPreauthLocked(request.Endpoint, request.InputBytes, reading.Mono); reason != RejectNone {
		return ChallengeResult{}, reason
	}
	if !request.Endpoint.IsValid() {
		return ChallengeResult{}, RejectAuthFailed
	}
	grant := store.grantsByID[request.GrantID]
	if grant == nil {
		return ChallengeResult{}, RejectUnknownGrant
	}
	room, reason := store.liveAuthority(grant, reading.Mono)
	if reason != RejectNone {
		return ChallengeResult{}, reason
	}
	if request.RoomID != grant.roomID {
		return ChallengeResult{}, RejectWrongRoom
	}
	if request.SessionID != grant.sessionID {
		return ChallengeResult{}, RejectAuthFailed
	}
	store.expireRelay(grant, reading.Mono)
	if pending := grant.pending; pending != nil {
		if pending.endpoint != request.Endpoint {
			return ChallengeResult{}, RejectWrongEndpoint
		}
		if pending.clientNonce != request.ClientNonce {
			return ChallengeResult{}, RejectAuthFailed
		}
		return pending.result, RejectNone
	}
	if grant.recent != nil && grant.recent.clientNonce == request.ClientNonce {
		return ChallengeResult{}, RejectAuthFailed
	}

	candidateID, ok := store.uniqueCandidateID()
	if !ok {
		return ChallengeResult{}, RejectFatalRandom
	}
	var serverNonce protocol.Bytes32
	if _, err := io.ReadFull(store.random, serverNonce[:]); err != nil {
		return ChallengeResult{}, RejectFatalRandom
	}
	deadline := saturatingAdd(reading.Mono, store.limits.ChallengeTTL)
	deadline = min(deadline, grant.monoDeadline, room.monoDeadline)
	result := ChallengeResult{
		CandidateID:   candidateID,
		ServerNonce:   serverNonce,
		ExpiresUnixMS: reading.Wall.Add(deadline - reading.Mono).UTC().UnixMilli(),
	}
	grant.pending = &challengeRecord{
		candidateID: candidateID,
		clientNonce: request.ClientNonce,
		serverNonce: serverNonce,
		endpoint:    request.Endpoint,
		deadline:    deadline,
		result:      result,
	}
	store.candidatesByID[candidateID] = grant
	if grant.binding != nil {
		grant.bindingState = BindingStateRebindPending
	}
	return result, RejectNone
}

func (store *Store) Authenticate(request AuthenticateRequest) (BoundResult, RejectReason) {
	store.mu.Lock()
	defer store.mu.Unlock()
	reading := store.now()
	if reason := store.admitPreauthLocked(request.Endpoint, request.InputBytes, reading.Mono); reason != RejectNone {
		return BoundResult{}, reason
	}
	if !request.Endpoint.IsValid() {
		return BoundResult{}, RejectAuthFailed
	}
	grant := store.candidatesByID[request.CandidateID]
	if grant == nil {
		return BoundResult{}, RejectAuthFailed
	}
	room, reason := store.liveAuthority(grant, reading.Mono)
	if reason != RejectNone {
		return BoundResult{}, reason
	}
	if request.RoomID != grant.roomID {
		return BoundResult{}, RejectWrongRoom
	}
	if request.SessionID != grant.sessionID {
		return BoundResult{}, RejectAuthFailed
	}
	if recent := grant.recent; recent != nil && recent.candidateID == request.CandidateID {
		if reading.Mono >= recent.deadline {
			if grant.binding != nil && reading.Mono >= grant.binding.deadline {
				store.clearBinding(grant)
			} else {
				store.clearRecent(grant)
			}
			return BoundResult{}, RejectExpired
		}
		if recent.endpoint != request.Endpoint {
			return BoundResult{}, RejectWrongEndpoint
		}
		if grant.binding == nil || grant.binding.generation != recent.generation || grant.binding.id != recent.result.BindingID {
			store.clearRecent(grant)
			return BoundResult{}, RejectExpired
		}
		if reading.Mono >= grant.binding.deadline {
			store.clearBinding(grant)
			return BoundResult{}, RejectExpired
		}
		expected := protocol.AuthTag(*grant.secret, protocol.Revision, grant.roomID, grant.sessionID, grant.id,
			recent.candidateID, recent.clientNonce, recent.serverNonce)
		if !protocol.EqualTag(expected, request.AuthTag[:]) {
			return BoundResult{}, RejectAuthFailed
		}
		return recent.result, RejectNone
	}
	pending := grant.pending
	if pending == nil || pending.candidateID != request.CandidateID {
		return BoundResult{}, RejectAuthFailed
	}
	if reading.Mono >= pending.deadline {
		store.clearPending(grant)
		return BoundResult{}, RejectExpired
	}
	if pending.endpoint != request.Endpoint {
		return BoundResult{}, RejectWrongEndpoint
	}
	expected := protocol.AuthTag(*grant.secret, protocol.Revision, grant.roomID, grant.sessionID, grant.id,
		pending.candidateID, pending.clientNonce, pending.serverNonce)
	if !protocol.EqualTag(expected, request.AuthTag[:]) {
		return BoundResult{}, RejectAuthFailed
	}
	bindingID, ok := store.uniqueBindingID()
	if !ok {
		return BoundResult{}, RejectFatalRandom
	}
	key := protocol.BindingKey(*grant.secret, protocol.Revision, grant.roomID, grant.sessionID, grant.id,
		pending.candidateID, pending.clientNonce, pending.serverNonce)
	deadline := saturatingAdd(reading.Mono, store.limits.BindingTTL)
	deadline = min(deadline, grant.monoDeadline, room.monoDeadline)
	expiresUnixMS := reading.Wall.Add(deadline - reading.Mono).UTC().UnixMilli()
	result := BoundResult{
		BindingID:     bindingID,
		ExpiresUnixMS: expiresUnixMS,
		AuthTag: protocol.BoundTag(key, protocol.Revision, grant.roomID, grant.sessionID,
			pending.candidateID, bindingID, expiresUnixMS),
	}
	completedDeadline := saturatingAdd(reading.Mono, store.limits.ChallengeTTL)
	completedDeadline = min(completedDeadline, deadline, grant.monoDeadline, room.monoDeadline)
	completed := &completedHandshake{
		candidateID: pending.candidateID,
		clientNonce: pending.clientNonce,
		serverNonce: pending.serverNonce,
		endpoint:    pending.endpoint,
		deadline:    completedDeadline,
		generation:  grant.generation + 1,
		result:      result,
	}
	newBinding := &bindingRecord{
		id:         bindingID,
		key:        key,
		endpoint:   request.Endpoint,
		deadline:   deadline,
		generation: completed.generation,
	}

	store.clearPending(grant)
	store.clearRecent(grant)
	store.clearBinding(grant)
	grant.generation = newBinding.generation
	grant.binding = newBinding
	grant.recent = completed
	grant.state = GrantStateBound
	grant.bindingState = BindingStateBound
	store.bindingsByID[bindingID] = grant
	store.candidatesByID[completed.candidateID] = grant
	return result, RejectNone
}

func (store *Store) liveAuthority(grant *grantRecord, now time.Duration) (*roomRecord, RejectReason) {
	if grant.state == GrantStateRevoked {
		return nil, RejectRevoked
	}
	if grant.state == GrantStateExpired {
		return nil, RejectExpired
	}
	room := store.roomsByID[grant.roomID]
	if room == nil || room.state == roomStateTombstone {
		return nil, RejectRevoked
	}
	if now >= room.monoDeadline {
		store.tombstoneRoom(room, now, GrantStateExpired)
		return nil, RejectExpired
	}
	if now >= grant.monoDeadline {
		store.terminalGrant(grant, GrantStateExpired)
		return nil, RejectExpired
	}
	return room, RejectNone
}

func (store *Store) uniqueCandidateID() (protocol.Bytes16, bool) {
	for range 9 {
		var candidateID protocol.Bytes16
		if _, err := io.ReadFull(store.random, candidateID[:]); err != nil {
			return protocol.Bytes16{}, false
		}
		if store.candidatesByID[candidateID] == nil {
			return candidateID, true
		}
	}
	return protocol.Bytes16{}, false
}

func (store *Store) uniqueBindingID() (protocol.Bytes16, bool) {
	for range 9 {
		var bindingID protocol.Bytes16
		if _, err := io.ReadFull(store.random, bindingID[:]); err != nil {
			return protocol.Bytes16{}, false
		}
		if store.bindingsByID[bindingID] == nil {
			return bindingID, true
		}
	}
	return protocol.Bytes16{}, false
}

func (store *Store) admitPreauthLocked(endpoint netip.AddrPort, inputBytes int, now time.Duration) RejectReason {
	key := sourceKey(endpoint)
	if !key.IsValid() {
		return RejectRateLimited
	}
	if inputBytes < 0 {
		inputBytes = 0
	}
	limiterNow := limiterTime(now)
	source := store.preauthSources[key]
	if source != nil && now >= saturatingAdd(source.lastObserved, preauthSourceIdleTTL) {
		delete(store.preauthSources, key)
		source = nil
	}
	if source == nil && len(store.preauthSources) >= HardMaxPreauthSources {
		if allowAtomic(limiterNow,
			limiterCharge{store.preauthGlobalPackets, 1},
			limiterCharge{store.preauthGlobalBytes, inputBytes},
		) {
			return RejectRateLimited
		}
		return RejectRateLimited
	}
	if source == nil {
		source = &preauthSource{
			packets: rate.NewLimiter(store.limits.PreauthSourcePacketRate, store.limits.PreauthSourcePacketBurst),
			bytes:   rate.NewLimiter(store.limits.PreauthSourceByteRate, store.limits.PreauthSourceByteBurst),
		}
		if !allowAtomic(limiterNow,
			limiterCharge{source.packets, 1}, limiterCharge{source.bytes, inputBytes},
			limiterCharge{store.preauthGlobalPackets, 1}, limiterCharge{store.preauthGlobalBytes, inputBytes},
		) {
			return RejectRateLimited
		}
		source.lastObserved = now
		store.preauthSources[key] = source
		return RejectNone
	}
	source.lastObserved = now
	if !allowAtomic(limiterNow,
		limiterCharge{source.packets, 1}, limiterCharge{source.bytes, inputBytes},
		limiterCharge{store.preauthGlobalPackets, 1}, limiterCharge{store.preauthGlobalBytes, inputBytes},
	) {
		return RejectRateLimited
	}
	return RejectNone
}

type limiterCharge struct {
	limiter *rate.Limiter
	cost    int
}

func allowAtomic(now time.Time, charges ...limiterCharge) bool {
	for _, charge := range charges {
		if charge.cost > charge.limiter.Burst() || charge.limiter.TokensAt(now) < float64(charge.cost) {
			return false
		}
	}
	for _, charge := range charges {
		if !charge.limiter.AllowN(now, charge.cost) {
			panic("store: limiter changed under store lock")
		}
	}
	return true
}

func sourceKey(endpoint netip.AddrPort) netip.Prefix {
	address := endpoint.Addr().Unmap()
	if address.Is4() {
		return netip.PrefixFrom(address, 32)
	}
	if address.Is6() {
		return netip.PrefixFrom(address, 64).Masked()
	}
	return netip.Prefix{}
}

func limiterTime(now time.Duration) time.Time {
	return time.Unix(0, int64(now))
}

func (store *Store) expireRelay(grant *grantRecord, now time.Duration) {
	if grant.pending != nil && now >= grant.pending.deadline {
		store.clearPending(grant)
	}
	if grant.recent != nil && now >= grant.recent.deadline {
		store.clearRecent(grant)
	}
	if grant.binding != nil && now >= grant.binding.deadline {
		store.clearBinding(grant)
	}
}

func (store *Store) clearPending(grant *grantRecord) {
	pending := grant.pending
	if pending == nil {
		return
	}
	delete(store.candidatesByID, pending.candidateID)
	pending.candidateID = protocol.Bytes16{}
	pending.clientNonce = protocol.Bytes16{}
	pending.serverNonce = protocol.Bytes32{}
	pending.endpoint = netip.AddrPort{}
	pending.result = ChallengeResult{}
	grant.pending = nil
	if grant.binding != nil {
		grant.bindingState = BindingStateBound
	}
}

func (store *Store) clearRecent(grant *grantRecord) {
	recent := grant.recent
	if recent == nil {
		return
	}
	delete(store.candidatesByID, recent.candidateID)
	recent.candidateID = protocol.Bytes16{}
	recent.clientNonce = protocol.Bytes16{}
	recent.serverNonce = protocol.Bytes32{}
	recent.endpoint = netip.AddrPort{}
	recent.result = BoundResult{}
	grant.recent = nil
}

func (store *Store) clearBinding(grant *grantRecord) {
	binding := grant.binding
	if binding == nil {
		return
	}
	if grant.recent != nil && grant.recent.generation == binding.generation {
		store.clearRecent(grant)
	}
	delete(store.bindingsByID, binding.id)
	binding.id = protocol.Bytes16{}
	binding.key = protocol.Bytes32{}
	binding.endpoint = netip.AddrPort{}
	binding.replay = replayWindow{}
	binding.generation = 0
	grant.binding = nil
	if grantLive(grant) {
		grant.state = GrantStateIssued
		grant.bindingState = BindingStateExpired
	}
}

func (store *Store) clearRelay(grant *grantRecord) {
	store.clearPending(grant)
	store.clearRecent(grant)
	store.clearBinding(grant)
	grant.generation = 0
}
