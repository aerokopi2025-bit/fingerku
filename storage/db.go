package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"fingerku/zk"

	_ "modernc.org/sqlite"
)

// DB wraps SQLite operations for fingerku.
type DB struct {
	db *sql.DB
	mu sync.RWMutex
}

// Open opens or creates an SQLite database file and runs initial schema migrations.
func Open(path string) (*DB, error) {
	if path == "" {
		path = "fingerku.db"
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	dbInstance := &DB{db: conn}
	if err := dbInstance.initSchema(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to initialize sqlite schema: %w", err)
	}

	return dbInstance, nil
}

// initSchema sets up required tables and indices.
func (d *DB) initSchema() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS attendance_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			uid INTEGER NOT NULL,
			user_id TEXT NOT NULL,
			user_name TEXT,
			timestamp DATETIME NOT NULL,
			status INTEGER NOT NULL,
			status_name TEXT,
			punch INTEGER DEFAULT 0,
			device_ip TEXT,
			source TEXT DEFAULT 'device',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, timestamp, status)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_att_user_id ON attendance_logs(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_att_timestamp ON attendance_logs(timestamp);`,
		`CREATE INDEX IF NOT EXISTS idx_att_status ON attendance_logs(status);`,
		`CREATE TABLE IF NOT EXISTS users (
			uid INTEGER PRIMARY KEY,
			user_id TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			privilege INTEGER DEFAULT 0,
			password TEXT,
			group_id TEXT,
			card INTEGER DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_users_uid ON users(uid);`,
		`CREATE TABLE IF NOT EXISTS finger_templates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			uid INTEGER NOT NULL,
			user_id TEXT,
			fid INTEGER NOT NULL,
			valid INTEGER DEFAULT 1,
			size INTEGER NOT NULL,
			template BLOB NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(uid, fid)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_fingers_uid ON finger_templates(uid);`,
		`CREATE INDEX IF NOT EXISTS idx_fingers_user_id ON finger_templates(user_id);`,
		`CREATE TABLE IF NOT EXISTS sync_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_ip TEXT NOT NULL,
			synced_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			total_records INTEGER NOT NULL,
			new_records INTEGER NOT NULL,
			status TEXT NOT NULL,
			error_message TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS device_config (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			ip TEXT NOT NULL DEFAULT '192.168.1.201',
			port INTEGER NOT NULL DEFAULT 4370,
			password INTEGER NOT NULL DEFAULT 0,
			udp BOOLEAN NOT NULL DEFAULT 0,
			omit_ping BOOLEAN NOT NULL DEFAULT 0,
			auto_connect BOOLEAN NOT NULL DEFAULT 1,
			auto_sync_interval_sec INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`INSERT OR IGNORE INTO device_config (id, ip, port, password, udp, omit_ping, auto_connect, auto_sync_interval_sec)
		VALUES (1, '192.168.1.201', 4370, 0, 0, 0, 1, 0);`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
	}

	for _, query := range queries {
		if _, err := d.db.Exec(query); err != nil {
			return err
		}
	}

	return nil
}

// Close closes the underlying SQLite database connection.
func (d *DB) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.db.Close()
}

