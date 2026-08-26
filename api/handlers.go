package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"fingerku/storage"
	"fingerku/zk"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	respondSuccess(w, http.StatusOK, map[string]interface{}{
		"service": "fingerku-api",
		"version": "1.0.0",
		"status":  "healthy",
		"time":    time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	cfg := s.getConfig()
	connected := s.IsConnected()

	stats, _ := s.db.GetAttendanceStats()
	users, _ := s.db.GetUsers()

	respondSuccess(w, http.StatusOK, map[string]interface{}{
		"connected":              connected,
		"device_ip":              cfg.IP,
		"device_port":            cfg.Port,
		"udp":                    cfg.UDP,
		"auto_connect":           cfg.AutoConnect,
		"auto_sync_interval_sec": cfg.AutoSyncIntervalSec,
		"uptime_seconds":         int(time.Since(s.startTime).Seconds()),
		"total_enrolled_users":   len(users),
		"attendance_stats":       stats,
	})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.db.GetDeviceConfig()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(w, http.StatusOK, cfg)
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var cfg storage.DeviceConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error())
		return
	}

	// Fill defaults before validation
	if cfg.IP == "" {
		cfg.IP = storage.DefaultDeviceConfig().IP
	}
	if cfg.Port <= 0 {
		cfg.Port = 4370
	}

	if err := validateDeviceConfig(cfg); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.db.SaveDeviceConfig(cfg); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
		return
	}

	// Reconnect if configured
	if cfg.AutoConnect {
		go func() { _ = s.Connect(cfg) }()
	}

	respondSuccess(w, http.StatusOK, cfg, "Configuration updated successfully")
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	storedCfg, _ := s.db.GetDeviceConfig()

	var cfg storage.DeviceConfig
	var raw map[string]json.RawMessage
	hasBody := false

	if r.Body != nil {
		body, _ := io.ReadAll(r.Body)
		if len(body) > 0 {
			hasBody = true
			_ = json.Unmarshal(body, &cfg)
			_ = json.Unmarshal(body, &raw)
		}
	}

	if cfg.IP == "" {
		cfg.IP = storedCfg.IP
	}
	if cfg.Port <= 0 {
		cfg.Port = storedCfg.Port
	}
	if hasBody && raw != nil {
		if _, ok := raw["password"]; !ok {
			cfg.Password = storedCfg.Password
		}
		if _, ok := raw["udp"]; !ok {
			cfg.UDP = storedCfg.UDP
		}
		if _, ok := raw["omit_ping"]; !ok {
			cfg.OmitPing = storedCfg.OmitPing
		}
		if _, ok := raw["auto_connect"]; !ok {
			cfg.AutoConnect = storedCfg.AutoConnect
		}
		if _, ok := raw["auto_sync_interval_sec"]; !ok {
			cfg.AutoSyncIntervalSec = storedCfg.AutoSyncIntervalSec
		}
	} else if !hasBody {
		// Empty body: use stored config entirely
		cfg = storedCfg
	}

	if err := s.Connect(cfg); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondSuccess(w, http.StatusOK, map[string]interface{}{
		"connected": true,
		"ip":        cfg.IP,
		"port":      cfg.Port,
	}, "Connected to ZKTeco machine successfully")
}

func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := s.Disconnect(); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(w, http.StatusOK, map[string]interface{}{"connected": false}, "Disconnected from device")
}

func (s *Server) handleDeviceInfo(w http.ResponseWriter, r *http.Request) {
	client, ok := s.activeDeviceClient()
	if !ok {
		respondError(w, http.StatusServiceUnavailable, "Device is not connected")
		return
	}

	info, err := client.GetDeviceInfo()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to read device info: "+err.Error())
		return
	}

	machTime, _ := client.GetTime()
	respondSuccess(w, http.StatusOK, map[string]interface{}{
		"info":        info,
		"device_time": machTime.Format("2006-01-02 15:04:05"),
	})
}

