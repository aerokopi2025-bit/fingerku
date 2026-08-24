package main

import (
	"embed"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"fingerku/storage"
	"fingerku/zk"
)

//go:embed static/*
var staticFS embed.FS

func main() {
	port := flag.Int("port", 8080, "HTTP Web Server Port")
	ip := flag.String("ip", "192.168.1.201", "Default ZKTeco Device IP Address")
	devPort := flag.Int("device-port", 4370, "Default ZKTeco Port")
	password := flag.Int("password", 0, "Default Commkey Password")
	udp := flag.Bool("udp", false, "Force UDP protocol")
	omitPing := flag.Bool("omit-ping", false, "Omit ICMP ping")
	mock := flag.Bool("mock", false, "Start directly in Mock/Demo mode")
	autoConnect := flag.Bool("auto-connect", true, "Automatically connect on server start")
	dbPath := flag.String("db", "fingerku.db", "SQLite database file path")

	flag.Parse()

	// Environment variable override
	if envPort := os.Getenv("PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			*port = p
		}
	}
	if envDB := os.Getenv("DB_PATH"); envDB != "" {
		*dbPath = envDB
	}

	// Initialize SQLite Database
	db, err := storage.Open(*dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize SQLite database (%s): %v", *dbPath, err)
	}
	defer db.Close()
	log.Printf("[SQLite Storage] Database ready at: %s", *dbPath)

	initialCfg := DeviceConfig{
		IP:       *ip,
		Port:     *devPort,
		Password: *password,
		UDP:      *udp,
		OmitPing: *omitPing,
		Mock:     *mock,
	}

	manager := NewDeviceManager(initialCfg, db)

	if *autoConnect {
		go func() {
			time.Sleep(200 * time.Millisecond)
			log.Printf("[DeviceManager] Attempting initial connection to %s:%d (Mock: %v)...", *ip, *devPort, *mock)
			if err := manager.Connect(initialCfg); err != nil {
				log.Printf("[DeviceManager] Initial connection failed (can be connected later via Web UI): %v", err)
			} else {
				log.Printf("[DeviceManager] Connected successfully to %s!", *ip)
				// Auto-sync initial records into SQLite
				go func() {
					time.Sleep(1 * time.Second)
					_, _ = manager.SyncToDatabase()
				}()
			}
		}()
	}

	server := &Server{
		manager: manager,
		db:      db,
	}

	mux := http.NewServeMux()
	server.registerRoutes(mux)

	addr := fmt.Sprintf(":%d", *port)
	fmt.Println("================================================================")
	fmt.Println("       🌟 Fingerku Web UI - ZKTeco Management Console 🌟        ")
	fmt.Println("================================================================")
	fmt.Printf(" Server running at:  http://localhost:%d\n", *port)
	fmt.Printf(" Device Target:      %s:%d (Mock: %v)\n", *ip, *devPort, *mock)
	fmt.Printf(" SQLite Database:    %s\n", *dbPath)
	fmt.Printf(" Tailwind CSS 4 UI:  Loaded and Ready\n")
	fmt.Println("================================================================")

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