// SaveAttendanceBatch inserts a slice of attendance logs from a ZKTeco device, ignoring duplicates.
// Returns the number of newly inserted records.
func (d *DB) SaveAttendanceBatch(records []zk.Attendance, userMap map[string]string, deviceIP string, source string) (int, error) {
	if len(records) == 0 {
		return 0, nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO attendance_logs (uid, user_id, user_name, timestamp, status, status_name, punch, device_ip, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, timestamp, status) DO NOTHING
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	insertedCount := 0
	for _, rec := range records {
		name := ""
		if userMap != nil {
			name = userMap[rec.UserID]
		}
		if name == "" {
			name = fmt.Sprintf("User %s", rec.UserID)
		}

		timeStr := rec.Timestamp.Format("2006-01-02 15:04:05")
		res, err := stmt.Exec(rec.UID, rec.UserID, name, timeStr, rec.Status, rec.StatusName(), rec.Punch, deviceIP, source)
		if err != nil {
			return insertedCount, err
		}
		rowsAff, _ := res.RowsAffected()
		if rowsAff > 0 {
			insertedCount++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return insertedCount, nil
}

// SaveSinglePunch inserts a single punch event (e.g. from live stream or manual action).
func (d *DB) SaveSinglePunch(rec zk.Attendance, userName string, deviceIP string, source string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if userName == "" {
		userName = fmt.Sprintf("User %s", rec.UserID)
	}

	timeStr := rec.Timestamp.Format("2006-01-02 15:04:05")
	res, err := d.db.Exec(`
		INSERT INTO attendance_logs (uid, user_id, user_name, timestamp, status, status_name, punch, device_ip, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, timestamp, status) DO UPDATE SET
			user_name = excluded.user_name,
			source = excluded.source
	`, rec.UID, rec.UserID, userName, timeStr, rec.Status, rec.StatusName(), rec.Punch, deviceIP, source)

	if err != nil {
		return false, err
	}

	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

// SaveUsersBatch saves user profiles into the SQLite database.
func (d *DB) SaveUsersBatch(users []zk.User) error {
	if len(users) == 0 {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO users (uid, user_id, name, privilege, password, group_id, card, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id) DO UPDATE SET
			uid = excluded.uid,
			name = excluded.name,
			privilege = excluded.privilege,
			password = excluded.password,
			group_id = excluded.group_id,
			card = excluded.card,
			updated_at = CURRENT_TIMESTAMP
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, u := range users {
		if _, err := stmt.Exec(u.UID, u.UserID, u.Name, u.Privilege, u.Password, u.GroupID, u.Card); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetUsers retrieves all cached user profiles stored in the SQLite database.
func (d *DB) GetUsers() ([]zk.User, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT uid, user_id, name, privilege, password, group_id, card
		FROM users
		ORDER BY uid ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []zk.User
	for rows.Next() {
		var u zk.User
		var pwd, gid sql.NullString
		if err := rows.Scan(&u.UID, &u.UserID, &u.Name, &u.Privilege, &pwd, &gid, &u.Card); err != nil {
			return nil, err
		}
		if pwd.Valid {
			u.Password = pwd.String
		}
		if gid.Valid {
			u.GroupID = gid.String
		}
		users = append(users, u)
	}

	return users, rows.Err()
}

// SaveTemplatesBatch saves fingerprint templates into the SQLite database.
func (d *DB) SaveTemplatesBatch(templates []zk.Finger, uidToUserID map[int]string) (int, error) {
	if len(templates) == 0 {
		return 0, nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO finger_templates (uid, user_id, fid, valid, size, template, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(uid, fid) DO UPDATE SET
			user_id = excluded.user_id,
			valid = excluded.valid,
			size = excluded.size,
			template = excluded.template,
			updated_at = CURRENT_TIMESTAMP
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	savedCount := 0
	for _, t := range templates {
		userID := ""
		if uidToUserID != nil {
			userID = uidToUserID[t.UID]
		}
		if _, err := stmt.Exec(t.UID, userID, t.FID, t.Valid, t.Size, t.Template); err != nil {
			return savedCount, err
		}
		savedCount++
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return savedCount, nil
}

// GetTemplates retrieves all stored fingerprint biometric templates from SQLite database.
func (d *DB) GetTemplates() ([]zk.Finger, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT uid, fid, valid, size, template
		FROM finger_templates
		ORDER BY uid ASC, fid ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []zk.Finger
	for rows.Next() {
		var f zk.Finger
		if err := rows.Scan(&f.UID, &f.FID, &f.Valid, &f.Size, &f.Template); err != nil {
			return nil, err
		}
		list = append(list, f)
	}

	return list, rows.Err()
}

// GetUserTemplates retrieves templates registered to a specific user by UID.
func (d *DB) GetUserTemplates(uid int) ([]zk.Finger, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT uid, fid, valid, size, template
		FROM finger_templates
		WHERE uid = ?
		ORDER BY fid ASC
	`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []zk.Finger
	for rows.Next() {
		var f zk.Finger
		if err := rows.Scan(&f.UID, &f.FID, &f.Valid, &f.Size, &f.Template); err != nil {
			return nil, err
		}
		list = append(list, f)
	}

	return list, rows.Err()
}

// GetUserFingerCountMap returns a map of UID -> enrolled fingers count from SQLite.
func (d *DB) GetUserFingerCountMap() (map[int]int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT uid, COUNT(*) as count
		FROM finger_templates
		GROUP BY uid
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[int]int)
	for rows.Next() {
		var uid, count int
		if err := rows.Scan(&uid, &count); err != nil {
			return nil, err
		}
		counts[uid] = count
	}

	return counts, rows.Err()
}

// GetAttendance retrieves stored attendance logs based on filter criteria with pagination.
func (d *DB) GetAttendance(filter AttendanceFilter) ([]AttendanceRecord, int64, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var conditions []string
	var args []interface{}

	if filter.Search != "" {
		q := "%" + strings.TrimSpace(filter.Search) + "%"
		conditions = append(conditions, "(user_id LIKE ? OR user_name LIKE ? OR CAST(uid AS TEXT) LIKE ?)")
		args = append(args, q, q, q)
	}

	if filter.UserID != "" {
		conditions = append(conditions, "user_id = ?")
		args = append(args, filter.UserID)
	}

	if filter.Status != nil {
		conditions = append(conditions, "status = ?")
		args = append(args, *filter.Status)
	}

	if filter.StartDate != "" {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, filter.StartDate+" 00:00:00")
	}

	if filter.EndDate != "" {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, filter.EndDate+" 23:59:59")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Count total matching
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM attendance_logs %s", whereClause)
	var total int64
	if err := d.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Limit & Offset
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	query := fmt.Sprintf(`
		SELECT id, uid, user_id, user_name, timestamp, status, status_name, punch, device_ip, source, created_at
		FROM attendance_logs
		%s
		ORDER BY timestamp DESC, id DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	queryArgs := append(args, limit, offset)
	rows, err := d.db.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var records []AttendanceRecord
	for rows.Next() {
		var r AttendanceRecord
		var timeStr, createdStr string
		var userName, deviceIP, source sql.NullString

		if err := rows.Scan(&r.ID, &r.UID, &r.UserID, &userName, &timeStr, &r.Status, &r.StatusName, &r.Punch, &deviceIP, &source, &createdStr); err != nil {
			return nil, 0, err
		}

		if userName.Valid {
			r.UserName = userName.String
		}
		if deviceIP.Valid {
			r.DeviceIP = deviceIP.String
		}
		if source.Valid {
			r.Source = source.String
		}

		if t, err := time.Parse("2006-01-02 15:04:05", timeStr); err == nil {
			r.Timestamp = t
		} else if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
			r.Timestamp = t
		}

		if t, err := time.Parse("2006-01-02 15:04:05", createdStr); err == nil {
			r.CreatedAt = t
		} else if t, err := time.Parse(time.RFC3339, createdStr); err == nil {
			r.CreatedAt = t
		}

		records = append(records, r)
	}

	return records, total, nil
}

// GetAttendanceStats returns overall and today statistics from SQLite.
func (d *DB) GetAttendanceStats() (AttendanceStats, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var stats AttendanceStats
	stats.StatusCounts = make(map[string]int64)

	// Total records
	_ = d.db.QueryRow("SELECT COUNT(*) FROM attendance_logs").Scan(&stats.TotalRecords)

	// Total unique users in logs
	_ = d.db.QueryRow("SELECT COUNT(DISTINCT user_id) FROM attendance_logs").Scan(&stats.TotalUsers)

	// Total enrolled users in users table
	_ = d.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&stats.TotalEnrolledUsers)

	// Today's records & unique users
	todayPrefix := time.Now().Format("2006-01-02")
	_ = d.db.QueryRow("SELECT COUNT(*) FROM attendance_logs WHERE timestamp >= ?", todayPrefix+" 00:00:00").Scan(&stats.TodayRecords)
	_ = d.db.QueryRow("SELECT COUNT(DISTINCT user_id) FROM attendance_logs WHERE timestamp >= ?", todayPrefix+" 00:00:00").Scan(&stats.TodayUniqueUsers)

	// Status breakdown
	rows, err := d.db.Query("SELECT status_name, COUNT(*) FROM attendance_logs GROUP BY status_name")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			var count int64
			if err := rows.Scan(&name, &count); err == nil {
				stats.StatusCounts[name] = count
			}
		}
	}

	return stats, nil
}

// LogSync records an entry in the sync_history audit table.
func (d *DB) LogSync(rec SyncRecord) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO sync_history (device_ip, total_records, new_records, status, error_message)
		VALUES (?, ?, ?, ?, ?)
	`, rec.DeviceIP, rec.TotalRecords, rec.NewRecords, rec.Status, rec.ErrorMessage)
	return err
}

// GetUser fetches a single user by UserID or numeric UID string from SQLite.
func (d *DB) GetUser(id string) (*zk.User, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var u zk.User
	var pwd, gid sql.NullString
	err := d.db.QueryRow(`
		SELECT uid, user_id, name, privilege, password, group_id, card
		FROM users
		WHERE user_id = ? OR CAST(uid AS TEXT) = ?
		LIMIT 1
	`, id, id).Scan(&u.UID, &u.UserID, &u.Name, &u.Privilege, &pwd, &gid, &u.Card)

	if err != nil {
		return nil, err
	}
	if pwd.Valid {
		u.Password = pwd.String
	}
	if gid.Valid {
		u.GroupID = gid.String
	}
	return &u, nil
}

// DeleteUser removes a user and their biometric templates from SQLite.
func (d *DB) DeleteUser(userID string, uid uint16) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if userID != "" {
		if _, err := tx.Exec("DELETE FROM users WHERE user_id = ?", userID); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM finger_templates WHERE user_id = ?", userID); err != nil {
			return err
		}
	}
	if uid > 0 {
		if _, err := tx.Exec("DELETE FROM users WHERE uid = ?", uid); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM finger_templates WHERE uid = ?", uid); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetSyncHistory retrieves recent sync history audit records.
func (d *DB) GetSyncHistory(limit int) ([]SyncRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	rows, err := d.db.Query(`
		SELECT id, device_ip, synced_at, total_records, new_records, status, error_message
		FROM sync_history
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []SyncRecord
	for rows.Next() {
		var r SyncRecord
		var timeStr string
		var errMsg sql.NullString
		if err := rows.Scan(&r.ID, &r.DeviceIP, &timeStr, &r.TotalRecords, &r.NewRecords, &r.Status, &errMsg); err != nil {
			return nil, err
		}
		if errMsg.Valid {
			r.ErrorMessage = errMsg.String
		}
		if t, err := time.Parse("2006-01-02 15:04:05", timeStr); err == nil {
			r.SyncedAt = t
		} else if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
			r.SyncedAt = t
		}
		list = append(list, r)
	}

	return list, rows.Err()
}

// GetDeviceConfig retrieves the saved device configuration from the SQLite database.
// If no configuration is stored, it initializes and returns the default configuration.
func (d *DB) GetDeviceConfig() (DeviceConfig, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var cfg DeviceConfig
	var udp, omitPing, autoConnect int
	var updatedStr string

	err := d.db.QueryRow(`
		SELECT ip, port, password, udp, omit_ping, auto_connect, auto_sync_interval_sec, updated_at
		FROM device_config
		WHERE id = 1
	`).Scan(&cfg.IP, &cfg.Port, &cfg.Password, &udp, &omitPing, &autoConnect, &cfg.AutoSyncIntervalSec, &updatedStr)

	if err == sql.ErrNoRows {
		return DefaultDeviceConfig(), nil
	} else if err != nil {
		return DefaultDeviceConfig(), err
	}

	cfg.UDP = udp != 0
	cfg.OmitPing = omitPing != 0
	cfg.AutoConnect = autoConnect != 0

	if t, err := time.Parse("2006-01-02 15:04:05", updatedStr); err == nil {
		cfg.UpdatedAt = t
	} else if t, err := time.Parse(time.RFC3339, updatedStr); err == nil {
		cfg.UpdatedAt = t
	}

	return cfg, nil
}

// SaveDeviceConfig updates or inserts the device configuration into the database.
func (d *DB) SaveDeviceConfig(cfg DeviceConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if cfg.IP == "" {
		cfg.IP = "192.168.1.201"
	}
	if cfg.Port <= 0 {
		cfg.Port = 4370
	}

	udpInt := 0
	if cfg.UDP {
		udpInt = 1
	}
	omitPingInt := 0
	if cfg.OmitPing {
		omitPingInt = 1
	}
	autoConnectInt := 0
	if cfg.AutoConnect {
		autoConnectInt = 1
	}

	_, err := d.db.Exec(`
		INSERT INTO device_config (id, ip, port, password, udp, omit_ping, auto_connect, auto_sync_interval_sec, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			ip = excluded.ip,
			port = excluded.port,
			password = excluded.password,
			udp = excluded.udp,
			omit_ping = excluded.omit_ping,
			auto_connect = excluded.auto_connect,
			auto_sync_interval_sec = excluded.auto_sync_interval_sec,
			updated_at = CURRENT_TIMESTAMP
	`, cfg.IP, cfg.Port, cfg.Password, udpInt, omitPingInt, autoConnectInt, cfg.AutoSyncIntervalSec)

	return err
}

// GetSetting retrieves a setting value by key from the database.
func (d *DB) GetSetting(key string) (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var val string
	err := d.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	return val, err
}

// SetSetting stores or updates a key-value setting in the database.
func (d *DB) SetSetting(key, value string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = CURRENT_TIMESTAMP
	`, key, value)
	return err
}