func (s *Server) handleUnlock(w http.ResponseWriter, r *http.Request) {
	if !s.allowAction(r, "unlock") {
		respondError(w, http.StatusTooManyRequests, "Too many unlock requests — please wait a moment")
		return
	}

	var req struct {
		Seconds int `json:"seconds"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	req.Seconds = validateSeconds(req.Seconds)

	client, ok := s.activeDeviceClient()
	if !ok {
		respondError(w, http.StatusServiceUnavailable, "Device is not connected")
		return
	}

	if err := client.Unlock(req.Seconds); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to unlock relay: "+err.Error())
		return
	}

	respondSuccess(w, http.StatusOK, map[string]interface{}{"seconds": req.Seconds}, "Door access relay triggered")
}

func (s *Server) handleSyncTime(w http.ResponseWriter, r *http.Request) {
	if !s.allowAction(r, "synctime") {
		respondError(w, http.StatusTooManyRequests, "Too many synctime requests — please wait a moment")
		return
	}
	client, ok := s.activeDeviceClient()
	if !ok {
		respondError(w, http.StatusServiceUnavailable, "Device is not connected")
		return
	}

	now := time.Now()
	if err := client.SetTime(now); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to sync device time: "+err.Error())
		return
	}

	respondSuccess(w, http.StatusOK, map[string]interface{}{"time": now.Format("2006-01-02 15:04:05")}, "Device time synchronized")
}

func (s *Server) handleVoice(w http.ResponseWriter, r *http.Request) {
	if !s.allowAction(r, "voice") {
		respondError(w, http.StatusTooManyRequests, "Too many voice requests — please wait a moment")
		return
	}

	var req struct {
		Index int `json:"index"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Index < 0 {
		req.Index = 0
	}
	if req.Index > 50 {
		respondError(w, http.StatusBadRequest, "voice index must be between 0 and 50")
		return
	}

	client, ok := s.activeDeviceClient()
	if !ok {
		respondError(w, http.StatusServiceUnavailable, "Device is not connected")
		return
	}

	if err := client.TestVoice(req.Index); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to play voice: "+err.Error())
		return
	}

	respondSuccess(w, http.StatusOK, map[string]interface{}{"index": req.Index}, "Voice prompt played")
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if !s.allowAction(r, "restart") {
		respondError(w, http.StatusTooManyRequests, "Too many restart requests — please wait a moment")
		return
	}
	client, ok := s.activeDeviceClient()
	if !ok {
		respondError(w, http.StatusServiceUnavailable, "Device is not connected")
		return
	}

	if err := client.Restart(); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to reboot: "+err.Error())
		return
	}
	s.connMu.Lock()
	s.connected = false
	s.connMu.Unlock()
	respondSuccess(w, http.StatusOK, nil, "Device reboot command sent")
}

func (s *Server) handlePowerOff(w http.ResponseWriter, r *http.Request) {
	if !s.allowAction(r, "poweroff") {
		respondError(w, http.StatusTooManyRequests, "Too many poweroff requests — please wait a moment")
		return
	}
	client, ok := s.activeDeviceClient()
	if !ok {
		respondError(w, http.StatusServiceUnavailable, "Device is not connected")
		return
	}

	if err := client.PowerOff(); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to power off: "+err.Error())
		return
	}
	s.connMu.Lock()
	s.connected = false
	s.connMu.Unlock()
	respondSuccess(w, http.StatusOK, nil, "Device power off command sent")
}

type UserWithDetails struct {
	zk.User
	FingerCount int    `json:"finger_count"`
	Role        string `json:"role"`
	IsDisabled  bool   `json:"is_disabled"`
}

