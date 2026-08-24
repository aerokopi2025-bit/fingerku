package main

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"fingerku/storage"
	"fingerku/zk"
)

// DeviceConfig holds the connection parameters for the ZKTeco device.
type DeviceConfig struct {
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Password int    `json:"password"`
	UDP      bool   `json:"udp"`
	OmitPing bool   `json:"omit_ping"`
	Mock     bool   `json:"mock"`
}

// ConnectionStatus reports the live state of the device connection.
type ConnectionStatus struct {
	Connected      bool           `json:"connected"`
	Config         DeviceConfig   `json:"config"`
	ConnectedSince *time.Time     `json:"connected_since,omitempty"`
	UptimeSeconds  int64          `json:"uptime_seconds"`
	LiveActive     bool           `json:"live_active"`
	DeviceInfo     *zk.DeviceInfo `json:"device_info,omitempty"`
	DeviceTime     *time.Time     `json:"device_time,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
	DatabaseStats  *storage.AttendanceStats `json:"db_stats,omitempty"`
}

// EventMessage is broadcast to SSE clients.
type EventMessage struct {
	Type      string      `json:"type"` // "punch", "status", "log", "device", "sync"
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// PunchEvent represents a live attendance punch with extra metadata.
type PunchEvent struct {
	UID        int       `json:"uid"`
	UserID     string    `json:"user_id"`
	UserName   string    `json:"user_name"`
	Timestamp  time.Time `json:"timestamp"`
	Status     int       `json:"status"`
	StatusName string    `json:"status_name"`
	Punch      int       `json:"punch"`
}

// DeviceManager coordinates all interactions with the physical or mock ZKTeco device.
type DeviceManager struct {
	mu           sync.Mutex
	client       *zk.Client
	config       DeviceConfig
	connected    bool
	connectTime  time.Time
	lastError    string
	deviceInfo   *zk.DeviceInfo
	db           *storage.DB

	// Live Capture
	liveCancel   context.CancelFunc
	liveActive   bool

	// SSE Subscriptions
	subMu        sync.RWMutex
	subscribers  map[chan EventMessage]bool

	// Mock state for offline testing
	mockUsers      []zk.User
	mockAttendance []zk.Attendance
	mockTemplates  []zk.Finger
	mockSizes      zk.Sizes
	mockCancel     context.CancelFunc
}

// NewDeviceManager initializes the manager with SQLite DB storage.
func NewDeviceManager(cfg DeviceConfig, db *storage.DB) *DeviceManager {
	dm := &DeviceManager{
		config:      cfg,
		db:          db,
		subscribers: make(map[chan EventMessage]bool),
	}
	dm.initMockData()
	return dm
}

// initMockData populates sample data for Demo mode.
func (m *DeviceManager) initMockData() {
	m.mockUsers = []zk.User{
		{UID: 1, UserID: "1001", Name: "Ahmad Fauzi", Privilege: zk.UserAdmin, Password: "", GroupID: "1", Card: 1048291},
		{UID: 2, UserID: "1002", Name: "Siti Rahma", Privilege: zk.UserDefault, Password: "", GroupID: "1", Card: 2049182},
		{UID: 3, UserID: "1003", Name: "Budi Santoso", Privilege: zk.UserManager, Password: "", GroupID: "2", Card: 3048192},
		{UID: 4, UserID: "1004", Name: "Dewi Lestari", Privilege: zk.UserDefault, Password: "", GroupID: "2", Card: 4092817},
		{UID: 5, UserID: "1005", Name: "Rudi Hermawan", Privilege: zk.UserEnroller, Password: "", GroupID: "1", Card: 5091823},
		{UID: 6, UserID: "1006", Name: "Maya Putri", Privilege: zk.UserDefault, Password: "", GroupID: "3", Card: 6091824},
		{UID: 7, UserID: "1007", Name: "Eko Prasetyo", Privilege: zk.UserDefault | 1, Password: "", GroupID: "3", Card: 7091825},
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	m.mockAttendance = []zk.Attendance{
		{UID: 1, UserID: "1001", Timestamp: today.Add(7*time.Hour + 45*time.Minute + 12*time.Second), Status: 0, Punch: 0},
		{UID: 2, UserID: "1002", Timestamp: today.Add(7*time.Hour + 52*time.Minute + 40*time.Second), Status: 0, Punch: 0},
		{UID: 3, UserID: "1003", Timestamp: today.Add(8*time.Hour + 02*time.Minute + 15*time.Second), Status: 0, Punch: 0},
		{UID: 4, UserID: "1004", Timestamp: today.Add(8*time.Hour + 14*time.Minute + 22*time.Second), Status: 0, Punch: 0},
		{UID: 5, UserID: "1005", Timestamp: today.Add(8*time.Hour + 28*time.Minute + 05*time.Second), Status: 0, Punch: 0},
		{UID: 6, UserID: "1006", Timestamp: today.Add(8*time.Hour + 35*time.Minute + 48*time.Second), Status: 0, Punch: 0},
		{UID: 1, UserID: "1001", Timestamp: today.Add(12*time.Hour + 05*time.Minute + 10*time.Second), Status: 2, Punch: 0},
		{UID: 1, UserID: "1001", Timestamp: today.Add(12*time.Hour + 58*time.Minute + 30*time.Second), Status: 3, Punch: 0},
		// Past day
		{UID: 1, UserID: "1001", Timestamp: today.Add(-24*time.Hour + 7*time.Hour + 50*time.Minute), Status: 0, Punch: 0},
		{UID: 1, UserID: "1001", Timestamp: today.Add(-24*time.Hour + 17*time.Hour + 05*time.Minute), Status: 1, Punch: 0},
		{UID: 2, UserID: "1002", Timestamp: today.Add(-24*time.Hour + 7*time.Hour + 58*time.Minute), Status: 0, Punch: 0},
		{UID: 2, UserID: "1002", Timestamp: today.Add(-24*time.Hour + 17*time.Hour + 10*time.Minute), Status: 1, Punch: 0},
		{UID: 3, UserID: "1003", Timestamp: today.Add(-24*time.Hour + 8*time.Hour + 00*time.Minute), Status: 0, Punch: 0},
		{UID: 3, UserID: "1003", Timestamp: today.Add(-24*time.Hour + 17*time.Hour + 02*time.Minute), Status: 1, Punch: 0},
	}

	m.mockTemplates = []zk.Finger{
		{UID: 1, FID: 6, Valid: 1, Size: 560, Template: []byte("ZKTEMP_MOCK_01_RIGHT_INDEX_FINGERPRINT_DATA_SAMPLE")},
		{UID: 1, FID: 7, Valid: 1, Size: 560, Template: []byte("ZKTEMP_MOCK_01_RIGHT_THUMB_FINGERPRINT_DATA_SAMPLE")},
		{UID: 2, FID: 6, Valid: 1, Size: 560, Template: []byte("ZKTEMP_MOCK_02_RIGHT_INDEX_FINGERPRINT_DATA_SAMPLE")},
		{UID: 3, FID: 6, Valid: 1, Size: 560, Template: []byte("ZKTEMP_MOCK_03_RIGHT_INDEX_FINGERPRINT_DATA_SAMPLE")},
		{UID: 4, FID: 6, Valid: 1, Size: 560, Template: []byte("ZKTEMP_MOCK_04_RIGHT_INDEX_FINGERPRINT_DATA_SAMPLE")},
		{UID: 5, FID: 6, Valid: 1, Size: 560, Template: []byte("ZKTEMP_MOCK_05_RIGHT_INDEX_FINGERPRINT_DATA_SAMPLE")},
		{UID: 6, FID: 6, Valid: 1, Size: 560, Template: []byte("ZKTEMP_MOCK_06_RIGHT_INDEX_FINGERPRINT_DATA_SAMPLE")},
	}

	m.mockSizes = zk.Sizes{
		Users:            len(m.mockUsers),
		UsersCap:         1000,
		UsersAvailable:   993,
		Fingers:          len(m.mockTemplates),
		FingersCap:       2000,
		FingersAvailable: 1993,
		Records:          len(m.mockAttendance),
		RecordsCap:       50000,
		RecordsAvailable: 49986,
		Cards:            7,
		Faces:            2,
		FacesCap:         500,
	}

	// Auto-seed mock data into SQLite if DB is empty
	if m.db != nil {
		stats, err := m.db.GetAttendanceStats()
		if err == nil && stats.TotalRecords == 0 {
			userMap := make(map[string]string)
			for _, u := range m.mockUsers {
				userMap[u.UserID] = u.Name
			}
			_ = m.db.SaveUsersBatch(m.mockUsers)
			_, _ = m.db.SaveAttendanceBatch(m.mockAttendance, userMap, "127.0.0.1", "initial_seed")
		}
	}
}

// Subscribe adds an SSE channel listener.
func (m *DeviceManager) Subscribe() chan EventMessage {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	ch := make(chan EventMessage, 50)
	m.subscribers[ch] = true
	return ch
}

// Unsubscribe removes an SSE channel listener.
func (m *DeviceManager) Unsubscribe(ch chan EventMessage) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	delete(m.subscribers, ch)
	close(ch)
}

// Broadcast sends an event to all connected web clients.
func (m *DeviceManager) Broadcast(event EventMessage) {
	m.subMu.RLock()
	defer m.subMu.RUnlock()
	for ch := range m.subscribers {
		select {
		case ch <- event:
		default:
			// Client channel full, skip to avoid blocking
		}
	}
}

// Connect connects to the ZKTeco device or activates mock mode.
func (m *DeviceManager) Connect(cfg DeviceConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If already connected, disconnect first
	if m.connected {
		m.disconnectInternal()
	}

	m.config = cfg

	if cfg.Mock {
		m.connected = true
		m.connectTime = time.Now()
		m.lastError = ""
		m.deviceInfo = &zk.DeviceInfo{
			FirmwareVersion: "Ver 6.60 Dec 15 2023 (MOCK)",
			SerialNumber:    "ZK-FINGERKU-99881",
			Platform:        "ZEM560_TFT_DEMO",
			MAC:             "00:17:61:A8:12:FE",
			DeviceName:      "ZKTeco K40 / iClock (Demo Mode)",
			FaceVersion:     7,
			FPVersion:       10,
			Network: zk.NetworkParams{
				IP:      cfg.IP,
				Mask:    "255.255.255.0",
				Gateway: "192.168.1.1",
			},
			Sizes: m.mockSizes,
		}

		m.startMockLiveCapture()
		m.Broadcast(EventMessage{
			Type:      "status",
			Timestamp: time.Now(),
			Data:      map[string]interface{}{"connected": true, "mock": true},
		})
		return nil
	}

	// Real ZKTeco Hardware connection
	client := zk.New(cfg.IP,
		zk.WithPort(cfg.Port),
		zk.WithPassword(cfg.Password),
		zk.WithForceUDP(cfg.UDP),
		zk.WithOmitPing(cfg.OmitPing),
		zk.WithTimeout(8*time.Second),
	)

	if err := client.Connect(); err != nil {
		m.lastError = err.Error()
		return fmt.Errorf("gagal terhubung ke mesin %s:%d : %w", cfg.IP, cfg.Port, err)
	}

	info, err := client.GetDeviceInfo()
	if err != nil {
		// Non-fatal, try to continue
		info = &zk.DeviceInfo{
			DeviceName: "ZKTeco Biometric Device",
			Sizes:      client.Sizes,
		}
	}

	m.client = client
	m.connected = true
	m.connectTime = time.Now()
	m.deviceInfo = info
	m.lastError = ""

	// Start real live capture in background
	m.startRealLiveCapture()

	m.Broadcast(EventMessage{
		Type:      "status",
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"connected": true, "mock": false},
	})

	return nil
}

// Disconnect closes connection.
func (m *DeviceManager) Disconnect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disconnectInternal()
	m.Broadcast(EventMessage{
		Type:      "status",
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"connected": false},
	})
	return nil
}

func (m *DeviceManager) disconnectInternal() {
	if m.liveCancel != nil {
		m.liveCancel()
		m.liveCancel = nil
	}
	if m.mockCancel != nil {
		m.mockCancel()
		m.mockCancel = nil
	}
	m.liveActive = false

	if m.client != nil {
		_ = m.client.Disconnect()
		m.client = nil
	}
	m.connected = false
}

// startRealLiveCapture initializes live streaming from real machine.
func (m *DeviceManager) startRealLiveCapture() {
	if m.client == nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.liveCancel = cancel
	m.liveActive = true

	// Live capture needs a dedicated client connection to avoid blocking command requests
	go func() {
		defer func() {
			m.mu.Lock()
			m.liveActive = false
			m.mu.Unlock()
		}()

		// Create a secondary client for event listening
		liveClient := zk.New(m.config.IP,
			zk.WithPort(m.config.Port),
			zk.WithPassword(m.config.Password),
			zk.WithForceUDP(m.config.UDP),
			zk.WithOmitPing(true),
			zk.WithTimeout(10*time.Second),
		)

		if err := liveClient.Connect(); err != nil {
			m.Broadcast(EventMessage{
				Type:      "log",
				Timestamp: time.Now(),
				Data:      fmt.Sprintf("Live capture secondary connection failed: %v", err),
			})
			return
		}
		defer liveClient.Disconnect()

		events, errs := liveClient.LiveCapture(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-errs:
				if ok && err != nil {
					m.Broadcast(EventMessage{
						Type:      "log",
						Timestamp: time.Now(),
						Data:      fmt.Sprintf("Live capture stream error: %v", err),
					})
				}
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				userName := m.resolveUserName(ev.UserID)
				punchEvent := PunchEvent{
					UID:        ev.UID,
					UserID:     ev.UserID,
					UserName:   userName,
					Timestamp:  ev.Timestamp,
					Status:     ev.Status,
					StatusName: ev.StatusName(),
					Punch:      ev.Punch,
				}

				// Persist into SQLite DB automatically!
				if m.db != nil {
					_, _ = m.db.SaveSinglePunch(ev, userName, m.config.IP, "live_stream")
				}

				m.Broadcast(EventMessage{
					Type:      "punch",
					Timestamp: time.Now(),
					Data:      punchEvent,
				})
			}
		}
	}()
}

// startMockLiveCapture generates periodic simulated punch events for demo testing.
func (m *DeviceManager) startMockLiveCapture() {
	ctx, cancel := context.WithCancel(context.Background())
	m.mockCancel = cancel
	m.liveActive = true

	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.mu.Lock()
				if !m.connected || !m.config.Mock {
					m.mu.Unlock()
					return
				}
				// Pick a random mock user
				if len(m.mockUsers) > 0 {
					idx := rand.Intn(len(m.mockUsers))
					u := m.mockUsers[idx]
					status := rand.Intn(2) // 0: Check-In, 1: Check-Out
					punch := 0
					now := time.Now()

					att := zk.Attendance{
						UID:       int(u.UID),
						UserID:    u.UserID,
						Timestamp: now,
						Status:    status,
						Punch:     punch,
					}
					m.mockAttendance = append([]zk.Attendance{att}, m.mockAttendance...)
					m.mockSizes.Records++
					m.mockSizes.RecordsAvailable--

					punchEvent := PunchEvent{
						UID:        int(u.UID),
						UserID:     u.UserID,
						UserName:   u.Name,
						Timestamp:  now,
						Status:     status,
						StatusName: att.StatusName(),
						Punch:      punch,
					}
					m.mu.Unlock()

					// Save into SQLite DB
					if m.db != nil {
						_, _ = m.db.SaveSinglePunch(att, u.Name, "127.0.0.1", "mock_live")
					}

					m.Broadcast(EventMessage{
						Type:      "punch",
						Timestamp: time.Now(),
						Data:      punchEvent,
					})
				} else {
					m.mu.Unlock()
				}
			}
		}
	}()
}

// resolveUserName attempts to lookup user's name from cached or device users.
func (m *DeviceManager) resolveUserName(userID string) string {
	users, err := m.GetUsers()
	if err == nil {
		for _, u := range users {
			if u.UserID == userID {
				return u.Name
			}
		}
	}
	return "User " + userID
}

// TriggerDemoPunch simulates an immediate punch event.
func (m *DeviceManager) TriggerDemoPunch(userID string, status int) (*PunchEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var targetUser *zk.User
	if len(m.mockUsers) > 0 {
		for i := range m.mockUsers {
			if m.mockUsers[i].UserID == userID || strconv.Itoa(int(m.mockUsers[i].UID)) == userID {
				targetUser = &m.mockUsers[i]
				break
			}
		}
		if targetUser == nil {
			targetUser = &m.mockUsers[rand.Intn(len(m.mockUsers))]
		}
	} else {
		targetUser = &zk.User{UID: 1, UserID: "1001", Name: "Pegawai Uji Coba", Privilege: 0}
	}

	now := time.Now()
	att := zk.Attendance{
		UID:       int(targetUser.UID),
		UserID:    targetUser.UserID,
		Timestamp: now,
		Status:    status,
		Punch:     0,
	}

	if m.config.Mock {
		m.mockAttendance = append([]zk.Attendance{att}, m.mockAttendance...)
		m.mockSizes.Records++
	}

	punchEvent := &PunchEvent{
		UID:        int(targetUser.UID),
		UserID:     targetUser.UserID,
		UserName:   targetUser.Name,
		Timestamp:  now,
		Status:     status,
		StatusName: att.StatusName(),
		Punch:      0,
	}

	// Persist to SQLite
	if m.db != nil {
		_, _ = m.db.SaveSinglePunch(att, targetUser.Name, "127.0.0.1", "demo_punch")
	}

	m.Broadcast(EventMessage{
		Type:      "punch",
		Timestamp: time.Now(),
		Data:      punchEvent,
	})

	return punchEvent, nil
}

// SyncToDatabase downloads all users and attendance from device and saves to SQLite.
func (m *DeviceManager) SyncToDatabase() (map[string]interface{}, error) {
	if m.db == nil {
		return nil, fmt.Errorf("database SQLite tidak diinisialisasi")
	}

	users, err := m.GetUsers()
	if err != nil {
		return nil, fmt.Errorf("gagal membaca pengguna dari mesin: %w", err)
	}

	userMap := make(map[string]string)
	for _, u := range users {
		userMap[u.UserID] = u.Name
	}
	_ = m.db.SaveUsersBatch(users)

	records, err := m.GetAttendance()
	if err != nil {
		return nil, fmt.Errorf("gagal membaca log presensi dari mesin: %w", err)
	}

	deviceIP := m.config.IP
	if m.config.Mock {
		deviceIP = "127.0.0.1 (Mock)"
	}

	inserted, err := m.db.SaveAttendanceBatch(records, userMap, deviceIP, "web_sync")
	if err != nil {
		_ = m.db.LogSync(storage.SyncRecord{
			DeviceIP:     deviceIP,
			TotalRecords: len(records),
			NewRecords:   inserted,
			Status:       "failed",
			ErrorMessage: err.Error(),
		})
		return nil, err
	}

	_ = m.db.LogSync(storage.SyncRecord{
		DeviceIP:     deviceIP,
		TotalRecords: len(records),
		NewRecords:   inserted,
		Status:       "success",
	})

	stats, _ := m.db.GetAttendanceStats()

	m.Broadcast(EventMessage{
		Type:      "sync",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"device_ip":    deviceIP,
			"total":        len(records),
			"new_inserted": inserted,
			"skipped":      len(records) - inserted,
		},
	})

	return map[string]interface{}{
		"total_device_records": len(records),
		"new_records":          inserted,
		"skipped_duplicates":   len(records) - inserted,
		"db_stats":             stats,
	}, nil
}

// GetStatus returns the current connection and device summary.
func (m *DeviceManager) GetStatus() ConnectionStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	status := ConnectionStatus{
		Connected:  m.connected,
		Config:     m.config,
		LiveActive: m.liveActive,
		DeviceInfo: m.deviceInfo,
		LastError:  m.lastError,
	}

	if m.connected {
		status.ConnectedSince = &m.connectTime
		status.UptimeSeconds = int64(time.Since(m.connectTime).Seconds())
	}

	if m.db != nil {
		stats, err := m.db.GetAttendanceStats()
		if err == nil {
			status.DatabaseStats = &stats
		}
	}

	return status
}

// GetDeviceInfo retrieves complete hardware and storage info.
func (m *DeviceManager) GetDeviceInfo() (*zk.DeviceInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return nil, fmt.Errorf("perangkat tidak terhubung")
	}

	if m.config.Mock {
		m.mockSizes.Users = len(m.mockUsers)
		m.mockSizes.Fingers = len(m.mockTemplates)
		m.mockSizes.Records = len(m.mockAttendance)
		if m.deviceInfo != nil {
			m.deviceInfo.Sizes = m.mockSizes
		}
		return m.deviceInfo, nil
	}

	info, err := m.client.GetDeviceInfo()
	if err != nil {
		return nil, err
	}
	m.deviceInfo = info
	return info, nil
}

// GetUsers returns all enrolled users.
func (m *DeviceManager) GetUsers() ([]zk.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return nil, fmt.Errorf("perangkat tidak terhubung")
	}

	if m.config.Mock {
		res := make([]zk.User, len(m.mockUsers))
		copy(res, m.mockUsers)
		return res, nil
	}

	_ = m.client.DisableDevice()
	defer m.client.EnableDevice()

	return m.client.GetUsers()
}

// SaveUser creates or updates a user profile.
func (m *DeviceManager) SaveUser(u zk.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return fmt.Errorf("perangkat tidak terhubung")
	}

	if m.config.Mock {
		found := false
		for i, existing := range m.mockUsers {
			if existing.UID == u.UID || (u.UserID != "" && existing.UserID == u.UserID) {
				m.mockUsers[i] = u
				found = true
				break
			}
		}
		if !found {
			if u.UID == 0 {
				var maxUID uint16 = 0
				for _, eu := range m.mockUsers {
					if eu.UID > maxUID {
						maxUID = eu.UID
					}
				}
				u.UID = maxUID + 1
			}
			if u.UserID == "" {
				u.UserID = strconv.Itoa(int(u.UID))
			}
			m.mockUsers = append(m.mockUsers, u)
			m.mockSizes.Users++
			m.mockSizes.UsersAvailable--
		}
		if m.db != nil {
			_ = m.db.SaveUsersBatch([]zk.User{u})
		}
		return nil
	}

	_ = m.client.DisableDevice()
	defer m.client.EnableDevice()

	if err := m.client.SetUser(u); err != nil {
		return err
	}

	if m.db != nil {
		_ = m.db.SaveUsersBatch([]zk.User{u})
	}

	return nil
}

// DeleteUser deletes user by UID or UserID.
func (m *DeviceManager) DeleteUser(uid uint16, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return fmt.Errorf("perangkat tidak terhubung")
	}

	if m.config.Mock {
		var newUsers []zk.User
		deleted := false
		for _, u := range m.mockUsers {
			if (uid > 0 && u.UID == uid) || (userID != "" && u.UserID == userID) {
				deleted = true
				continue
			}
			newUsers = append(newUsers, u)
		}
		if !deleted {
			return fmt.Errorf("user tidak ditemukan")
		}
		m.mockUsers = newUsers
		m.mockSizes.Users = len(m.mockUsers)
		return nil
	}

	_ = m.client.DisableDevice()
	defer m.client.EnableDevice()

	return m.client.DeleteUser(uid, userID)
}

// ClearAdmin resets administrator privileges.
func (m *DeviceManager) ClearAdmin() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return fmt.Errorf("perangkat tidak terhubung")
	}

	if m.config.Mock {
		for i := range m.mockUsers {
			m.mockUsers[i].Privilege = zk.UserDefault
		}
		return nil
	}

	_ = m.client.DisableDevice()
	defer m.client.EnableDevice()

	return m.client.ClearAdmin()
}

// GetAttendance retrieves all attendance logs from device.
func (m *DeviceManager) GetAttendance() ([]zk.Attendance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return nil, fmt.Errorf("perangkat tidak terhubung")
	}

	if m.config.Mock {
		res := make([]zk.Attendance, len(m.mockAttendance))
		copy(res, m.mockAttendance)
		return res, nil
	}

	_ = m.client.DisableDevice()
	defer m.client.EnableDevice()

	return m.client.GetAttendance()
}

// GetTemplates retrieves all fingerprint biometric templates.
func (m *DeviceManager) GetTemplates() ([]zk.Finger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return nil, fmt.Errorf("perangkat tidak terhubung")
	}

	if m.config.Mock {
		res := make([]zk.Finger, len(m.mockTemplates))
		copy(res, m.mockTemplates)
		return res, nil
	}

	_ = m.client.DisableDevice()
	defer m.client.EnableDevice()

	return m.client.GetTemplates()
}

// DeleteTemplate deletes biometric template for user.
func (m *DeviceManager) DeleteTemplate(uid uint16, fid int, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return fmt.Errorf("perangkat tidak terhubung")
	}

	if m.config.Mock {
		var newTmps []zk.Finger
		deleted := false
		for _, f := range m.mockTemplates {
			if uint16(f.UID) == uid && f.FID == fid {
				deleted = true
				continue
			}
			newTmps = append(newTmps, f)
		}
		if !deleted {
			return fmt.Errorf("template tidak ditemukan")
		}
		m.mockTemplates = newTmps
		m.mockSizes.Fingers = len(m.mockTemplates)
		return nil
	}

	_ = m.client.DisableDevice()
	defer m.client.EnableDevice()

	return m.client.DeleteUserTemplate(uid, fid, userID)
}

// Unlock opens the door relay.
func (m *DeviceManager) Unlock(seconds int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return fmt.Errorf("perangkat tidak terhubung")
	}

	if m.config.Mock {
		m.Broadcast(EventMessage{
			Type:      "device",
			Timestamp: time.Now(),
			Data:      map[string]interface{}{"action": "unlock", "duration": seconds, "status": "ok"},
		})
		return nil
	}

	return m.client.Unlock(seconds)
}

// SyncTime updates the RTC clock with server timestamp.
func (m *DeviceManager) SyncTime(t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return fmt.Errorf("perangkat tidak terhubung")
	}

	if m.config.Mock {
		return nil
	}

	return m.client.SetTime(t)
}

// TestVoice triggers voice prompt on the speaker.
func (m *DeviceManager) TestVoice(index int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return fmt.Errorf("perangkat tidak terhubung")
	}

	if m.config.Mock {
		return nil
	}

	return m.client.TestVoice(index)
}

// WriteLCD sends text to LCD.
func (m *DeviceManager) WriteLCD(line int, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return fmt.Errorf("perangkat tidak terhubung")
	}

	if m.config.Mock {
		return nil
	}

	return m.client.WriteLCD(line, text)
}

// ClearLCD clears text on LCD.
func (m *DeviceManager) ClearLCD() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return fmt.Errorf("perangkat tidak terhubung")
	}

	if m.config.Mock {
		return nil
	}

	return m.client.ClearLCD()
}

// Restart reboots the device.
func (m *DeviceManager) Restart() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return fmt.Errorf("perangkat tidak terhubung")
	}

	if m.config.Mock {
		m.disconnectInternal()
		return nil
	}

	err := m.client.Restart()
	m.disconnectInternal()
	return err
}

// PowerOff turns off the device.
func (m *DeviceManager) PowerOff() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return fmt.Errorf("perangkat tidak terhubung")
	}

	if m.config.Mock {
		m.disconnectInternal()
		return nil
	}

	err := m.client.PowerOff()
	m.disconnectInternal()
	return err
}
