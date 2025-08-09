package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

type Device struct {
	ID     string `json:"deviceId"`
	Secret string `json:"deviceSecret"`
	Label  string `json:"label"`
}

type Prefs struct {
	Idle struct {
		Effect string `json:"effect"`
		Color  string `json:"color"`
		Cycles int    `json:"cycles"`
	} `json:"idle"`
	Events map[string]struct {
		Effect string `json:"effect"`
		Color  string `json:"color"`
		Cycles int    `json:"cycles"`
	} `json:"events"`
}

type RegisterReq struct {
	Label    string `json:"label"`
	DeviceID string `json:"deviceId,omitempty"` // optional custom id
}
type RegisterResp struct {
	DeviceID     string `json:"deviceId"`
	DeviceSecret string `json:"deviceSecret"`
}

var (
	dataDir    = env("DATA_DIR", ".data")
	devFile    = filepath.Join(dataDir, "devices.json")
	prefsDir   = filepath.Join(dataDir, "prefs")
	upgrader   = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	devMu      sync.RWMutex
	devices    = map[string]Device{}
	wsMu       sync.Mutex
	wsByDevice = map[string]map[*websocket.Conn]struct{}{}
)

func main() {
	must(os.MkdirAll(prefsDir, 0755))
	must(loadDevices())

	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200); _, _ = w.Write([]byte("ok")) })

	r.Post("/register", handleRegister)

	r.Route("/devices/{id}", func(r chi.Router) {
		r.Get("/prefs", handleGetPrefs)
		r.Put("/prefs", handlePutPrefs)
		r.Post("/notify-config", handleNotifyConfig) // push “config_updated”
	})

	r.Get("/ws", handleWS)
	r.Post("/test/broadcast", handleTestBroadcast) // dev/test helper

	addr := ":" + env("PORT", "8080")
	fmt.Println("Server listening on", addr, "(data at", dataDir, ")")
	panic(http.ListenAndServe(addr, r))
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}

// ---------- devices.json ----------
func loadDevices() error {
	devMu.Lock()
	defer devMu.Unlock()
	_ = os.MkdirAll(dataDir, 0755)
	f, err := os.Open(devFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			devices = map[string]Device{}
			return nil
		}
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(&devices)
}
func saveDevices() error {
	devMu.RLock()
	defer devMu.RUnlock()
	tmp := devFile + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(devices); err != nil {
		_ = f.Close()
		return err
	}
	_ = f.Close()
	return os.Rename(tmp, devFile)
}

// ---------- prefs/<id>.json ----------
func prefsPath(id string) string { return filepath.Join(prefsDir, id+".json") }

func readPrefs(id string) (Prefs, error) {
	var p Prefs
	b, err := os.ReadFile(prefsPath(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			p.Idle.Effect, p.Idle.Color, p.Idle.Cycles = "breath", "#0000ff", 0
			p.Events = map[string]struct {
				Effect string `json:"effect"`
				Color  string `json:"color"`
				Cycles int    `json:"cycles"`
			}{
				"deal_won":        {Effect: "blink", Color: "#00ff00", Cycles: 3},
				"account_created": {Effect: "wipe", Color: "#00ffaa", Cycles: 2},
				"celebrate":       {Effect: "blink", Color: "#ff7f00", Cycles: 1},
			}
			return p, nil
		}
		return p, err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return p, err
	}
	return p, nil
}
func writePrefs(id string, p Prefs) error {
	_ = os.MkdirAll(prefsDir, 0755)
	tmp := prefsPath(id) + ".tmp"
	if err := os.WriteFile(tmp, mustJSON(p), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, prefsPath(id))
}
func mustJSON(v any) []byte { b, _ := json.MarshalIndent(v, "", "  "); return b }

// ---------- register ----------
func handleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", 400)
		return
	}

	id := strings.TrimSpace(req.DeviceID)
	if id == "" {
		id = "dev-" + randHex(6)
	}
	secret := randHex(16)

	devMu.Lock()
	if _, exists := devices[id]; exists {
		devMu.Unlock()
		http.Error(w, "device exists", http.StatusConflict)
		return
	}
	devices[id] = Device{ID: id, Secret: secret, Label: req.Label}
	devMu.Unlock()
	if err := saveDevices(); err != nil {
		http.Error(w, "save devices: "+err.Error(), 500)
		return
	}

	writeJSON(w, RegisterResp{DeviceID: id, DeviceSecret: secret})
}
func randHex(n int) string { b := make([]byte, n); _, _ = rand.Read(b); return hex.EncodeToString(b) }