func (s *Server) handleGetUsers(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source") // 'machine' or 'db' (default: db with fallback)

	var users []zk.User
	var err error

	if source == "machine" {
		client, ok := s.activeDeviceClient()
		if !ok {
			respondError(w, http.StatusServiceUnavailable, "Device is not connected")
			return
		}
		_ = client.DisableDevice()
		users, err = client.GetUsers()
		_ = client.EnableDevice()

		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to get users from machine: "+err.Error())
			return
		}
		_ = s.db.SaveUsersBatch(users)
	} else {
		users, err = s.db.GetUsers()
		if err != nil || len(users) == 0 {
			if client, ok := s.activeDeviceClient(); ok {
				_ = client.DisableDevice()
				users, _ = client.GetUsers()
				_ = client.EnableDevice()
				if len(users) > 0 {
					_ = s.db.SaveUsersBatch(users)
				}
			}
		}
	}

	fCountMap, _ := s.db.GetUserFingerCountMap()
	var userList []UserWithDetails
	for _, u := range users {
		userList = append(userList, UserWithDetails{
			User:        u,
			FingerCount: fCountMap[int(u.UID)],
			Role:        u.PrivilegeName(),
			IsDisabled:  u.IsDisabled(),
		})
	}

	respondSuccess(w, http.StatusOK, userList)
}

func (s *Server) handleSaveUser(w http.ResponseWriter, r *http.Request) {
	var u zk.User
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error())
		return
	}

	if u.UserID == "" {
		respondError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if len(u.UserID) > 24 {
		respondError(w, http.StatusBadRequest, "user_id must be at most 24 characters")
		return
	}
	if len(u.Name) > 24 {
		respondError(w, http.StatusBadRequest, "name must be at most 24 characters")
		return
	}
	if u.Privilege < 0 || u.Privilege > 14 {
		respondError(w, http.StatusBadRequest, "privilege must be between 0 and 14")
		return
	}
	if u.UID == 0 {
		if idNum, err := strconv.Atoi(u.UserID); err == nil && idNum > 0 && idNum <= 65535 {
			u.UID = uint16(idNum)
		}
	}
	if u.UID == 0 {
		respondError(w, http.StatusBadRequest, "uid is required (or provide numeric user_id)")
		return
	}

	// If connected, push to machine
	if client, ok := s.activeDeviceClient(); ok {
		_ = client.DisableDevice()
		if err := client.SetUser(u); err != nil {
			_ = client.EnableDevice()
			respondError(w, http.StatusInternalServerError, "Failed to save user to machine: "+err.Error())
			return
		}
		_ = client.EnableDevice()
	}

	// Save to SQLite
	_ = s.db.SaveUsersBatch([]zk.User{u})

	respondSuccess(w, http.StatusOK, u, "User saved successfully")
}

func (s *Server) handleGetUserByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "User ID is required")
		return
	}

	user, err := s.db.GetUser(id)
	if err != nil || user == nil {
		respondError(w, http.StatusNotFound, "User not found")
		return
	}

	templates, _ := s.db.GetUserTemplates(int(user.UID))

	respondSuccess(w, http.StatusOK, map[string]interface{}{
		"user":        user,
		"role":        user.PrivilegeName(),
		"is_disabled": user.IsDisabled(),
		"templates":   templates,
	})
}

func (s *Server) handleDeleteUserByID(w http.ResponseWriter, r *http.Request) {
	if !s.allowAction(r, "deleteuser") {
		respondError(w, http.StatusTooManyRequests, "Too many delete requests — please wait a moment")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "User ID is required")
		return
	}

	user, _ := s.db.GetUser(id)

	if client, ok := s.activeDeviceClient(); ok {
		_ = client.DisableDevice()
		uidNum, _ := strconv.Atoi(id)
		var uid uint16
		if user != nil {
			uid = user.UID
		} else if uidNum > 0 {
			uid = uint16(uidNum)
		}

		if uid > 0 {
			if err := client.DeleteUser(uid, id); err != nil {
				_ = client.EnableDevice()
				respondError(w, http.StatusInternalServerError, "Failed to delete user on device: "+err.Error())
				return
			}
		}
		_ = client.EnableDevice()
	}

	var uid uint16
	if user != nil {
		uid = user.UID
	}
	_ = s.db.DeleteUser(id, uid)

	respondSuccess(w, http.StatusOK, map[string]interface{}{"deleted": true, "user_id": id}, "User deleted successfully")
}

