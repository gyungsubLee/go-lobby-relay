package lobby

import (
	"math"
	"time"

	"github.com/gyungsubLee/go-lobby-relay/internal/protocol"
	"github.com/gyungsubLee/go-lobby-relay/internal/store"
)

const (
	HardMaxTickets = 4096
	TicketTTL      = 2 * time.Minute
)

type TicketState string

const (
	TicketStateQueued    TicketState = "queued"
	TicketStateMatched   TicketState = "matched"
	TicketStateCancelled TicketState = "cancelled"
	TicketStateExpired   TicketState = "expired"
)

type EnqueueRequest struct {
	QueueKey string
	Capacity uint32
}

type TicketSnapshot struct {
	TicketID, PlayerID, QueueKey string
	State                        TicketState
	Capacity                     uint32
	Revision                     uint64
	ExpiresAt                    time.Time
	Assignment                   *Assignment
}

type queueKey struct {
	queueKey string
	capacity uint32
}

type ticketRecord struct {
	id, playerID, queueKey string
	capacity               uint32
	state                  TicketState
	revision, sequence     uint64
	expiresAt              time.Time
	monoDeadline           time.Duration
	assignment             *Assignment
}

func (manager *Manager) Enqueue(playerID string, request EnqueueRequest) (TicketSnapshot, error) {
	if !protocol.ValidID(playerID) || !validEnqueueRequest(request) {
		return TicketSnapshot{}, ErrInvalid
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	reading := manager.now()
	manager.expireLocked(reading)
	if _, exists := manager.lobbyByPlayer[playerID]; exists {
		return TicketSnapshot{}, ErrConflict
	}
	if _, exists := manager.ticketsByPlayer[playerID]; exists {
		return TicketSnapshot{}, ErrConflict
	}
	if len(manager.ticketsByPlayer) >= HardMaxTickets || manager.nextSequence == math.MaxUint64 {
		return TicketSnapshot{}, ErrCapacity
	}
	ticketID, err := manager.uniqueIDLocked("t-", func(candidate string) bool {
		for _, ticket := range manager.ticketsByPlayer {
			if ticket.id == candidate {
				return true
			}
		}
		return false
	})
	if err != nil {
		return TicketSnapshot{}, err
	}
	deadline, ok := deadlineAfter(reading.Mono, TicketTTL)
	if !ok {
		return TicketSnapshot{}, ErrInvalid
	}
	record := &ticketRecord{
		id: ticketID, playerID: playerID, queueKey: request.QueueKey, capacity: request.Capacity,
		state: TicketStateQueued, revision: 1, sequence: manager.takeSequenceLocked(),
		expiresAt: reading.Wall.UTC().Add(TicketTTL), monoDeadline: deadline,
	}
	manager.ticketsByPlayer[playerID] = record
	key := queueKey{queueKey: request.QueueKey, capacity: request.Capacity}
	manager.queues[key] = append(manager.queues[key], playerID)
	if err := manager.matchQueueLocked(key, reading); err != nil {
		return TicketSnapshot{}, err
	}
	return ticketSnapshot(record), nil
}

func (manager *Manager) GetTicket(playerID string) (TicketSnapshot, error) {
	if !protocol.ValidID(playerID) {
		return TicketSnapshot{}, ErrInvalid
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.expireLocked(manager.now())
	record := manager.ticketsByPlayer[playerID]
	if record == nil {
		return TicketSnapshot{}, ErrNotFound
	}
	return ticketSnapshot(record), nil
}

func (manager *Manager) CancelTicket(playerID string, revision uint64) (TicketSnapshot, error) {
	if !protocol.ValidID(playerID) || revision == 0 {
		return TicketSnapshot{}, ErrInvalid
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.expireLocked(manager.now())
	record := manager.ticketsByPlayer[playerID]
	if record == nil {
		return TicketSnapshot{}, ErrNotFound
	}
	if record.state != TicketStateQueued || record.revision != revision || record.revision == math.MaxUint64 {
		return TicketSnapshot{}, ErrConflict
	}
	record.state = TicketStateCancelled
	record.revision++
	snapshot := ticketSnapshot(record)
	delete(manager.ticketsByPlayer, playerID)
	manager.removeQueuedPlayerLocked(queueKey{queueKey: record.queueKey, capacity: record.capacity}, playerID)
	return snapshot, nil
}

func validEnqueueRequest(request EnqueueRequest) bool {
	return protocol.ValidID(request.QueueKey) && request.Capacity >= 2 && request.Capacity <= HardMaxMembers
}

func (manager *Manager) matchQueueLocked(key queueKey, reading store.ClockReading) error {
	manager.compactQueueLocked(key)
	for len(manager.queues[key]) >= int(key.capacity) {
		selectedIDs := manager.queues[key][:key.capacity]
		players := append([]string(nil), selectedIDs...)
		matchID, _, assignments, expiresAt, deadline, err := manager.allocateMatchLocked(players, reading)
		if err != nil {
			return err
		}
		for _, playerID := range players {
			ticket := manager.ticketsByPlayer[playerID]
			assignment := assignments[playerID]
			ticket.state = TicketStateMatched
			ticket.revision++
			ticket.expiresAt = expiresAt
			ticket.monoDeadline = deadline
			ticket.assignment = &assignment
		}
		manager.matchIDs[matchID] = struct{}{}
		remaining := append([]string(nil), manager.queues[key][key.capacity:]...)
		if len(remaining) == 0 {
			delete(manager.queues, key)
			return nil
		}
		manager.queues[key] = remaining
	}
	return nil
}

func (manager *Manager) compactQueueLocked(key queueKey) {
	queued := manager.queues[key]
	kept := queued[:0]
	for _, playerID := range queued {
		ticket := manager.ticketsByPlayer[playerID]
		if ticket != nil && ticket.state == TicketStateQueued && ticket.queueKey == key.queueKey && ticket.capacity == key.capacity {
			kept = append(kept, playerID)
		}
	}
	if len(kept) == 0 {
		delete(manager.queues, key)
		return
	}
	manager.queues[key] = kept
}

func (manager *Manager) removeQueuedPlayerLocked(key queueKey, playerID string) {
	queued := manager.queues[key]
	for index, candidate := range queued {
		if candidate != playerID {
			continue
		}
		manager.queues[key] = append(queued[:index], queued[index+1:]...)
		if len(manager.queues[key]) == 0 {
			delete(manager.queues, key)
		}
		return
	}
}

func (manager *Manager) expireTicketsLocked(reading store.ClockReading) {
	for playerID, ticket := range manager.ticketsByPlayer {
		if reading.Mono < ticket.monoDeadline {
			continue
		}
		if ticket.state == TicketStateQueued {
			manager.removeQueuedPlayerLocked(queueKey{queueKey: ticket.queueKey, capacity: ticket.capacity}, playerID)
		}
		if ticket.state == TicketStateMatched && ticket.assignment != nil {
			_ = manager.relay.EndRoom(ticket.assignment.RoomID)
			delete(manager.matchIDs, ticket.assignment.MatchID)
		}
		delete(manager.ticketsByPlayer, playerID)
	}
}

func ticketSnapshot(record *ticketRecord) TicketSnapshot {
	snapshot := TicketSnapshot{
		TicketID: record.id, PlayerID: record.playerID, QueueKey: record.queueKey,
		State: record.state, Capacity: record.capacity, Revision: record.revision, ExpiresAt: record.expiresAt,
	}
	if record.assignment != nil {
		assignment := *record.assignment
		snapshot.Assignment = &assignment
	}
	return snapshot
}