// ---------- prefs handlers ----------
func handleGetPrefs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !deviceExists(id) {
		http.Error(w, "unknown device", 404)
		return
	}
	p, err := readPrefs(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, p)
}
func handlePutPrefs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !deviceExists(id) {
		http.Error(w, "unknown device", 404)
		return
	}
	var p Prefs
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	if err := writePrefs(id, p); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}
func deviceExists(id string) bool {
	devMu.RLock()
	defer devMu.RUnlock()
	_, ok := devices[id]
	return ok
}

// ---------- WebSocket (HMAC auth) ----------
func handleWS(w http.ResponseWriter, r *http.Request) {
	devID, ts, sig := r.Header.Get("X-Device-ID"), r.Header.Get("X-Auth-Ts"), r.Header.Get("X-Auth-Sig")
	if devID == "" || ts == "" || sig == "" {
		http.Error(w, "missing auth headers", http.StatusUnauthorized)
		return
	}
	if !deviceExists(devID) {
		http.Error(w, "unknown device", http.StatusUnauthorized)
		return
	}
	sec := deviceSecret(devID)
	if sec == "" {
		http.Error(w, "no secret", http.StatusUnauthorized)
		return
	}

	tUnix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || abs(time.Now().Unix()-tUnix) > 300 {
		http.Error(w, "timestamp skew", http.StatusUnauthorized)
		return
	}

	want := makeSig(devID, sec, ts)
	if !hmac.Equal([]byte(strings.ToLower(sig)), []byte(want)) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	addConn(devID, conn)
	defer removeConn(devID, conn)

	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(60 * time.Second)) })

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			return
		}
	}
}
func deviceSecret(id string) string {
	devMu.RLock()
	defer devMu.RUnlock()
	if d, ok := devices[id]; ok {
		return d.Secret
	}
	return ""
}
func makeSig(id, secret, ts string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(id))
	m.Write([]byte(":"))
	m.Write([]byte(ts))
	return hex.EncodeToString(m.Sum(nil))
}
func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
func addConn(id string, c *websocket.Conn) {
	wsMu.Lock()
	defer wsMu.Unlock()
	if wsByDevice[id] == nil {
		wsByDevice[id] = map[*websocket.Conn]struct{}{}
	}
	wsByDevice[id][c] = struct{}{}
}
func removeConn(id string, c *websocket.Conn) {
	wsMu.Lock()
	defer wsMu.Unlock()
	if set := wsByDevice[id]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(wsByDevice, id)
		}
	}
	_ = c.Close()
}

// ---------- broadcast & config notify ----------
type Broadcast struct {
	Type     string `json:"type"`
	Effect   string `json:"effect"`
	Color    string `json:"color"`
	Cycles   int    `json:"cycles"`
	DeviceID string `json:"deviceId,omitempty"` // optional target
}

func handleTestBroadcast(w http.ResponseWriter, r *http.Request) {
	var b Broadcast
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	if b.Type == "" && b.Effect == "" {
		http.Error(w, "need type or effect", 400)
		return
	}

	payload, _ := json.Marshal(b)

	sent := 0
	wsMu.Lock()
	if b.DeviceID != "" {
		if set := wsByDevice[b.DeviceID]; set != nil {
			for c := range set {
				_ = c.WriteMessage(websocket.TextMessage, payload)
				sent++
			}
		}
	} else {
		for _, set := range wsByDevice {
			for c := range set {
				_ = c.WriteMessage(websocket.TextMessage, payload)
				sent++
			}
		}
	}
	wsMu.Unlock()
	writeJSON(w, map[string]any{"status": "sent", "count": sent})
}
func handleNotifyConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	msg := []byte(`{"type":"config_updated"}`)
	n := 0
	wsMu.Lock()
	if set := wsByDevice[id]; set != nil {
		for c := range set {
			_ = c.WriteMessage(websocket.TextMessage, msg)
			n++
		}
	}
	wsMu.Unlock()
	writeJSON(w, map[string]any{"status": "notified", "count": n})
}

// ---------- utils ----------
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
