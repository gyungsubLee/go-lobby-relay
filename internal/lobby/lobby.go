package lobby

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/gyungsubLee/go-lobby-relay/internal/protocol"
	"github.com/gyungsubLee/go-lobby-relay/internal/store"
)

const (
	HardMaxOpenLobbies = 256
	HardMaxMembers     = 16
	HardMaxLobbyTTL    = 2 * time.Hour
	HardMaxListPage    = 50
	DefaultLobbyTTL    = 30 * time.Minute
	MatchTTL           = 2 * time.Minute

	maxIDDraws = 9
)

var (
	ErrInvalid     = errors.New("lobby: invalid")
	ErrNotFound    = errors.New("lobby: not found")
	ErrConflict    = errors.New("lobby: conflict")
	ErrForbidden   = errors.New("lobby: forbidden")
	ErrCapacity    = errors.New("lobby: capacity")
	ErrUnavailable = errors.New("lobby: unavailable")
	ErrFatalRandom = errors.New("lobby: fatal random")
)

type Visibility string

const (
	VisibilityPublic  Visibility = "public"
	VisibilityPrivate Visibility = "private"
)

type LobbyState string

const (
	LobbyStateOpen    LobbyState = "open"
	LobbyStateMatched LobbyState = "matched"
	LobbyStateClosed  LobbyState = "closed"
)

type Config struct {
	Relay  *store.Store
	Now    func() store.ClockReading
	Random io.Reader
}

type CreateRequest struct {
	Visibility Visibility
	QueueKey   string
	Capacity   uint32
}

type MemberSnapshot struct {
	PlayerID string
	Ready    bool
}

type LobbySummary struct {
	LobbyID, OwnerPlayerID, QueueKey string
	Visibility                       Visibility
	Capacity, MemberCount            uint32
	Revision                         uint64
	ExpiresAt                        time.Time
}

type LobbySnapshot struct {
	LobbyID, OwnerPlayerID, QueueKey string
	Visibility                       Visibility
	Capacity                         uint32
	Revision                         uint64
	State                            LobbyState
	Members                          []MemberSnapshot
	ExpiresAt                        time.Time
	Assignment                       *Assignment
}

type LobbyPage struct {
	Lobbies    []LobbySummary
	NextCursor string
}

type Assignment struct {
	MatchID, RoomID, PlayerID, SessionID string
	GrantID                              protocol.Bytes16
	GrantSecret                          protocol.Bytes32
	GrantExpiresAt                       time.Time
}

type Manager struct {
	mu sync.Mutex

	relay  *store.Store
	now    func() store.ClockReading
	random io.Reader

	lobbiesByID   map[string]*lobbyRecord
	lobbyByPlayer map[string]string
	matchIDs      map[string]struct{}
	nextSequence  uint64
}

type lobbyRecord struct {
	id, ownerPlayerID, queueKey string
	visibility                  Visibility
	capacity                    uint32
	state                       LobbyState
	revision, sequence          uint64
	createdAt, expiresAt        time.Time
	monoDeadline                time.Duration
	members                     map[string]*memberRecord
	assignments                 map[string]Assignment
}

type memberRecord struct {
	playerID     string
	ready        bool
	joinSequence uint64
}

func New(config Config) (*Manager, error) {
	if config.Relay == nil {
		return nil, ErrInvalid
	}
	now := config.Now
	if now == nil {
		origin := time.Now()
		now = func() store.ClockReading {
			current := time.Now()
			return store.ClockReading{Wall: current.UTC(), Mono: current.Sub(origin)}
		}
	}
	random := config.Random
	if random == nil {
		random = rand.Reader
	}
	return &Manager{
		relay:         config.Relay,
		now:           now,
		random:        random,
		lobbiesByID:   make(map[string]*lobbyRecord),
		lobbyByPlayer: make(map[string]string),
		matchIDs:      make(map[string]struct{}),
		nextSequence:  1,
	}, nil
}

