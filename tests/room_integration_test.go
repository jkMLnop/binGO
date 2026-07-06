//go:build integration
// +build integration

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkMLnop/binGO/db"
	"github.com/jkMLnop/binGO/server"
	"golang.org/x/net/websocket"
)

// startTestServerWithDB starts a server backed by a temp SQLite DB.
// Required for endpoints that return 503 when DB is nil (buzzwords, leaderboard).
func startTestServerWithDB(t *testing.T, port string) *server.Server {
	t.Helper()
	buzzwordPaths := []string{"../buzzwords.csv", "buzzwords.csv", filepath.Join(".", "buzzwords.csv")}
	var buzzwords [][]string
	for _, p := range buzzwordPaths {
		bw, err := LoadBuzzwords(p)
		if err == nil && len(bw) > 0 {
			buzzwords = bw
			break
		}
	}
	if len(buzzwords) == 0 {
		t.Fatal("startTestServerWithDB: could not load buzzwords.csv")
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	store, err := db.NewSQLiteStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("startTestServerWithDB: create DB: %v", err)
	}
	if err := store.Init(ctx); err != nil {
		t.Fatalf("startTestServerWithDB: init DB: %v", err)
	}

	srv := server.NewServer(buzzwords, 3, 3, port)
	server.ResetMetrics()
	srv.SetDB(store)
	go func() {
		if err := srv.Start(); err != nil && err.Error() != "http: Server closed" {
			log.Printf("server error on port %s: %v", port, err)
		}
	}()
	t.Cleanup(func() {
		srv.Stop(context.Background())
		store.Close(context.Background())
	})
	time.Sleep(100 * time.Millisecond)
	return srv
}

// createRoomForTest calls POST /api/rooms and returns the 5-char room code.
func createRoomForTest(t *testing.T, serverPort string) string {
	t.Helper()
	apiURL := fmt.Sprintf("http://localhost:%s/api/rooms", serverPort)
	resp, err := http.Post(apiURL, "application/json", nil)
	if err != nil {
		t.Fatalf("createRoomForTest: POST /api/rooms failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("createRoomForTest: expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Code       string `json:"code"`
			GameCode   string `json:"game_code"`
			HostID     string `json:"host_id"`
			GameStatus string `json:"game_status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("createRoomForTest: decode failed: %v", err)
	}
	if !result.Success || result.Data.Code == "" {
		t.Fatalf("createRoomForTest: expected room code, got empty")
	}
	return result.Data.Code
}

// TestCreateRoomAPI verifies that POST /api/rooms returns a valid 5-char room code,
// a pending game status, and no admin key is required.
func TestCreateRoomAPI(t *testing.T) {
	srv, err := startTestServer("9985")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop(context.Background())
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Post("http://localhost:9985/api/rooms", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/rooms failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Code       string `json:"code"`
			GameCode   string `json:"game_code"`
			HostID     string `json:"host_id"`
			GameStatus string `json:"game_status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !payload.Success {
		t.Fatal("expected success=true")
	}
	if len(payload.Data.Code) != 5 {
		t.Fatalf("expected 5-char room code, got %q", payload.Data.Code)
	}
	// Room code must be uppercase alphanumeric
	for _, c := range payload.Data.Code {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			t.Fatalf("room code %q contains non-alphanumeric char %q", payload.Data.Code, c)
		}
	}
	if payload.Data.GameStatus != "pending" {
		t.Fatalf("expected game_status=pending, got %q", payload.Data.GameStatus)
	}
	if payload.Data.HostID == "" {
		t.Fatal("expected non-empty host_id")
	}
	t.Logf("✓ room created: code=%s host=%s", payload.Data.Code, payload.Data.HostID)
}