// Server holds web API dependencies.
type Server struct {
	manager *DeviceManager
	db      *storage.DB
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// API Endpoints - Connection & Device
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/connect", s.handleConnect)
	mux.HandleFunc("/api/disconnect", s.handleDisconnect)
	mux.HandleFunc("/api/info", s.handleInfo)
	mux.HandleFunc("/api/users", s.handleUsers)
	mux.HandleFunc("/api/users/", s.handleUserByID)
	mux.HandleFunc("/api/admin/clear", s.handleClearAdmin)
	mux.HandleFunc("/api/attendance", s.handleAttendance)
	mux.HandleFunc("/api/templates", s.handleTemplates)
	mux.HandleFunc("/api/templates/", s.handleTemplateByID)
	mux.HandleFunc("/api/device/unlock", s.handleUnlock)
	mux.HandleFunc("/api/device/synctime", s.handleSyncTime)
	mux.HandleFunc("/api/device/voice", s.handleVoice)
	mux.HandleFunc("/api/device/lcd", s.handleLCD)
	mux.HandleFunc("/api/device/lcd/clear", s.handleLCDClear)
	mux.HandleFunc("/api/device/restart", s.handleRestart)
	mux.HandleFunc("/api/device/poweroff", s.handlePowerOff)
	mux.HandleFunc("/api/demo/punch", s.handleDemoPunch)
	mux.HandleFunc("/api/events", s.handleEvents)

	// API Endpoints - SQLite Database
	mux.HandleFunc("/api/db/sync", s.handleDBSync)
	mux.HandleFunc("/api/db/attendance", s.handleDBAttendance)
	mux.HandleFunc("/api/db/stats", s.handleDBStats)
	mux.HandleFunc("/api/db/history", s.handleDBSyncHistory)

	// Exports
	mux.HandleFunc("/api/export/attendance.csv", s.handleExportAttendanceCSV)
	mux.HandleFunc("/api/export/db-attendance.csv", s.handleExportDBAttendanceCSV)
	mux.HandleFunc("/api/export/users.csv", s.handleExportUsersCSV)

	// Static Assets with embedded fallback
	subFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("Failed to initialize static sub fs: %v", err)
	}

	fileServer := http.FileServer(http.FS(subFS))
	mux.Handle("/", fileServer)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	status := s.manager.GetStatus()
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var cfg DeviceConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid payload: "+err.Error())
		return
	}

	if cfg.Port == 0 {
		cfg.Port = 4370
	}
	if cfg.IP == "" && !cfg.Mock {
		cfg.IP = "192.168.1.201"
	}

	if err := s.manager.Connect(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status":  s.manager.GetStatus(),
	})
}

func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := s.manager.Disconnect(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	info, err := s.manager.GetDeviceInfo()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		users, err := s.manager.GetUsers()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, users)

	case http.MethodPost:
		var u zk.User
		if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid payload: "+err.Error())
			return
		}
		if err := s.manager.SaveUser(u); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "user": u})

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleUserByID(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		writeError(w, http.StatusBadRequest, "Missing user ID or UID")
		return
	}
	idParam := parts[2]

	switch r.Method {
	case http.MethodDelete:
		uidNum, _ := strconv.Atoi(idParam)
		var uid uint16
		var userID string
		if uidNum > 0 {
			uid = uint16(uidNum)
		} else {
			userID = idParam
		}

		if err := s.manager.DeleteUser(uid, userID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": idParam})

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleClearAdmin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := s.manager.ClearAdmin(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Admin privileges cleared"})
}

func (s *Server) handleAttendance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	records, err := s.manager.GetAttendance()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	tmps, err := s.manager.GetTemplates()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type TemplateResponse struct {
		UID  int    `json:"uid"`
		FID  int    `json:"fid"`
		Mark string `json:"mark"`
		Size int    `json:"size"`
	}
	var res []TemplateResponse
	for _, t := range tmps {
		res = append(res, TemplateResponse{
			UID:  t.UID,
			FID:  t.FID,
			Mark: t.Mark(),
			Size: t.Size,
		})
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleTemplateByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		writeError(w, http.StatusBadRequest, "Expected format: /api/templates/:uid/:fid")
		return
	}
	uidNum, _ := strconv.Atoi(parts[2])
	fidNum, _ := strconv.Atoi(parts[3])

	if err := s.manager.DeleteTemplate(uint16(uidNum), fidNum, ""); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// SQLite Database Endpoints

func (s *Server) handleDBSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	res, err := s.manager.SyncToDatabase()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"result":  res,
	})
}

func (s *Server) handleDBAttendance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	q := r.URL.Query()
	search := q.Get("search")
	userId := q.Get("user_id")
	startDate := q.Get("start_date")
	endDate := q.Get("end_date")
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 25
	}
	offset := (page - 1) * limit

	var statusPtr *int
	if statusStr := q.Get("status"); statusStr != "" && statusStr != "all" {
		if st, err := strconv.Atoi(statusStr); err == nil {
			statusPtr = &st
		}
	}

	filter := storage.AttendanceFilter{
		Search:    search,
		UserID:    userId,
		Status:    statusPtr,
		StartDate: startDate,
		EndDate:   endDate,
		Limit:     limit,
		Offset:    offset,
	}

	records, total, err := s.db.GetAttendance(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"records": records,
		"total":   total,
		"page":    page,
		"limit":   limit,
		"pages":   (total + int64(limit) - 1) / int64(limit),
	})
}