func (manager *Manager) Create(playerID string, request CreateRequest) (LobbySnapshot, error) {
	if !protocol.ValidID(playerID) || !validCreateRequest(request) {
		return LobbySnapshot{}, ErrInvalid
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	reading := manager.now()
	manager.expireLocked(reading)
	if _, exists := manager.lobbyByPlayer[playerID]; exists {
		return LobbySnapshot{}, ErrConflict
	}
	if len(manager.lobbiesByID) >= HardMaxOpenLobbies || manager.nextSequence == math.MaxUint64 {
		return LobbySnapshot{}, ErrCapacity
	}
	lobbyID, err := manager.uniqueIDLocked("l-", func(candidate string) bool {
		_, exists := manager.lobbiesByID[candidate]
		return exists
	})
	if err != nil {
		return LobbySnapshot{}, err
	}
	deadline, ok := deadlineAfter(reading.Mono, DefaultLobbyTTL)
	if !ok {
		return LobbySnapshot{}, ErrInvalid
	}
	record := &lobbyRecord{
		id:            lobbyID,
		ownerPlayerID: playerID,
		queueKey:      request.QueueKey,
		visibility:    request.Visibility,
		capacity:      request.Capacity,
		state:         LobbyStateOpen,
		revision:      1,
		sequence:      manager.takeSequenceLocked(),
		createdAt:     reading.Wall.UTC(),
		expiresAt:     reading.Wall.UTC().Add(DefaultLobbyTTL),
		monoDeadline:  deadline,
		members:       make(map[string]*memberRecord, request.Capacity),
		assignments:   make(map[string]Assignment),
	}
	record.members[playerID] = &memberRecord{playerID: playerID, joinSequence: record.sequence}
	manager.lobbiesByID[lobbyID] = record
	manager.lobbyByPlayer[playerID] = lobbyID
	return snapshotFor(record, playerID), nil
}

func (manager *Manager) List(queueKey, cursor string, limit int) (LobbyPage, error) {
	if !protocol.ValidID(queueKey) || limit <= 0 || limit > HardMaxListPage {
		return LobbyPage{}, ErrInvalid
	}
	after, err := parseCursor(cursor)
	if err != nil {
		return LobbyPage{}, ErrInvalid
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.expireLocked(manager.now())
	records := make([]*lobbyRecord, 0, len(manager.lobbiesByID))
	for _, record := range manager.lobbiesByID {
		if record.state == LobbyStateOpen && record.visibility == VisibilityPublic && record.queueKey == queueKey && record.sequence > after {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(left, right int) bool { return records[left].sequence < records[right].sequence })
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	page := LobbyPage{Lobbies: make([]LobbySummary, len(records))}
	for index, record := range records {
		page.Lobbies[index] = summaryFor(record)
	}
	if hasMore {
		page.NextCursor = strconv.FormatUint(records[len(records)-1].sequence, 10)
	}
	return page, nil
}

func (manager *Manager) Get(playerID, lobbyID string) (LobbySnapshot, error) {
	if !protocol.ValidID(playerID) || !protocol.ValidID(lobbyID) {
		return LobbySnapshot{}, ErrInvalid
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.expireLocked(manager.now())
	record := manager.lobbiesByID[lobbyID]
	if record == nil {
		return LobbySnapshot{}, ErrNotFound
	}
	_, member := record.members[playerID]
	if (record.visibility == VisibilityPrivate || record.state == LobbyStateMatched) && !member {
		return LobbySnapshot{}, ErrNotFound
	}
	return snapshotFor(record, playerID), nil
}

func (manager *Manager) Join(playerID, lobbyID string, revision uint64) (LobbySnapshot, error) {
	if !protocol.ValidID(playerID) || !protocol.ValidID(lobbyID) || revision == 0 {
		return LobbySnapshot{}, ErrInvalid
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.expireLocked(manager.now())
	record := manager.lobbiesByID[lobbyID]
	if record == nil {
		return LobbySnapshot{}, ErrNotFound
	}
	if record.state != LobbyStateOpen || record.revision != revision {
		return LobbySnapshot{}, ErrConflict
	}
	if _, exists := manager.lobbyByPlayer[playerID]; exists {
		return LobbySnapshot{}, ErrConflict
	}
	if len(record.members) >= int(record.capacity) {
		return LobbySnapshot{}, ErrCapacity
	}
	if manager.nextSequence == math.MaxUint64 || record.revision == math.MaxUint64 {
		return LobbySnapshot{}, ErrCapacity
	}
	joinSequence := manager.takeSequenceLocked()
	record.members[playerID] = &memberRecord{playerID: playerID, joinSequence: joinSequence}
	manager.lobbyByPlayer[playerID] = lobbyID
	resetReady(record)
	record.revision++
	return snapshotFor(record, playerID), nil
}

func (manager *Manager) Leave(playerID, lobbyID string, revision uint64) (LobbySnapshot, error) {
	if !protocol.ValidID(playerID) || !protocol.ValidID(lobbyID) || revision == 0 {
		return LobbySnapshot{}, ErrInvalid
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.expireLocked(manager.now())
	record := manager.lobbiesByID[lobbyID]
	if record == nil {
		return LobbySnapshot{}, ErrNotFound
	}
	if record.state != LobbyStateOpen || record.revision != revision {
		return LobbySnapshot{}, ErrConflict
	}
	if _, member := record.members[playerID]; !member {
		return LobbySnapshot{}, ErrNotFound
	}
	if record.revision == math.MaxUint64 {
		return LobbySnapshot{}, ErrCapacity
	}
	delete(record.members, playerID)
	delete(manager.lobbyByPlayer, playerID)
	record.revision++
	if len(record.members) == 0 {
		record.state = LobbyStateClosed
		record.ownerPlayerID = ""
		closed := snapshotFor(record, playerID)
		delete(manager.lobbiesByID, lobbyID)
		return closed, nil
	}
	if record.ownerPlayerID == playerID {
		record.ownerPlayerID = firstMember(record).playerID
	}
	resetReady(record)
	return snapshotFor(record, playerID), nil
}

func (manager *Manager) SetReady(playerID, lobbyID string, revision uint64, ready bool) (LobbySnapshot, error) {
	if !protocol.ValidID(playerID) || !protocol.ValidID(lobbyID) || revision == 0 {
		return LobbySnapshot{}, ErrInvalid
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.expireLocked(manager.now())
	record := manager.lobbiesByID[lobbyID]
	if record == nil {
		return LobbySnapshot{}, ErrNotFound
	}
	if record.state != LobbyStateOpen || record.revision != revision {
		return LobbySnapshot{}, ErrConflict
	}
	member := record.members[playerID]
	if member == nil {
		return LobbySnapshot{}, ErrNotFound
	}
	if member.ready == ready {
		return snapshotFor(record, playerID), nil
	}
	if record.revision == math.MaxUint64 {
		return LobbySnapshot{}, ErrCapacity
	}
	member.ready = ready
	record.revision++
	return snapshotFor(record, playerID), nil
}

func (manager *Manager) Start(playerID, lobbyID string, revision uint64) (Assignment, error) {
	if !protocol.ValidID(playerID) || !protocol.ValidID(lobbyID) || revision == 0 {
		return Assignment{}, ErrInvalid
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	reading := manager.now()
	manager.expireLocked(reading)
	record := manager.lobbiesByID[lobbyID]
	if record == nil {
		return Assignment{}, ErrNotFound
	}
	if record.state != LobbyStateOpen || record.revision != revision {
		return Assignment{}, ErrConflict
	}
	if record.ownerPlayerID != playerID {
		return Assignment{}, ErrForbidden
	}
	if len(record.members) != int(record.capacity) || !allReady(record) || record.revision == math.MaxUint64 {
		return Assignment{}, ErrConflict
	}
	players := membersInJoinOrder(record)
	matchID, _, assignments, expiresAt, deadline, err := manager.allocateMatchLocked(players, reading)
	if err != nil {
		return Assignment{}, err
	}
	record.state = LobbyStateMatched
	record.revision++
	record.expiresAt = expiresAt
	record.monoDeadline = deadline
	record.assignments = assignments
	manager.matchIDs[matchID] = struct{}{}
	return assignments[playerID], nil
}

func (manager *Manager) Expire() {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.expireLocked(manager.now())
}

func validCreateRequest(request CreateRequest) bool {
	return (request.Visibility == VisibilityPublic || request.Visibility == VisibilityPrivate) &&
		protocol.ValidID(request.QueueKey) && request.Capacity >= 2 && request.Capacity <= HardMaxMembers
}

func parseCursor(cursor string) (uint64, error) {
	if cursor == "" {
		return 0, nil
	}
	value, err := strconv.ParseUint(cursor, 10, 64)
	if err != nil || value == 0 || strconv.FormatUint(value, 10) != cursor {
		return 0, ErrInvalid
	}
	return value, nil
}

func (manager *Manager) takeSequenceLocked() uint64 {
	sequence := manager.nextSequence
	manager.nextSequence++
	return sequence
}

func (manager *Manager) uniqueIDLocked(prefix string, exists func(string) bool) (string, error) {
	for range maxIDDraws {
		candidate, err := manager.drawIDLocked(prefix)
		if err != nil {
			return "", err
		}
		if !exists(candidate) {
			return candidate, nil
		}
	}
	return "", ErrFatalRandom
}

func (manager *Manager) drawIDLocked(prefix string) (string, error) {
	var raw [16]byte
	if _, err := io.ReadFull(manager.random, raw[:]); err != nil {
		return "", ErrFatalRandom
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func (manager *Manager) allocateMatchLocked(players []string, reading store.ClockReading) (string, string, map[string]Assignment, time.Time, time.Duration, error) {
	expiresAt := reading.Wall.UTC().Add(MatchTTL)
	deadline, ok := deadlineAfter(reading.Mono, MatchTTL)
	if !ok {
		return "", "", nil, time.Time{}, 0, ErrUnavailable
	}
	for range maxIDDraws {
		matchID, err := manager.uniqueIDLocked("m-", func(candidate string) bool {
			_, exists := manager.matchIDs[candidate]
			return exists
		})
		if err != nil {
			return "", "", nil, time.Time{}, 0, err
		}
		roomID, err := manager.drawIDLocked("r-")
		if err != nil {
			return "", "", nil, time.Time{}, 0, err
		}
		participants := make([]store.ParticipantDefinition, len(players))
		sessions := make(map[string]struct{}, len(players))
		validAttempt := true
		for index, player := range players {
			sessionID, sessionErr := manager.uniqueIDLocked("s-", func(candidate string) bool {
				_, exists := sessions[candidate]
				return exists
			})
			if sessionErr != nil {
				return "", "", nil, time.Time{}, 0, sessionErr
			}
			if _, duplicate := sessions[sessionID]; duplicate {
				validAttempt = false
				break
			}
			sessions[sessionID] = struct{}{}
			participants[index] = store.ParticipantDefinition{ParticipantID: player, SessionID: sessionID, GrantExpiresAt: expiresAt}
		}
		if !validAttempt {
			continue
		}
		allocation, created, allocationErr := manager.relay.CreateRoom(roomID, store.RoomDefinition{
			Capacity: uint32(len(players)), ExpiresAt: expiresAt, Participants: participants,
		})
		if errors.Is(allocationErr, store.ErrConflict) || allocationErr == nil && !created {
			continue
		}
		if allocationErr != nil {
			return "", "", nil, time.Time{}, 0, mapStoreError(allocationErr)
		}
		assignments := make(map[string]Assignment, len(players))
		for _, grant := range allocation.Grants {
			if grant.GrantSecret == nil {
				_ = manager.relay.EndRoom(roomID)
				return "", "", nil, time.Time{}, 0, ErrUnavailable
			}
			assignments[grant.ParticipantID] = Assignment{
				MatchID: matchID, RoomID: roomID, PlayerID: grant.ParticipantID, SessionID: grant.SessionID,
				GrantID: grant.GrantID, GrantSecret: *grant.GrantSecret, GrantExpiresAt: grant.GrantExpiresAt,
			}
		}
		if len(assignments) != len(players) {
			_ = manager.relay.EndRoom(roomID)
			return "", "", nil, time.Time{}, 0, ErrUnavailable
		}
		return matchID, roomID, assignments, expiresAt, deadline, nil
	}
	return "", "", nil, time.Time{}, 0, ErrFatalRandom
}

func mapStoreError(err error) error {
	switch {
	case errors.Is(err, store.ErrFatalRandom):
		return ErrFatalRandom
	case errors.Is(err, store.ErrCapacity):
		return ErrUnavailable
	default:
		return ErrUnavailable
	}
}

func (manager *Manager) expireLocked(reading store.ClockReading) {
	// ponytail: bounded M1 maps are scanned once; add deadline heaps only if profiling proves this too costly.
	for lobbyID, record := range manager.lobbiesByID {
		if reading.Mono < record.monoDeadline {
			continue
		}
		if record.state == LobbyStateMatched && len(record.assignments) != 0 {
			_ = manager.relay.EndRoom(firstAssignment(record.assignments).RoomID)
			delete(manager.matchIDs, firstAssignment(record.assignments).MatchID)
		}
		for playerID := range record.members {
			delete(manager.lobbyByPlayer, playerID)
		}
		delete(manager.lobbiesByID, lobbyID)
	}
}

func deadlineAfter(now, ttl time.Duration) (time.Duration, bool) {
	if ttl <= 0 || now > time.Duration(math.MaxInt64)-ttl {
		return 0, false
	}
	return now + ttl, true
}

func resetReady(record *lobbyRecord) {
	for _, member := range record.members {
		member.ready = false
	}
}

func allReady(record *lobbyRecord) bool {
	for _, member := range record.members {
		if !member.ready {
			return false
		}
	}
	return true
}

func firstMember(record *lobbyRecord) *memberRecord {
	members := membersInJoinOrder(record)
	return record.members[members[0]]
}

func membersInJoinOrder(record *lobbyRecord) []string {
	members := make([]*memberRecord, 0, len(record.members))
	for _, member := range record.members {
		members = append(members, member)
	}
	sort.Slice(members, func(left, right int) bool { return members[left].joinSequence < members[right].joinSequence })
	result := make([]string, len(members))
	for index, member := range members {
		result[index] = member.playerID
	}
	return result
}

func snapshotFor(record *lobbyRecord, playerID string) LobbySnapshot {
	snapshot := LobbySnapshot{
		LobbyID: record.id, OwnerPlayerID: record.ownerPlayerID, QueueKey: record.queueKey,
		Visibility: record.visibility, Capacity: record.capacity, Revision: record.revision,
		State: record.state, ExpiresAt: record.expiresAt,
	}
	ordered := membersInJoinOrder(record)
	snapshot.Members = make([]MemberSnapshot, len(ordered))
	for index, memberID := range ordered {
		member := record.members[memberID]
		snapshot.Members[index] = MemberSnapshot{PlayerID: member.playerID, Ready: member.ready}
	}
	if assignment, exists := record.assignments[playerID]; exists {
		copy := assignment
		snapshot.Assignment = &copy
	}
	return snapshot
}

func summaryFor(record *lobbyRecord) LobbySummary {
	return LobbySummary{
		LobbyID: record.id, OwnerPlayerID: record.ownerPlayerID, QueueKey: record.queueKey,
		Visibility: record.visibility, Capacity: record.capacity, MemberCount: uint32(len(record.members)),
		Revision: record.revision, ExpiresAt: record.expiresAt,
	}
}

func firstAssignment(assignments map[string]Assignment) Assignment {
	for _, assignment := range assignments {
		return assignment
	}
	return Assignment{}
}
