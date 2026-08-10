package store

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"math"
	"net/netip"
	"sort"
	"sync"
	"time"

	"github.com/gyungsubLee/go-game-relay/internal/protocol"
	"golang.org/x/time/rate"
)

const (
	HardMaxOpenRooms      = 256
	HardMaxRoomRecords    = 4096
	HardMaxRoomCapacity   = 16
	HardMaxActiveSessions = 4096
	HardMaxRoomTTL        = 2 * time.Hour
	HardMaxGrantTTL       = 2 * time.Hour
	HardMaxSweepInterval  = time.Second
	HardMaxEmptyGrace     = 5 * time.Second
	HardMaxTombstoneTTL   = 60 * time.Second
	HardMaxChallengeTTL   = 3 * time.Second
	HardMaxBindingTTL     = 60 * time.Second
	HardMaxPreauthSources = 4096

	HardMaxPreauthSourcePacketRate  rate.Limit = 16
	HardMaxPreauthSourcePacketBurst            = 160
	HardMaxPreauthSourceByteRate    rate.Limit = 19_200
	HardMaxPreauthSourceByteBurst              = 192_000
	HardMaxPreauthGlobalPacketRate  rate.Limit = 128
	HardMaxPreauthGlobalPacketBurst            = 1_280
	HardMaxPreauthGlobalByteRate    rate.Limit = 153_600
	HardMaxPreauthGlobalByteBurst              = 1_536_000
)

var (
	ErrInvalid     = errors.New("invalid")
	ErrNotFound    = errors.New("not found")
	ErrConflict    = errors.New("conflict")
	ErrCapacity    = errors.New("capacity")
	ErrFatalRandom = errors.New("fatal random")
)

type Limits struct {
	MaxOpenRooms      int
	MaxRoomRecords    int
	MaxRoomCapacity   int
	MaxActiveSessions int
	MaxRoomTTL        time.Duration
	MaxGrantTTL       time.Duration
	SweepInterval     time.Duration
	EmptyGrace        time.Duration
	TombstoneTTL      time.Duration
	ChallengeTTL      time.Duration
	BindingTTL        time.Duration

	PreauthSourcePacketRate  rate.Limit
	PreauthSourcePacketBurst int
	PreauthSourceByteRate    rate.Limit
	PreauthSourceByteBurst   int
	PreauthGlobalPacketRate  rate.Limit
	PreauthGlobalPacketBurst int
	PreauthGlobalByteRate    rate.Limit
	PreauthGlobalByteBurst   int
}

func DefaultLimits() Limits {
	return Limits{
		MaxOpenRooms:             HardMaxOpenRooms,
		MaxRoomRecords:           HardMaxRoomRecords,
		MaxRoomCapacity:          HardMaxRoomCapacity,
		MaxActiveSessions:        HardMaxActiveSessions,
		MaxRoomTTL:               HardMaxRoomTTL,
		MaxGrantTTL:              HardMaxGrantTTL,
		SweepInterval:            HardMaxSweepInterval,
		EmptyGrace:               HardMaxEmptyGrace,
		TombstoneTTL:             HardMaxTombstoneTTL,
		ChallengeTTL:             HardMaxChallengeTTL,
		BindingTTL:               HardMaxBindingTTL,
		PreauthSourcePacketRate:  HardMaxPreauthSourcePacketRate,
		PreauthSourcePacketBurst: HardMaxPreauthSourcePacketBurst,
		PreauthSourceByteRate:    HardMaxPreauthSourceByteRate,
		PreauthSourceByteBurst:   HardMaxPreauthSourceByteBurst,
		PreauthGlobalPacketRate:  HardMaxPreauthGlobalPacketRate,
		PreauthGlobalPacketBurst: HardMaxPreauthGlobalPacketBurst,
		PreauthGlobalByteRate:    HardMaxPreauthGlobalByteRate,
		PreauthGlobalByteBurst:   HardMaxPreauthGlobalByteBurst,
	}
}

type Config struct {
	Limits Limits
	Now    func() ClockReading
	Random io.Reader
}

