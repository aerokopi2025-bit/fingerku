package api

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"fingerku/storage"
	"fingerku/zk"
)

//go:embed static/*
var staticFS embed.FS

// SSEBroker manages connected Server-Sent Events subscribers.
type SSEBroker struct {
	mu      sync.Mutex
	clients map[chan []byte]bool
}

func newSSEBroker() *SSEBroker {
	return &SSEBroker{
		clients: make(map[chan []byte]bool),
	}
}

func (b *SSEBroker) subscribe() chan []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan []byte, 32)
	b.clients[ch] = true
	return ch
}

func (b *SSEBroker) unsubscribe(ch chan []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.clients, ch)
	close(ch)
}

func (b *SSEBroker) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.clients)
}

func (b *SSEBroker) broadcast(data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- data:
		default:
		}
	}
}

// Server provides the REST API and background device manager.
type Server struct {
	db           *storage.DB
	client       *zk.Client
	config       storage.DeviceConfig
	connected    bool
	mu           sync.Mutex
	broker       *SSEBroker
	startTime    time.Time
	autoSyncStop chan struct{}
	verbose      bool
}

// NewServer creates a new API Server with SQLite backing.
func NewServer(db *storage.DB, verbose bool) (*Server, error) {
	cfg, err := db.GetDeviceConfig()
	if err != nil {
		cfg = storage.DefaultDeviceConfig()
	}

	s := &Server{
		db:        db,
		config:    cfg,
		broker:    newSSEBroker(),
		startTime: time.Now(),
		verbose:   verbose,
	}

	// Auto-connect if configured
	if cfg.AutoConnect {
		go func() {
			time.Sleep(200 * time.Millisecond)
			if err := s.Connect(cfg); err != nil {
				log.Printf("[API Server] Auto-connect to %s:%d failed: %v", cfg.IP, cfg.Port, err)
			}
		}()
	}

	return s, nil
}

// Routes configures and returns the chi router.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	// Standard middlewares
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.CleanPath)

	// CORS handler
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Health endpoint
	r.Get("/health", s.handleHealth)

	// API v1
	r.Route("/api/v1", func(r chi.Router) {
		// System & Config
		r.Get("/status", s.handleStatus)
		r.Get("/config", s.handleGetConfig)
		r.Put("/config", s.handleUpdateConfig)

		// Device Connection & Hardware Controls
		r.Route("/device", func(r chi.Router) {
			r.Post("/connect", s.handleConnect)
			r.Post("/disconnect", s.handleDisconnect)
			r.Get("/info", s.handleDeviceInfo)
			r.Post("/unlock", s.handleUnlock)
			r.Post("/synctime", s.handleSyncTime)
			r.Post("/voice", s.handleVoice)
			r.Post("/restart", s.handleRestart)
			r.Post("/poweroff", s.handlePowerOff)
		})

		// Users & Biometric Fingerprints
		r.Route("/users", func(r chi.Router) {
			r.Get("/", s.handleGetUsers)
			r.Post("/", s.handleSaveUser)
			r.Get("/{id}", s.handleGetUserByID)
			r.Delete("/{id}", s.handleDeleteUserByID)
			r.Get("/{id}/templates", s.handleGetUserTemplates)
		})

		// Fingerprint Templates
		r.Get("/templates", s.handleGetTemplates)

		// Attendance Logs & Statistics
		r.Route("/attendance", func(r chi.Router) {
			r.Get("/", s.handleGetAttendance)
			r.Get("/stats", s.handleGetAttendanceStats)
			r.Get("/machine", s.handleGetMachineAttendance)
		})

		// Sync & Stream
		r.Post("/sync", s.handleTriggerSync)
		r.Get("/sync/history", s.handleGetSyncHistory)
		r.Get("/events", s.handleSSELiveEvents)
	})

	// Static UI Dashboard FileServer
	subFS, err := fs.Sub(staticFS, "static")
	if err == nil {
		r.Handle("/*", http.FileServer(http.FS(subFS)))
	}

	return r
}