func (s *Server) handleGetTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := s.db.GetTemplates()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(w, http.StatusOK, templates)
}

func (s *Server) handleGetUserTemplates(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	user, err := s.db.GetUser(id)
	if err != nil || user == nil {
		respondError(w, http.StatusNotFound, "User not found")
		return
	}

	templates, err := s.db.GetUserTemplates(int(user.UID))
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondSuccess(w, http.StatusOK, templates)
}

func (s *Server) handleGetAttendance(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	page, _ := strconv.Atoi(q.Get("page"))
	if page > 1 {
		offset = (page - 1) * limit
	}

	// Validate date formats early
	for _, key := range []string{"from", "to"} {
		if v := q.Get(key); v != "" {
			if _, err := time.Parse("2006-01-02", v); err != nil {
				respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid date %q: expected YYYY-MM-DD", v))
				return
			}
		}
	}

	filter := storage.AttendanceFilter{
		UserID:    q.Get("user_id"),
		Search:    q.Get("search"),
		StartDate: q.Get("from"),
		EndDate:   q.Get("to"),
		Limit:     limit,
		Offset:    offset,
	}

	if statusStr := q.Get("status"); statusStr != "" {
		if statusVal, err := strconv.Atoi(statusStr); err == nil {
			filter.Status = &statusVal
		}
	}

	records, total, err := s.db.GetAttendance(filter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to query attendance logs: "+err.Error())
		return
	}

	respondPaginated(w, records, total, limit, offset)
}

func (s *Server) handleGetAttendanceStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.GetAttendanceStats()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(w, http.StatusOK, stats)
}

func (s *Server) handleGetMachineAttendance(w http.ResponseWriter, r *http.Request) {
	client, ok := s.activeDeviceClient()
	if !ok {
		respondError(w, http.StatusServiceUnavailable, "Device is not connected")
		return
	}

	_ = client.DisableDevice()
	defer client.EnableDevice()

	records, err := client.GetAttendance()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to read records from device RAM: "+err.Error())
		return
	}

	respondSuccess(w, http.StatusOK, records)
}

func (s *Server) handleClearMachineAttendance(w http.ResponseWriter, r *http.Request) {
	if !s.allowAction(r, "clearattendance") {
		respondError(w, http.StatusTooManyRequests, "Too many requests — please wait a moment")
		return
	}
	if err := s.ClearMachineAttendance(); err != nil {
		// Distinguish not-connected vs device error
		if err.Error() == "perangkat tidak terhubung" {
			respondError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "Failed to clear attendance on device: "+err.Error())
		return
	}
	respondSuccess(w, http.StatusOK, map[string]interface{}{"cleared": true}, "Attendance logs cleared on device")
}

func (s *Server) handleTriggerSync(w http.ResponseWriter, r *http.Request) {
	if !s.allowAction(r, "sync") {
		respondError(w, http.StatusTooManyRequests, "Too many sync requests — please wait a moment")
		return
	}
	res, err := s.SyncAll()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Sync failed: "+err.Error())
		return
	}
	respondSuccess(w, http.StatusOK, res, "Synchronization completed successfully")
}

func (s *Server) handleGetSyncHistory(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	history, err := s.db.GetSyncHistory(limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(w, http.StatusOK, history)
}

func (s *Server) handleSSELiveEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "Streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no")

	clientChan := s.broker.subscribe()
	defer s.broker.unsubscribe(clientChan)

	// Send initial ping
	_, _ = fmt.Fprintf(w, "event: ping\ndata: %s\n\n", time.Now().Format(time.RFC3339))
	flusher.Flush()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case msg, ok := <-clientChan:
			if !ok {
				return
			}
			_, _ = fmt.Fprintf(w, "event: punch\ndata: %s\n\n", msg)
			flusher.Flush()
		}
	}
}