type ClockReading struct {
	Wall time.Time
	Mono time.Duration
}

type GrantState string

const (
	GrantStateIssued  GrantState = "issued"
	GrantStateBound   GrantState = "bound"
	GrantStateExpired GrantState = "expired"
	GrantStateRevoked GrantState = "revoked"
)

type BindingState string

const (
	BindingStateUnbound       BindingState = "unbound"
	BindingStateBound         BindingState = "bound"
	BindingStateRebindPending BindingState = "rebind_pending"
	BindingStateExpired       BindingState = "expired"
	BindingStateRevoked       BindingState = "revoked"
)

type ParticipantDefinition struct {
	ParticipantID  string
	SessionID      string
	GrantExpiresAt time.Time
}

type RoomDefinition struct {
	Capacity     uint32
	ExpiresAt    time.Time
	Participants []ParticipantDefinition
}

type Allocation struct {
	RoomID               string
	CreatedAt, ExpiresAt time.Time
	Capacity             uint32
	Grants               []GrantAllocation
}

type GrantAllocation struct {
	ParticipantID, SessionID string
	GrantID                  protocol.Bytes16
	GrantSecret              *protocol.Bytes32
	GrantExpiresAt           time.Time
	State                    GrantState
}

type RoomSnapshot struct {
	RoomID               string
	CreatedAt, ExpiresAt time.Time
	Capacity             uint32
	Participants         []ParticipantSnapshot
}

type ParticipantSnapshot struct {
	ParticipantID, SessionID string
	GrantExpiresAt           time.Time
	GrantState               GrantState
	BindingState             BindingState
}

type Store struct {
	mu sync.RWMutex

	limits Limits
	now    func() ClockReading
	random io.Reader

	roomsByID            map[string]*roomRecord
	grantsByID           map[protocol.Bytes16]*grantRecord
	candidatesByID       map[protocol.Bytes16]*grantRecord
	bindingsByID         map[protocol.Bytes16]*grantRecord
	preauthSources       map[netip.Prefix]*preauthSource
	preauthGlobalPackets *rate.Limiter
	preauthGlobalBytes   *rate.Limiter
	openRooms            int
	activeSessions       int
}

type roomRecordState uint8

const (
	roomStateOpen roomRecordState = iota
	roomStateEmpty
	roomStateTombstone
)

type roomRecord struct {
	state             roomRecordState
	capacity          uint32
	createdAt         time.Time
	expiresAt         time.Time
	monoDeadline      time.Duration
	grants            []*grantRecord
	tombstoneDeadline time.Duration
}

type grantRecord struct {
	roomID        string
	participantID string
	sessionID     string
	id            protocol.Bytes16
	secret        *protocol.Bytes32
	expiresAt     time.Time
	monoDeadline  time.Duration
	state         GrantState
	bindingState  BindingState
	generation    uint64
	pending       *challengeRecord
	recent        *completedHandshake
	binding       *bindingRecord
}

func New(config Config) (*Store, error) {
	if !validLimits(config.Limits) {
		return nil, ErrInvalid
	}

	now := config.Now
	if now == nil {
		origin := time.Now()
		now = func() ClockReading {
			current := time.Now()
			return ClockReading{Wall: current.UTC(), Mono: current.Sub(origin)}
		}
	}
	random := config.Random
	if random == nil {
		random = rand.Reader
	}

	return &Store{
		limits:               config.Limits,
		now:                  now,
		random:               random,
		roomsByID:            make(map[string]*roomRecord),
		grantsByID:           make(map[protocol.Bytes16]*grantRecord),
		candidatesByID:       make(map[protocol.Bytes16]*grantRecord),
		bindingsByID:         make(map[protocol.Bytes16]*grantRecord),
		preauthSources:       make(map[netip.Prefix]*preauthSource),
		preauthGlobalPackets: rate.NewLimiter(config.Limits.PreauthGlobalPacketRate, config.Limits.PreauthGlobalPacketBurst),
		preauthGlobalBytes:   rate.NewLimiter(config.Limits.PreauthGlobalByteRate, config.Limits.PreauthGlobalByteBurst),
	}, nil
}