// Connect establishes connection with the ZKTeco hardware.
func (s *Server) Connect(cfg storage.DeviceConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.connected && s.client != nil {
		_ = s.client.Disconnect()
		s.connected = false
	}

	opts := []zk.Option{
		zk.WithPort(cfg.Port),
		zk.WithTimeout(8 * time.Second),
		zk.WithPassword(cfg.Password),
	}
	if cfg.UDP {
		opts = append(opts, zk.WithForceUDP(true))
	}
	if cfg.OmitPing {
		opts = append(opts, zk.WithOmitPing(true))
	}
	if s.verbose {
		opts = append(opts, zk.WithVerbose(true))
	}

	client := zk.New(cfg.IP, opts...)
	if err := client.Connect(); err != nil {
		return fmt.Errorf("gagal terhubung ke %s:%d: %w", cfg.IP, cfg.Port, err)
	}

	s.client = client
	s.config = cfg
	s.connected = true

	// Save to DB
	_ = s.db.SaveDeviceConfig(cfg)

	// Start auto sync if interval is set
	s.startAutoSyncLocked()

	log.Printf("[API Server] Successfully connected to ZKTeco machine at %s:%d", cfg.IP, cfg.Port)
	return nil
}

// Disconnect cleanly disconnects from the machine.
func (s *Server) Disconnect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stopAutoSyncLocked()

	if s.client != nil {
		_ = s.client.Disconnect()
		s.client = nil
	}
	s.connected = false
	log.Printf("[API Server] Disconnected from machine")
	return nil
}

// IsConnected returns current connection status.
func (s *Server) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

func (s *Server) startAutoSyncLocked() {
	s.stopAutoSyncLocked()
	if s.config.AutoSyncIntervalSec <= 0 {
		return
	}

	s.autoSyncStop = make(chan struct{})
	ticker := time.NewTicker(time.Duration(s.config.AutoSyncIntervalSec) * time.Second)

	go func(stopCh chan struct{}) {
		for {
			select {
			case <-stopCh:
				ticker.Stop()
				return
			case <-ticker.C:
				_, err := s.SyncAll()
				if err != nil {
					log.Printf("[API Server] Auto-sync background error: %v", err)
				}
			}
		}
	}(s.autoSyncStop)
}

func (s *Server) stopAutoSyncLocked() {
	if s.autoSyncStop != nil {
		close(s.autoSyncStop)
		s.autoSyncStop = nil
	}
}

// SyncAll pulls users, biometric templates, and attendance records into SQLite.
func (s *Server) SyncAll() (map[string]interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected || s.client == nil {
		return nil, fmt.Errorf("perangkat tidak terhubung")
	}

	_ = s.client.DisableDevice()
	defer s.client.EnableDevice()

	userMap := make(map[string]string)
	uidToUserID := make(map[int]string)

	// 1. Users
	users, err := s.client.GetUsers()
	if err == nil {
		_ = s.db.SaveUsersBatch(users)
		for _, u := range users {
			userMap[u.UserID] = u.Name
			uidToUserID[int(u.UID)] = u.UserID
		}
	}

	// 2. Templates
	templates, _ := s.client.GetTemplates()
	savedTemplates := 0
	if len(templates) > 0 {
		savedTemplates, _ = s.db.SaveTemplatesBatch(templates, uidToUserID)
	}

	// 3. Attendance Logs
	records, err := s.client.GetAttendance()
	if err != nil {
		_ = s.db.LogSync(storage.SyncRecord{
			DeviceIP:     s.config.IP,
			TotalRecords: 0,
			NewRecords:   0,
			Status:       "failed",
			ErrorMessage: err.Error(),
		})
		return nil, err
	}

	inserted, err := s.db.SaveAttendanceBatch(records, userMap, s.config.IP, "api_sync")
	status := "success"
	errMsg := ""
	if err != nil {
		status = "failed"
		errMsg = err.Error()
	}

	_ = s.db.LogSync(storage.SyncRecord{
		DeviceIP:     s.config.IP,
		TotalRecords: len(records),
		NewRecords:   inserted,
		Status:       status,
		ErrorMessage: errMsg,
	})

	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total_users":     len(users),
		"saved_templates": savedTemplates,
		"device_records":  len(records),
		"new_records":     inserted,
		"skipped_records": len(records) - inserted,
	}, nil
}
