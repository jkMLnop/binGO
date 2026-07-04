package server

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/jkMLnop/binGO/db"
)

// Room is the in-memory representation of a bingo room (Phase 11.0).
// The room code (5-char alphanumeric) is the player-facing identifier.
// The associated game (BINGO-XXXXX) is created lazily on first room_login.
type Room struct {
	Code      string // 5-char alphanumeric, e.g. "AB3K7"
	HostID    string // First player to log into the room
	Game      *Game  // nil until the first player connects via room_login
	CreatedAt time.Time
	mu        sync.RWMutex
}

// NewRoom creates a new in-memory room with a generated code.
func NewRoom(code, hostID string) *Room {
	return &Room{
		Code:      code,
		HostID:    hostID,
		CreatedAt: time.Now(),
	}
}

// SetGame stores the lazy-created game for this room.
func (r *Room) SetGame(g *Game) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Game = g
}

// GetGame returns the current game (may be nil before first login).
func (r *Room) GetGame() *Game {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Game
}

// GenerateRoomCode returns a new random 5-char uppercase alphanumeric code.
// Collision checking against the DB is the caller's responsibility (done in CreateRoom).
func GenerateRoomCode() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var sb strings.Builder
	for range 5 {
		sb.WriteByte(chars[rand.Intn(len(chars))])
	}
	return sb.String()
}

// getOrCreateRoom returns the in-memory Room for the given 5-char code.
// If not found in memory it looks up the DB (for rooms created by other means or
// persisted across restarts). Returns an error if the room does not exist at all.
func (s *Server) getOrCreateRoom(code string) (*Room, error) {
	s.RoomsMu.RLock()
	room, ok := s.Rooms[code]
	s.RoomsMu.RUnlock()
	if ok {
		return room, nil
	}

	// Try DB lookup
	if s.DB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		dbRoom, err := s.DB.GetRoom(ctx, code)
		if err == nil && dbRoom != nil {
			r := NewRoom(dbRoom.Code, dbRoom.HostID)
			s.RoomsMu.Lock()
			s.Rooms[code] = r
			s.RoomsMu.Unlock()
			return r, nil
		}
	}

	return nil, fmt.Errorf("room code %s not found", code)
}

// createRoom creates a new room, persists it to the DB, and registers it in memory.
func (s *Server) createRoom(ctx context.Context, hostID string) (*Room, error) {
	var dbRoom *db.Room
	var err error

	if s.DB != nil {
		dbRoom, err = s.DB.CreateRoom(ctx, hostID)
		if err != nil {
			return nil, fmt.Errorf("failed to persist room: %w", err)
		}
	} else {
		// No DB — generate code entirely in-memory
		code := s.generateUniqueRoomCode()
		dbRoom = &db.Room{Code: code, HostID: hostID}
	}

	room := NewRoom(dbRoom.Code, dbRoom.HostID)

	s.RoomsMu.Lock()
	s.Rooms[dbRoom.Code] = room
	s.RoomsMu.Unlock()

	s.Metrics.RoomsActive.Inc()
	s.Logger.RoomCreated(dbRoom.Code, hostID)

	return room, nil
}

// generateUniqueRoomCode generates a room code that is not already in s.Rooms.
func (s *Server) generateUniqueRoomCode() string {
	for {
		code := GenerateRoomCode()
		s.RoomsMu.RLock()
		_, exists := s.Rooms[code]
		s.RoomsMu.RUnlock()
		if !exists {
			return code
		}
	}
}

// roomCodeForGame returns the 5-char room code associated with a game, or "" for
// standalone games that are not attached to any room.
func (s *Server) roomCodeForGame(game *Game) string {
	s.RoomsMu.RLock()
	defer s.RoomsMu.RUnlock()
	for _, room := range s.Rooms {
		if g := room.GetGame(); g != nil && g.ID == game.ID {
			return room.Code
		}
	}
	return ""
}