func (store *Store) CreateRoom(roomID string, definition RoomDefinition) (Allocation, bool, error) {
	canonical, err := canonicalDefinition(roomID, definition, store.limits)
	if err != nil {
		return Allocation{}, false, err
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	reading := store.now()

	if existing := store.roomsByID[roomID]; existing != nil {
		switch existing.state {
		case roomStateTombstone:
			if reading.Mono < existing.tombstoneDeadline {
				return Allocation{}, false, ErrConflict
			}
			delete(store.roomsByID, roomID)
		case roomStateEmpty:
			return Allocation{}, false, ErrConflict
		case roomStateOpen:
			if roomAccessTerminal(existing, reading.Mono) || !sameDefinition(existing, canonical) {
				return Allocation{}, false, ErrConflict
			}
			return allocationAt(roomID, existing, reading.Mono), false, nil
		default:
			return Allocation{}, false, ErrConflict
		}
	}

	wall := reading.Wall.UTC()
	roomTTL := canonical.expiresAt.Sub(wall)
	roomDeadline, ok := deadlineAfter(reading.Mono, roomTTL)
	if !ok || roomTTL > store.limits.MaxRoomTTL {
		return Allocation{}, false, ErrInvalid
	}
	grantDeadlines := make([]time.Duration, len(canonical.participants))
	for index, participant := range canonical.participants {
		grantTTL := participant.GrantExpiresAt.Sub(wall)
		grantDeadlines[index], ok = deadlineAfter(reading.Mono, grantTTL)
		if !ok || grantTTL > store.limits.MaxGrantTTL {
			return Allocation{}, false, ErrInvalid
		}
	}
	if store.openRooms >= store.limits.MaxOpenRooms ||
		len(store.roomsByID) >= store.limits.MaxRoomRecords ||
		store.activeSessions > store.limits.MaxActiveSessions-len(canonical.participants) {
		return Allocation{}, false, ErrCapacity
	}

	// ponytail: CSPRNG reads stay under the one store lock; split reservation/commit only if profiling shows contention.
	grants := make([]*grantRecord, len(canonical.participants))
	stagedIDs := make(map[protocol.Bytes16]struct{}, len(canonical.participants))
	for index, participant := range canonical.participants {
		grantID, ok := store.uniqueGrantID(stagedIDs)
		if !ok {
			return Allocation{}, false, ErrFatalRandom
		}
		var secret protocol.Bytes32
		if _, err := io.ReadFull(store.random, secret[:]); err != nil {
			return Allocation{}, false, ErrFatalRandom
		}
		secretCopy := secret
		grant := &grantRecord{
			roomID:        roomID,
			participantID: participant.ParticipantID,
			sessionID:     participant.SessionID,
			id:            grantID,
			secret:        &secretCopy,
			expiresAt:     participant.GrantExpiresAt,
			monoDeadline:  grantDeadlines[index],
			state:         GrantStateIssued,
			bindingState:  BindingStateUnbound,
		}
		grants[index] = grant
		stagedIDs[grantID] = struct{}{}
	}

	record := &roomRecord{
		state:        roomStateOpen,
		capacity:     canonical.capacity,
		createdAt:    wall,
		expiresAt:    canonical.expiresAt,
		monoDeadline: roomDeadline,
		grants:       grants,
	}
	store.roomsByID[roomID] = record
	for _, grant := range grants {
		store.grantsByID[grant.id] = grant
	}
	store.openRooms++
	store.activeSessions += len(grants)
	return allocationAt(roomID, record, reading.Mono), true, nil
}

func (store *Store) GetRoom(roomID string) (RoomSnapshot, error) {
	if !protocol.ValidID(roomID) {
		return RoomSnapshot{}, ErrInvalid
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	reading := store.now()
	record := store.roomsByID[roomID]
	if record == nil || record.state != roomStateOpen || roomAccessTerminal(record, reading.Mono) {
		return RoomSnapshot{}, ErrNotFound
	}
	return snapshotAt(roomID, record, reading.Mono), nil
}

func (store *Store) EndRoom(roomID string) error {
	if !protocol.ValidID(roomID) {
		return ErrInvalid
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	reading := store.now()
	record := store.roomsByID[roomID]
	if record == nil {
		return nil
	}
	if record.state == roomStateTombstone {
		if reading.Mono >= record.tombstoneDeadline {
			delete(store.roomsByID, roomID)
		}
		return nil
	}
	store.tombstoneRoom(record, reading.Mono, GrantStateRevoked)
	return nil
}

func (store *Store) Expire() {
	store.mu.Lock()
	defer store.mu.Unlock()
	now := store.now().Mono
	for roomID, room := range store.roomsByID {
		if room.state == roomStateTombstone {
			if now >= room.tombstoneDeadline {
				delete(store.roomsByID, roomID)
			}
			continue
		}

		for _, grant := range room.grants {
			store.expireRelay(grant, now)
			if grantLive(grant) && now >= grant.monoDeadline {
				store.terminalGrant(grant, GrantStateExpired)
			}
		}
		if now >= room.monoDeadline {
			store.tombstoneRoom(room, now, GrantStateExpired)
			continue
		}

		finalGrantDeadline := lastGrantDeadline(room)
		if now < finalGrantDeadline {
			continue
		}
		if room.state == roomStateOpen {
			room.state = roomStateEmpty
			store.openRooms--
		}
		if now >= saturatingAdd(finalGrantDeadline, store.limits.EmptyGrace) {
			store.tombstoneRoom(room, now, GrantStateExpired)
		}
	}
	for key, source := range store.preauthSources {
		if now >= saturatingAdd(source.lastObserved, preauthSourceIdleTTL) {
			delete(store.preauthSources, key)
		}
	}
}

func (store *Store) RunSweeper(ctx context.Context) {
	ticker := time.NewTicker(store.limits.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			store.Expire()
		}
	}
}

type normalizedDefinition struct {
	capacity     uint32
	expiresAt    time.Time
	participants []ParticipantDefinition
}

func canonicalDefinition(roomID string, definition RoomDefinition, limits Limits) (normalizedDefinition, error) {
	if !protocol.ValidID(roomID) || definition.Capacity == 0 || len(definition.Participants) == 0 ||
		uint64(definition.Capacity) != uint64(len(definition.Participants)) {
		return normalizedDefinition{}, ErrInvalid
	}
	if uint64(definition.Capacity) > uint64(limits.MaxRoomCapacity) || len(definition.Participants) > limits.MaxRoomCapacity {
		return normalizedDefinition{}, ErrCapacity
	}
	expiresAt := definition.ExpiresAt.UTC()
	if expiresAt.IsZero() {
		return normalizedDefinition{}, ErrInvalid
	}

	participants := append([]ParticipantDefinition(nil), definition.Participants...)
	participantIDs := make(map[string]struct{}, len(participants))
	sessionIDs := make(map[string]struct{}, len(participants))
	for index := range participants {
		participant := &participants[index]
		if !protocol.ValidID(participant.ParticipantID) || !protocol.ValidID(participant.SessionID) || participant.GrantExpiresAt.IsZero() {
			return normalizedDefinition{}, ErrInvalid
		}
		if _, exists := participantIDs[participant.ParticipantID]; exists {
			return normalizedDefinition{}, ErrInvalid
		}
		if _, exists := sessionIDs[participant.SessionID]; exists {
			return normalizedDefinition{}, ErrInvalid
		}
		participantIDs[participant.ParticipantID] = struct{}{}
		sessionIDs[participant.SessionID] = struct{}{}
		participant.GrantExpiresAt = participant.GrantExpiresAt.UTC()
		if participant.GrantExpiresAt.After(expiresAt) {
			return normalizedDefinition{}, ErrInvalid
		}
	}
	sort.Slice(participants, func(left, right int) bool {
		if participants[left].ParticipantID == participants[right].ParticipantID {
			return participants[left].SessionID < participants[right].SessionID
		}
		return participants[left].ParticipantID < participants[right].ParticipantID
	})
	return normalizedDefinition{capacity: definition.Capacity, expiresAt: expiresAt, participants: participants}, nil
}

func (store *Store) uniqueGrantID(staged map[protocol.Bytes16]struct{}) (protocol.Bytes16, bool) {
	for range 9 {
		var grantID protocol.Bytes16
		if _, err := io.ReadFull(store.random, grantID[:]); err != nil {
			return protocol.Bytes16{}, false
		}
		if _, exists := store.grantsByID[grantID]; exists {
			continue
		}
		if _, exists := staged[grantID]; exists {
			continue
		}
		return grantID, true
	}
	return protocol.Bytes16{}, false
}

func allocationAt(roomID string, room *roomRecord, now time.Duration) Allocation {
	allocation := Allocation{
		RoomID:    roomID,
		CreatedAt: room.createdAt,
		ExpiresAt: room.expiresAt,
		Capacity:  room.capacity,
		Grants:    make([]GrantAllocation, len(room.grants)),
	}
	for index, grant := range room.grants {
		state := grant.state
		if (state == GrantStateIssued || state == GrantStateBound) && now >= grant.monoDeadline {
			state = GrantStateExpired
		}
		allocationGrant := GrantAllocation{
			ParticipantID:  grant.participantID,
			SessionID:      grant.sessionID,
			GrantID:        grant.id,
			GrantExpiresAt: grant.expiresAt,
			State:          state,
		}
		if (state == GrantStateIssued || state == GrantStateBound) && grant.secret != nil {
			secret := *grant.secret
			allocationGrant.GrantSecret = &secret
		}
		allocation.Grants[index] = allocationGrant
	}
	return allocation
}

func snapshotAt(roomID string, room *roomRecord, now time.Duration) RoomSnapshot {
	snapshot := RoomSnapshot{
		RoomID:       roomID,
		CreatedAt:    room.createdAt,
		ExpiresAt:    room.expiresAt,
		Capacity:     room.capacity,
		Participants: make([]ParticipantSnapshot, len(room.grants)),
	}
	for index, grant := range room.grants {
		state := grantStateAt(grant, now)
		bindingState := grant.bindingState
		if bindingState == "" {
			bindingState = BindingStateUnbound
		}
		switch state {
		case GrantStateExpired:
			bindingState = BindingStateExpired
		case GrantStateRevoked:
			bindingState = BindingStateRevoked
		}
		snapshot.Participants[index] = ParticipantSnapshot{
			ParticipantID:  grant.participantID,
			SessionID:      grant.sessionID,
			GrantExpiresAt: grant.expiresAt,
			GrantState:     state,
			BindingState:   bindingState,
		}
	}
	return snapshot
}

func (store *Store) tombstoneRoom(room *roomRecord, now time.Duration, terminalState GrantState) {
	if room.state == roomStateTombstone {
		return
	}
	if room.state == roomStateOpen {
		store.openRooms--
	}
	for _, grant := range room.grants {
		store.terminalGrant(grant, terminalState)
	}
	*room = roomRecord{
		state:             roomStateTombstone,
		tombstoneDeadline: saturatingAdd(now, store.limits.TombstoneTTL),
	}
}

func (store *Store) terminalGrant(grant *grantRecord, terminalState GrantState) {
	store.clearRelay(grant)
	if grantLive(grant) {
		store.activeSessions--
		grant.state = terminalState
		if terminalState == GrantStateRevoked {
			grant.bindingState = BindingStateRevoked
		} else {
			grant.bindingState = BindingStateExpired
		}
	}
	delete(store.grantsByID, grant.id)
	if grant.secret != nil {
		*grant.secret = protocol.Bytes32{}
		grant.secret = nil
	}
}

func roomAccessTerminal(room *roomRecord, now time.Duration) bool {
	return room.state != roomStateOpen || now >= room.monoDeadline || now >= lastGrantDeadline(room)
}

func lastGrantDeadline(room *roomRecord) time.Duration {
	var deadline time.Duration
	for index, grant := range room.grants {
		if index == 0 || grant.monoDeadline > deadline {
			deadline = grant.monoDeadline
		}
	}
	return deadline
}

func grantLive(grant *grantRecord) bool {
	return grant.state == GrantStateIssued || grant.state == GrantStateBound
}

func grantStateAt(grant *grantRecord, now time.Duration) GrantState {
	if grantLive(grant) && now >= grant.monoDeadline {
		return GrantStateExpired
	}
	return grant.state
}

func sameDefinition(room *roomRecord, definition normalizedDefinition) bool {
	if room.capacity != definition.capacity || room.expiresAt != definition.expiresAt || len(room.grants) != len(definition.participants) {
		return false
	}
	for index, participant := range definition.participants {
		grant := room.grants[index]
		if grant.participantID != participant.ParticipantID || grant.sessionID != participant.SessionID ||
			grant.expiresAt != participant.GrantExpiresAt {
			return false
		}
	}
	return true
}

func deadlineAfter(now, ttl time.Duration) (time.Duration, bool) {
	if ttl <= 0 || now > time.Duration(math.MaxInt64)-ttl {
		return 0, false
	}
	return now + ttl, true
}

func saturatingAdd(deadline, delta time.Duration) time.Duration {
	if deadline > time.Duration(math.MaxInt64)-delta {
		return time.Duration(math.MaxInt64)
	}
	return deadline + delta
}

func validLimits(limits Limits) bool {
	return limits.MaxOpenRooms > 0 && limits.MaxOpenRooms <= HardMaxOpenRooms &&
		limits.MaxRoomRecords > 0 && limits.MaxRoomRecords <= HardMaxRoomRecords &&
		limits.MaxRoomCapacity > 0 && limits.MaxRoomCapacity <= HardMaxRoomCapacity &&
		limits.MaxActiveSessions > 0 && limits.MaxActiveSessions <= HardMaxActiveSessions &&
		limits.MaxRoomTTL > 0 && limits.MaxRoomTTL <= HardMaxRoomTTL &&
		limits.MaxGrantTTL > 0 && limits.MaxGrantTTL <= HardMaxGrantTTL &&
		limits.SweepInterval > 0 && limits.SweepInterval <= HardMaxSweepInterval &&
		limits.EmptyGrace > 0 && limits.EmptyGrace <= HardMaxEmptyGrace &&
		limits.TombstoneTTL > 0 && limits.TombstoneTTL <= HardMaxTombstoneTTL &&
		limits.ChallengeTTL > 0 && limits.ChallengeTTL <= HardMaxChallengeTTL &&
		limits.BindingTTL > 0 && limits.BindingTTL <= HardMaxBindingTTL &&
		validRate(limits.PreauthSourcePacketRate, HardMaxPreauthSourcePacketRate) &&
		limits.PreauthSourcePacketBurst > 0 && limits.PreauthSourcePacketBurst <= HardMaxPreauthSourcePacketBurst &&
		validRate(limits.PreauthSourceByteRate, HardMaxPreauthSourceByteRate) &&
		limits.PreauthSourceByteBurst > 0 && limits.PreauthSourceByteBurst <= HardMaxPreauthSourceByteBurst &&
		validRate(limits.PreauthGlobalPacketRate, HardMaxPreauthGlobalPacketRate) &&
		limits.PreauthGlobalPacketBurst > 0 && limits.PreauthGlobalPacketBurst <= HardMaxPreauthGlobalPacketBurst &&
		validRate(limits.PreauthGlobalByteRate, HardMaxPreauthGlobalByteRate) &&
		limits.PreauthGlobalByteBurst > 0 && limits.PreauthGlobalByteBurst <= HardMaxPreauthGlobalByteBurst &&
		limits.MaxOpenRooms <= limits.MaxRoomRecords &&
		limits.MaxRoomCapacity <= limits.MaxActiveSessions &&
		limits.MaxGrantTTL <= limits.MaxRoomTTL
}

func validRate(value, maximum rate.Limit) bool {
	return value > 0 && value <= maximum
}
