package api

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"golang.org/x/time/rate"

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
	db     *storage.DB
	client *zk.Client
	config storage.DeviceConfig

	// connMu protects client/connected/config.
	connMu    sync.RWMutex
	connected bool

	// syncMu serializes SyncAll so only one sync runs at a time.
	syncMu sync.Mutex

	broker       *SSEBroker
	startTime    time.Time
	autoSyncStop chan struct{}
	liveStop     chan struct{}
	verbose      bool

	// Rate limiters per IP for hardware-mutating actions.
	rlMu           sync.Mutex
	actionLimiters map[string]*rate.Limiter

	// Configurable CORS and auth.
	corsOrigins []string
	apiToken    string

	liveBackoff time.Duration
}

// NewServer creates a new API Server with SQLite backing.
func NewServer(db *storage.DB, verbose bool) (*Server, error) {
	cfg, err := db.GetDeviceConfig()
	if err != nil {
		cfg = storage.DefaultDeviceConfig()
	}

	// CORS allowlist from env CORS_ORIGINS (comma-separated), default wildcard.
	origins := []string{"*"}
	if v := strings.TrimSpace(os.Getenv("CORS_ORIGINS")); v != "" {
		parts := strings.Split(v, ",")
		origins = origins[:0]
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				origins = append(origins, p)
			}
		}
		if len(origins) == 0 {
			origins = []string{"*"}
		}
	}

	s := &Server{
		db:             db,
		config:         cfg,
		broker:         newSSEBroker(),
		startTime:      time.Now(),
		verbose:        verbose,
		actionLimiters: make(map[string]*rate.Limiter),
		corsOrigins:    origins,
		apiToken:       strings.TrimSpace(os.Getenv("API_TOKEN")),
		liveBackoff:    2 * time.Second,
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

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   s.corsOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Health endpoint (no auth)
	r.Get("/health", s.handleHealth)

	// API v1
	r.Route("/api/v1", func(r chi.Router) {
		// System & Config
		r.Get("/status", s.handleStatus)
		r.Get("/config", s.handleGetConfig)
		r.Put("/config", s.handleUpdateConfig)

		// Device Connection & Hardware Controls (mutating routes require auth if API_TOKEN set)
		r.Route("/device", func(r chi.Router) {
			r.Post("/connect", s.handleConnect)
			r.Post("/disconnect", s.handleDisconnect)
			r.Get("/info", s.handleDeviceInfo)
			r.With(s.requireAuth).Post("/unlock", s.handleUnlock)
			r.Post("/synctime", s.handleSyncTime)
			r.With(s.requireAuth).Post("/voice", s.handleVoice)
			r.With(s.requireAuth).Post("/restart", s.handleRestart)
			r.With(s.requireAuth).Post("/poweroff", s.handlePowerOff)
		})

		// Users & Biometric Fingerprints
		r.Route("/users", func(r chi.Router) {
			r.Get("/", s.handleGetUsers)
			r.With(s.requireAuth).Post("/", s.handleSaveUser)
			r.Get("/{id}", s.handleGetUserByID)
			r.With(s.requireAuth).Delete("/{id}", s.handleDeleteUserByID)
			r.Get("/{id}/templates", s.handleGetUserTemplates)
		})

		// Fingerprint Templates
		r.Get("/templates", s.handleGetTemplates)

		// Attendance Logs & Statistics
		r.Route("/attendance", func(r chi.Router) {
			r.Get("/", s.handleGetAttendance)
			r.Get("/stats", s.handleGetAttendanceStats)
			r.Get("/machine", s.handleGetMachineAttendance)
			r.With(s.requireAuth).Delete("/machine", s.handleClearMachineAttendance)
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

// validateDeviceConfig checks IP/hostname, port, password bounds.
// Hostname is accepted if it looks like a valid DNS name; IP is validated strictly.
func validateDeviceConfig(cfg storage.DeviceConfig) error {
	if strings.TrimSpace(cfg.IP) == "" {
		return fmt.Errorf("ip/hostname is required")
	}
	trimmed := strings.TrimSpace(cfg.IP)
	if net.ParseIP(trimmed) == nil {
		// Allow hostname: RFC 1123 label format, plus simple lookup fallback
		if !isValidHostname(trimmed) {
			// Try DNS lookup as fallback before rejecting (covers e.g. finger.local)
			if _, err := net.LookupHost(trimmed); err != nil {
				return fmt.Errorf("ip %q is not a valid IP address or resolvable hostname", cfg.IP)
			}
		}
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if cfg.Password < 0 {
		return fmt.Errorf("password must be >= 0")
	}
	if cfg.AutoSyncIntervalSec < 0 {
		return fmt.Errorf("auto_sync_interval_sec must be >= 0")
	}
	return nil
}

func isValidHostname(h string) bool {
	if len(h) == 0 || len(h) > 253 {
		return false
	}
	if strings.Contains(h, " ") {
		return false
	}
	labels := strings.Split(h, ".")
	for _, l := range labels {
		if len(l) == 0 || len(l) > 63 {
			return false
		}
		if l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, ch := range l {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}

func (s *Server) getClient() (*zk.Client, bool) {
	s.connMu.RLock()
	defer s.connMu.RUnlock()
	return s.client, s.connected
}

func (s *Server) getConfig() storage.DeviceConfig {
	s.connMu.RLock()
	defer s.connMu.RUnlock()
	return s.config
}

// Connect establishes connection with the ZKTeco hardware.
func (s *Server) Connect(cfg storage.DeviceConfig) error {
	if err := validateDeviceConfig(cfg); err != nil {
		return err
	}

	// Disconnect previous if any (without holding connMu across dial)
	s.connMu.Lock()
	if s.connected && s.client != nil {
		c := s.client
		s.connMu.Unlock()
		_ = c.Disconnect()
		s.connMu.Lock()
		s.connected = false
		s.client = nil
	}
	s.connMu.Unlock()

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

	s.connMu.Lock()
	s.client = client
	s.config = cfg
	s.connected = true
	s.connMu.Unlock()

	// Save to DB
	_ = s.db.SaveDeviceConfig(cfg)

	// Start background workers
	s.startAutoSyncLocked()
	s.startLiveStreamLocked()

	log.Printf("[API Server] Successfully connected to ZKTeco machine at %s:%d", cfg.IP, cfg.Port)
	return nil
}

// Disconnect cleanly disconnects from the machine.
func (s *Server) Disconnect() error {
	s.stopAutoSyncLocked()
	s.stopLiveStreamLocked()

	s.connMu.Lock()
	defer s.connMu.Unlock()

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
	s.connMu.RLock()
	defer s.connMu.RUnlock()
	return s.connected
}

func (s *Server) startAutoSyncLocked() {
	s.stopAutoSyncLocked()
	cfg := s.getConfig()
	if cfg.AutoSyncIntervalSec <= 0 {
		return
	}

	s.autoSyncStop = make(chan struct{})
	ticker := time.NewTicker(time.Duration(cfg.AutoSyncIntervalSec) * time.Second)

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

func (s *Server) startLiveStreamLocked() {
	s.stopLiveStreamLocked()
	s.liveStop = make(chan struct{})
	stopCh := s.liveStop
	backoff := s.liveBackoff
	if backoff <= 0 {
		backoff = 2 * time.Second
	}

	go func() {
		// Retry loop with backoff — handles reconnect and stale client.
		for {
			select {
			case <-stopCh:
				return
			default:
			}

			client, ok := s.getClient()
			if !ok || client == nil {
				// Wait for reconnect
				select {
				case <-stopCh:
					return
				case <-time.After(backoff):
					continue
				}
			}

			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				<-stopCh
				cancel()
			}()

			events, errs := client.LiveCapture(ctx)

		inner:
			for {
				select {
				case <-stopCh:
					cancel()
					return
				case <-ctx.Done():
					break inner
				case err, ok := <-errs:
					if ok && err != nil {
						log.Printf("[API Server] LiveCapture error: %v", err)
					}
					// error channel closed or error received — break to reconnect
					if !ok {
						break inner
					}
				case att, ok := <-events:
					if !ok {
						break inner
					}
					// Persist live punch (deduplicated by UNIQUE constraint)
					userName := ""
					if u, err := s.db.GetUser(att.UserID); err == nil && u != nil {
						userName = u.Name
					}
					cfg := s.getConfig()
					if _, err := s.db.SaveSinglePunch(att, userName, cfg.IP, "live_stream"); err != nil {
						log.Printf("[API Server] SaveSinglePunch failed: %v", err)
					}
					// Broadcast to SSE subscribers
					if payload, err := json.Marshal(att); err == nil {
						s.broker.broadcast(payload)
					}
				}
			}
			cancel()

			// If stopped, exit; otherwise backoff and retry (reconnect case)
			select {
			case <-stopCh:
				return
			case <-time.After(backoff):
			}
		}
	}()
}

func (s *Server) stopLiveStreamLocked() {
	if s.liveStop != nil {
		close(s.liveStop)
		s.liveStop = nil
	}
}

// SyncAll pulls users, biometric templates, and attendance records into SQLite.
// Only one sync may run concurrently (syncMu); connection state uses connMu.
func (s *Server) SyncAll() (map[string]interface{}, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	client, ok := s.getClient()
	if !ok || client == nil {
		return nil, fmt.Errorf("perangkat tidak terhubung")
	}
	cfg := s.getConfig()

	_ = client.DisableDevice()
	defer client.EnableDevice()

	userMap := make(map[string]string)
	uidToUserID := make(map[int]string)

	// 1. Users
	users, err := client.GetUsers()
	if err == nil {
		_ = s.db.SaveUsersBatch(users)
		for _, u := range users {
			userMap[u.UserID] = u.Name
			uidToUserID[int(u.UID)] = u.UserID
		}
	}

	// 2. Templates
	templates, _ := client.GetTemplates()
	savedTemplates := 0
	if len(templates) > 0 {
		savedTemplates, _ = s.db.SaveTemplatesBatch(templates, uidToUserID)
	}

	// 3. Attendance Logs
	records, err := client.GetAttendance()
	if err != nil {
		_ = s.db.LogSync(storage.SyncRecord{
			DeviceIP:     cfg.IP,
			TotalRecords: 0,
			NewRecords:   0,
			Status:       "failed",
			ErrorMessage: err.Error(),
		})
		return nil, err
	}

	inserted, err := s.db.SaveAttendanceBatch(records, userMap, cfg.IP, "api_sync")
	status := "success"
	errMsg := ""
	if err != nil {
		status = "failed"
		errMsg = err.Error()
	}

	_ = s.db.LogSync(storage.SyncRecord{
		DeviceIP:     cfg.IP,
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

// ClearMachineAttendance removes attendance logs from device RAM.
func (s *Server) ClearMachineAttendance() error {
	client, ok := s.getClient()
	if !ok || client == nil {
		return fmt.Errorf("perangkat tidak terhubung")
	}
	_ = client.DisableDevice()
	defer client.EnableDevice()
	return client.ClearAttendance()
}

// requireAuth enforces Bearer token when API_TOKEN env is set.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.apiToken == "" {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		token := ""
		if strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		} else {
			token = r.Header.Get("X-API-Token")
			if token == "" {
				token = r.URL.Query().Get("token")
			}
		}
		if token != s.apiToken {
			respondError(w, http.StatusUnauthorized, "Unauthorized — valid API token required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Per-IP rate limit helpers.

// actionRate returns the rate limit interval/burst for a device action.
func actionRate(name string) (time.Duration, int) {
	switch name {
	case "unlock":
		return 2 * time.Second, 1
	case "voice":
		return time.Second, 2
	default:
		return time.Second, 3
	}
}

// allowAction enforces a per-IP rate limit for a hardware-mutating action.
func (s *Server) allowAction(r *http.Request, name string) bool {
	ip := clientIP(r)
	every, burst := actionRate(name)
	s.rlMu.Lock()
	defer s.rlMu.Unlock()
	key := name + ":" + ip
	lim, ok := s.actionLimiters[key]
	if !ok {
		lim = rate.NewLimiter(rate.Every(every), burst)
		s.actionLimiters[key] = lim
	}
	return lim.Allow()
}

// clientIP returns the connecting client's IP. Client-supplied headers
// (X-Forwarded-For / X-Real-IP) are not trusted here because they can be
// spoofed to bypass per-IP rate limits. The chi RealIP middleware rewrites
// RemoteAddr using the trusted proxy headers before this runs.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// activeDeviceClient returns the connected client or an error response helper flag.
func (s *Server) activeDeviceClient() (*zk.Client, bool) {
	c, ok := s.getClient()
	if !ok || c == nil {
		return nil, false
	}
	return c, true
}

// validateSeconds clamps relay seconds.
func validateSeconds(v int) int {
	if v <= 0 {
		return 3
	}
	if v > 60 {
		return 60
	}
	return v
}