// TestGetRoomAPI verifies that GET /api/room/:code returns the room after creation.
func TestGetRoomAPI(t *testing.T) {
	srv, err := startTestServer("9984")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop(context.Background())
	time.Sleep(100 * time.Millisecond)

	roomCode := createRoomForTest(t, "9984")

	resp, err := http.Get(fmt.Sprintf("http://localhost:9984/api/room/%s", roomCode))
	if err != nil {
		t.Fatalf("GET /api/room/:code failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Code       string `json:"code"`
			GameStatus string `json:"game_status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !payload.Success {
		t.Fatal("expected success=true")
	}
	if payload.Data.Code != roomCode {
		t.Fatalf("expected code=%s, got %s", roomCode, payload.Data.Code)
	}
	t.Logf("✓ GET /api/room/%s returned correct room info", roomCode)
}

// TestGetRoomAPINotFound verifies that GET /api/room/:code returns 404 for unknown codes.
func TestGetRoomAPINotFound(t *testing.T) {
	srv, err := startTestServer("9983")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop(context.Background())
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:9983/api/room/ZZZZZ")
	if err != nil {
		t.Fatalf("GET /api/room/ZZZZZ failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown room, got %d", resp.StatusCode)
	}
	t.Log("✓ unknown room returns 404")
}

// TestRoomLoginWebSocket verifies the room_login WS action creates a game lazily
// on first login and returns a welcome message with the room_code populated.
func TestRoomLoginWebSocket(t *testing.T) {
	srv, err := startTestServer("9982")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop(context.Background())
	time.Sleep(100 * time.Millisecond)

	roomCode := createRoomForTest(t, "9982")

	ws, err := websocket.Dial("ws://localhost:9982/ws", "", "http://localhost")
	if err != nil {
		t.Fatalf("failed to connect WS: %v", err)
	}
	defer ws.Close()

	loginMsg := map[string]interface{}{
		"action":    "room_login",
		"username":  "RoomPlayer1",
		"room_code": roomCode,
	}
	if err := websocket.JSON.Send(ws, loginMsg); err != nil {
		t.Fatalf("failed to send room_login: %v", err)
	}

	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var welcome map[string]interface{}
	if err := websocket.JSON.Receive(ws, &welcome); err != nil {
		t.Fatalf("failed to receive welcome: %v", err)
	}

	if welcome["type"] != "welcome" {
		t.Fatalf("expected type=welcome, got %v", welcome["type"])
	}
	gotRoomCode, _ := welcome["room_code"].(string)
	if gotRoomCode != roomCode {
		t.Fatalf("expected room_code=%s in welcome, got %q", roomCode, gotRoomCode)
	}
	gameCode, _ := welcome["code"].(string)
	if !strings.HasPrefix(gameCode, "BINGO-") {
		t.Fatalf("expected BINGO- prefixed game code in welcome, got %q", gameCode)
	}
	if welcome["player_id"] == "" || welcome["player_id"] == nil {
		t.Fatal("expected non-empty player_id in welcome")
	}
	t.Logf("✓ room_login created game %s for room %s", gameCode, roomCode)
}

// TestRoomBackwardCompatLogin verifies that the old "login" action with a BINGO-XXXXX
// game code still works after the game was lazily created via room_login.
func TestRoomBackwardCompatLogin(t *testing.T) {
	srv, err := startTestServer("9981")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop(context.Background())
	time.Sleep(100 * time.Millisecond)

	roomCode := createRoomForTest(t, "9981")

	// First player joins via room_login — triggers lazy game creation
	ws1, err := websocket.Dial("ws://localhost:9981/ws", "", "http://localhost")
	if err != nil {
		t.Fatalf("failed to connect ws1: %v", err)
	}
	defer ws1.Close()

	ws1.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := websocket.JSON.Send(ws1, map[string]interface{}{
		"action": "room_login", "username": "HostPlayer", "room_code": roomCode,
	}); err != nil {
		t.Fatalf("ws1 room_login send failed: %v", err)
	}

	var welcome1 map[string]interface{}
	if err := websocket.JSON.Receive(ws1, &welcome1); err != nil {
		t.Fatalf("ws1 welcome receive failed: %v", err)
	}
	gameCode, _ := welcome1["code"].(string)
	if gameCode == "" {
		t.Fatal("expected game code in first welcome")
	}

	// Second player joins via old-style login with the BINGO- game code
	ws2, err := websocket.Dial("ws://localhost:9981/ws", "", "http://localhost")
	if err != nil {
		t.Fatalf("failed to connect ws2: %v", err)
	}
	defer ws2.Close()

	ws2.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := websocket.JSON.Send(ws2, map[string]interface{}{
		"action": "login", "username": "JoinPlayer", "code": gameCode,
	}); err != nil {
		t.Fatalf("ws2 login send failed: %v", err)
	}

	var welcome2 map[string]interface{}
	if err := websocket.JSON.Receive(ws2, &welcome2); err != nil {
		t.Fatalf("ws2 welcome receive failed: %v", err)
	}
	if welcome2["type"] != "welcome" {
		t.Fatalf("expected welcome, got %v", welcome2["type"])
	}
	if welcome2["code"] != gameCode {
		t.Fatalf("expected same game code %s, got %v", gameCode, welcome2["code"])
	}
	t.Logf("✓ backward-compat login with %s worked alongside room_login for room %s", gameCode, roomCode)
}

// TestRoomBuzzwordsAPI verifies that the host can upload a custom word list and
// retrieve it via GET /api/room/:code/buzzwords.
func TestRoomBuzzwordsAPI(t *testing.T) {
	startTestServerWithDB(t, "9980")

	roomCode := createRoomForTest(t, "9980")

	// Build a list of 25 unique words (minimum required)
	words := make([]string, 25)
	for i := range words {
		words[i] = fmt.Sprintf("buzzword%02d", i+1)
	}

	body, _ := json.Marshal(map[string]interface{}{"words": words})
	postURL := fmt.Sprintf("http://localhost:9980/api/room/%s/buzzwords", roomCode)
	resp, err := http.Post(postURL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST buzzwords failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on POST buzzwords, got %d", resp.StatusCode)
	}

	var postPayload struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&postPayload); err != nil {
		t.Fatalf("decode POST response failed: %v", err)
	}
	if !postPayload.Success {
		t.Fatal("expected success=true on POST buzzwords")
	}

	// Retrieve and verify
	getResp, err := http.Get(postURL)
	if err != nil {
		t.Fatalf("GET buzzwords failed: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on GET buzzwords, got %d", getResp.StatusCode)
	}

	var getPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Words []string `json:"words"`
		} `json:"data"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&getPayload); err != nil {
		t.Fatalf("decode GET response failed: %v", err)
	}
	if len(getPayload.Data.Words) != 25 {
		t.Fatalf("expected 25 words back, got %d", len(getPayload.Data.Words))
	}
	t.Logf("✓ uploaded and retrieved %d buzzwords for room %s", len(getPayload.Data.Words), roomCode)
}

// TestRoomBuzzwordsTooFew verifies that POST /api/room/:code/buzzwords rejects
// lists with fewer than 24 words.
func TestRoomBuzzwordsTooFew(t *testing.T) {
	startTestServerWithDB(t, "9979")

	roomCode := createRoomForTest(t, "9979")

	body, _ := json.Marshal(map[string]interface{}{"words": []string{"only", "three", "words"}})
	resp, err := http.Post(
		fmt.Sprintf("http://localhost:9979/api/room/%s/buzzwords", roomCode),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for too-few words, got %d", resp.StatusCode)
	}
	t.Log("✓ too-few buzzwords correctly rejected with 400")
}

// TestRoomLeaderboard verifies that GET /api/room/:code/leaderboard returns an
// empty list for a fresh room with no wins recorded.
func TestRoomLeaderboard(t *testing.T) {
	startTestServerWithDB(t, "9978")

	roomCode := createRoomForTest(t, "9978")

	resp, err := http.Get(fmt.Sprintf("http://localhost:9978/api/room/%s/leaderboard", roomCode))
	if err != nil {
		t.Fatalf("GET /api/room/:code/leaderboard failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var payload struct {
		Success bool          `json:"success"`
		Data    []interface{} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !payload.Success {
		t.Fatal("expected success=true")
	}
	// Fresh room has no wins — data should be empty list (not null)
	if payload.Data == nil {
		t.Fatal("expected empty list, got null data")
	}
	t.Logf("✓ fresh room leaderboard returned empty list for room %s", roomCode)
}

// TestCreateRoomWithLinkedCode verifies that creating a room with linked_room_code
// in the request body stores the link and returns it in GET /api/room/:code.
func TestCreateRoomWithLinkedCode(t *testing.T) {
	startTestServerWithDB(t, "9979")

	// Create a parent room first
	parentCode := createRoomForTest(t, "9979")

	// Create a side-bet room linked to the parent
	body, err := json.Marshal(map[string]interface{}{
		"host_id":          "side-bet-host",
		"linked_room_code": parentCode,
	})
	if err != nil {
		t.Fatalf("marshal create room body: %v", err)
	}
	resp, err := http.Post("http://localhost:9979/api/rooms", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/rooms with linked_room_code failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var createPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Code           string  `json:"code"`
			LinkedRoomCode *string `json:"linked_room_code"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if !createPayload.Success {
		t.Fatal("expected success=true")
	}
	childCode := createPayload.Data.Code
	if childCode == "" {
		t.Fatal("expected non-empty room code")
	}
	if createPayload.Data.LinkedRoomCode == nil || *createPayload.Data.LinkedRoomCode != parentCode {
		t.Fatalf("expected linked_room_code=%s in create response, got %v", parentCode, createPayload.Data.LinkedRoomCode)
	}

	// Verify GET /api/room/:code also returns linked_room_code
	getResp, err := http.Get(fmt.Sprintf("http://localhost:9979/api/room/%s", childCode))
	if err != nil {
		t.Fatalf("GET /api/room/:code failed: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", getResp.StatusCode)
	}

	var getPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Code           string  `json:"code"`
			LinkedRoomCode *string `json:"linked_room_code"`
		} `json:"data"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&getPayload); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getPayload.Data.LinkedRoomCode == nil || *getPayload.Data.LinkedRoomCode != parentCode {
		t.Fatalf("expected linked_room_code=%s in GET response, got %v", parentCode, getPayload.Data.LinkedRoomCode)
	}

	t.Logf("✓ linked room creation and retrieval: parent=%s child=%s", parentCode, childCode)
}

// TestGetLinkedRoomsDB verifies that GetLinkedRooms returns all rooms linked to a
// given parent room code and returns nil/empty when no links exist.
func TestGetLinkedRoomsDB(t *testing.T) {
	srv := startTestServerWithDB(t, "9978")

	// Create two parent rooms and linked child rooms directly via API
	parentA := createRoomForTest(t, "9978")
	parentB := createRoomForTest(t, "9978")

	makeLinkedRoom := func(parent string) string {
		body, _ := json.Marshal(map[string]interface{}{
			"host_id":          "test-host",
			"linked_room_code": parent,
		})
		resp, err := http.Post("http://localhost:9978/api/rooms", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST linked room failed: %v", err)
		}
		defer resp.Body.Close()
		var p struct {
			Data struct{ Code string `json:"code"` } `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&p)
		return p.Data.Code
	}

	childA1 := makeLinkedRoom(parentA)
	childA2 := makeLinkedRoom(parentA)
	childB1 := makeLinkedRoom(parentB)

	// Verify GetLinkedRooms via the DB store directly
	ctx := context.Background()
	linkedA, err := srv.DB.GetLinkedRooms(ctx, parentA)
	if err != nil {
		t.Fatalf("GetLinkedRooms(%s) failed: %v", parentA, err)
	}
	if len(linkedA) != 2 {
		t.Fatalf("expected 2 rooms linked to parent A, got %d", len(linkedA))
	}
	for _, r := range linkedA {
		if r.Code != childA1 && r.Code != childA2 {
			t.Errorf("unexpected linked room: %s", r.Code)
		}
		if r.LinkedRoomCode == nil || *r.LinkedRoomCode != parentA {
			t.Errorf("linked room %s: expected LinkedRoomCode=%s, got %v", r.Code, parentA, r.LinkedRoomCode)
		}
	}

	linkedB, err := srv.DB.GetLinkedRooms(ctx, parentB)
	if err != nil {
		t.Fatalf("GetLinkedRooms(%s) failed: %v", parentB, err)
	}
	if len(linkedB) != 1 {
		t.Fatalf("expected 1 room linked to parent B, got %d", len(linkedB))
	}
	if linkedB[0].Code != childB1 {
		t.Errorf("expected %s, got %s", childB1, linkedB[0].Code)
	}

	// No rooms linked to an unknown code
	emptyResult, err := srv.DB.GetLinkedRooms(ctx, "ZZZZZ")
	if err != nil {
		t.Fatalf("GetLinkedRooms(ZZZZZ) failed: %v", err)
	}
	if len(emptyResult) != 0 {
		t.Fatalf("expected 0 linked rooms for unknown code, got %d", len(emptyResult))
	}

	t.Logf("✓ GetLinkedRooms: parentA has %d children, parentB has %d, unknown has 0",
		len(linkedA), len(linkedB))
}