func (s *Server) handleDBStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	stats, err := s.db.GetAttendanceStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleDBSyncHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	history, err := s.db.GetSyncHistory(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, history)
}

func (s *Server) handleUnlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Seconds int `json:"seconds"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Seconds <= 0 {
		req.Seconds = 3
	}
	if err := s.manager.Unlock(req.Seconds); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "seconds": req.Seconds})
}

func (s *Server) handleSyncTime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	now := time.Now()
	if err := s.manager.SyncTime(now); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"timestamp": now.Format("2006-01-02 15:04:05"),
	})
}

func (s *Server) handleVoice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Index int `json:"index"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := s.manager.TestVoice(req.Index); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "index": req.Index})
}

func (s *Server) handleLCD(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Line int    `json:"line"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid payload: "+err.Error())
		return
	}
	if err := s.manager.WriteLCD(req.Line, req.Text); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Server) handleLCDClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := s.manager.ClearLCD(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := s.manager.Restart(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Device is restarting"})
}

func (s *Server) handlePowerOff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := s.manager.PowerOff(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Device powered off"})
}

func (s *Server) handleDemoPunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		UserID string `json:"user_id"`
		Status int    `json:"status"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	ev, err := s.manager.TriggerDemoPunch(req.UserID, req.Status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := s.manager.Subscribe()
	defer s.manager.Unsubscribe(ch)

	// Send initial ping
	fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"connected\"}\n\n")
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// SSE Keep-alive heartbeat
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			dataBytes, err := json.Marshal(ev.Data)
			if err == nil {
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, string(dataBytes))
				flusher.Flush()
			}
		}
	}
}

func (s *Server) handleExportAttendanceCSV(w http.ResponseWriter, r *http.Request) {
	records, err := s.manager.GetAttendance()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	users, _ := s.manager.GetUsers()
	userMap := make(map[string]string)
	for _, u := range users {
		userMap[u.UserID] = u.Name
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"device_attendance_%s.csv\"", time.Now().Format("20060102_150405")))

	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"UID", "User ID", "Nama", "Waktu", "Status ID", "Keterangan", "Punch Code"})

	for _, rec := range records {
		name := userMap[rec.UserID]
		if name == "" {
			name = "-"
		}
		_ = writer.Write([]string{
			strconv.Itoa(rec.UID),
			rec.UserID,
			name,
			rec.Timestamp.Format("2006-01-02 15:04:05"),
			strconv.Itoa(rec.Status),
			rec.StatusName(),
			strconv.Itoa(rec.Punch),
		})
	}
	writer.Flush()
}

func (s *Server) handleExportDBAttendanceCSV(w http.ResponseWriter, r *http.Request) {
	filter := storage.AttendanceFilter{
		Limit: 100000,
	}
	records, _, err := s.db.GetAttendance(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"sqlite_attendance_%s.csv\"", time.Now().Format("20060102_150405")))

	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"ID", "UID", "User ID", "Nama", "Waktu", "Status ID", "Keterangan", "Punch Code", "Device IP", "Source"})

	for _, rec := range records {
		_ = writer.Write([]string{
			strconv.FormatInt(rec.ID, 10),
			strconv.Itoa(rec.UID),
			rec.UserID,
			rec.UserName,
			rec.Timestamp.Format("2006-01-02 15:04:05"),
			strconv.Itoa(rec.Status),
			rec.StatusName,
			strconv.Itoa(rec.Punch),
			rec.DeviceIP,
			rec.Source,
		})
	}
	writer.Flush()
}

func (s *Server) handleExportUsersCSV(w http.ResponseWriter, r *http.Request) {
	users, err := s.manager.GetUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"users_export_%s.csv\"", time.Now().Format("20060102_150405")))

	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"UID", "User ID", "Nama", "Role / Privilege", "Password", "Group ID", "Card Number", "Status"})

	for _, u := range users {
		status := "Aktif"
		if u.IsDisabled() {
			status = "Nonaktif"
		}
		_ = writer.Write([]string{
			strconv.Itoa(int(u.UID)),
			u.UserID,
			u.Name,
			u.PrivilegeName(),
			u.Password,
			u.GroupID,
			strconv.Itoa(int(u.Card)),
			status,
		})
	}
	writer.Flush()
}
