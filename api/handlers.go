package api

import (
	"encoding/json"
	"fmt"
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
	s.mu.RLock()
	connected := s.connected
	cfg := s.config
	s.mu.RUnlock()

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

	if cfg.IP == "" {
		cfg.IP = "192.168.1.201"
	}
	if cfg.Port <= 0 {
		cfg.Port = 4370
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
	var cfg storage.DeviceConfig
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&cfg)
	}

	storedCfg, _ := s.db.GetDeviceConfig()
	if cfg.IP == "" {
		cfg.IP = storedCfg.IP
	}
	if cfg.Port <= 0 {
		cfg.Port = storedCfg.Port
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
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected || s.client == nil {
		respondError(w, http.StatusServiceUnavailable, "Device is not connected")
		return
	}

	info, err := s.client.GetDeviceInfo()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to read device info: "+err.Error())
		return
	}

	machTime, _ := s.client.GetTime()
	respondSuccess(w, http.StatusOK, map[string]interface{}{
		"info":        info,
		"device_time": machTime.Format("2006-01-02 15:04:05"),
	})
}

func (s *Server) handleUnlock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Seconds int `json:"seconds"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Seconds <= 0 {
		req.Seconds = 3
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected || s.client == nil {
		respondError(w, http.StatusServiceUnavailable, "Device is not connected")
		return
	}

	if err := s.client.Unlock(req.Seconds); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to unlock relay: "+err.Error())
		return
	}

	respondSuccess(w, http.StatusOK, map[string]interface{}{"seconds": req.Seconds}, "Door access relay triggered")
}

func (s *Server) handleSyncTime(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected || s.client == nil {
		respondError(w, http.StatusServiceUnavailable, "Device is not connected")
		return
	}

	now := time.Now()
	if err := s.client.SetTime(now); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to sync device time: "+err.Error())
		return
	}

	respondSuccess(w, http.StatusOK, map[string]interface{}{"time": now.Format("2006-01-02 15:04:05")}, "Device time synchronized")
}

func (s *Server) handleVoice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Index int `json:"index"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected || s.client == nil {
		respondError(w, http.StatusServiceUnavailable, "Device is not connected")
		return
	}

	if err := s.client.TestVoice(req.Index); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to play voice: "+err.Error())
		return
	}

	respondSuccess(w, http.StatusOK, map[string]interface{}{"index": req.Index}, "Voice prompt played")
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected || s.client == nil {
		respondError(w, http.StatusServiceUnavailable, "Device is not connected")
		return
	}

	if err := s.client.Restart(); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to reboot: "+err.Error())
		return
	}
	s.connected = false
	respondSuccess(w, http.StatusOK, nil, "Device reboot command sent")
}

func (s *Server) handlePowerOff(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected || s.client == nil {
		respondError(w, http.StatusServiceUnavailable, "Device is not connected")
		return
	}

	if err := s.client.PowerOff(); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to power off: "+err.Error())
		return
	}
	s.connected = false
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
		s.mu.Lock()
		if !s.connected || s.client == nil {
			s.mu.Unlock()
			respondError(w, http.StatusServiceUnavailable, "Device is not connected")
			return
		}
		_ = s.client.DisableDevice()
		users, err = s.client.GetUsers()
		_ = s.client.EnableDevice()
		s.mu.Unlock()

		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to get users from machine: "+err.Error())
			return
		}
		_ = s.db.SaveUsersBatch(users)
	} else {
		users, err = s.db.GetUsers()
		if err != nil || len(users) == 0 {
			// Try fallback to device if empty
			s.mu.Lock()
			if s.connected && s.client != nil {
				_ = s.client.DisableDevice()
				users, _ = s.client.GetUsers()
				_ = s.client.EnableDevice()
				if len(users) > 0 {
					_ = s.db.SaveUsersBatch(users)
				}
			}
			s.mu.Unlock()
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
	if u.UID == 0 {
		if idNum, err := strconv.Atoi(u.UserID); err == nil && idNum > 0 && idNum <= 65535 {
			u.UID = uint16(idNum)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// If connected, push to machine
	if s.connected && s.client != nil {
		_ = s.client.DisableDevice()
		defer s.client.EnableDevice()
		if err := s.client.SetUser(u); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to save user to machine: "+err.Error())
			return
		}
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
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "User ID is required")
		return
	}

	user, _ := s.db.GetUser(id)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.connected && s.client != nil {
		_ = s.client.DisableDevice()
		defer s.client.EnableDevice()

		uidNum, _ := strconv.Atoi(id)
		var uid uint16
		if user != nil {
			uid = user.UID
		} else if uidNum > 0 {
			uid = uint16(uidNum)
		}

		if uid > 0 {
			_ = s.client.DeleteUser(uid, id)
		}
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
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	page, _ := strconv.Atoi(q.Get("page"))
	if page > 1 {
		offset = (page - 1) * limit
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
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected || s.client == nil {
		respondError(w, http.StatusServiceUnavailable, "Device is not connected")
		return
	}

	_ = s.client.DisableDevice()
	defer s.client.EnableDevice()

	records, err := s.client.GetAttendance()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to read records from device RAM: "+err.Error())
		return
	}

	respondSuccess(w, http.StatusOK, records)
}

func (s *Server) handleTriggerSync(w http.ResponseWriter, r *http.Request) {
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
