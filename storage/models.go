package storage

import (
	"time"
)

// AttendanceRecord represents a stored attendance log entry in the SQLite database.
type AttendanceRecord struct {
	ID         int64     `json:"id"`
	UID        int       `json:"uid"`
	UserID     string    `json:"user_id"`
	UserName   string    `json:"user_name"`
	Timestamp  time.Time `json:"timestamp"`
	Status     int       `json:"status"`
	StatusName string    `json:"status_name"`
	Punch      int       `json:"punch"`
	DeviceIP   string    `json:"device_ip"`
	Source     string    `json:"source"` // "device", "live_stream", "manual", "mock"
	CreatedAt  time.Time `json:"created_at"`
}

// AttendanceFilter provides query parameters for filtering stored attendance logs.
type AttendanceFilter struct {
	Search    string `json:"search"`
	UserID    string `json:"user_id"`
	Status    *int   `json:"status"`
	StartDate string `json:"start_date"` // YYYY-MM-DD
	EndDate   string `json:"end_date"`   // YYYY-MM-DD
	Limit     int    `json:"limit"`
	Offset    int    `json:"offset"`
}

// AttendanceStats aggregates attendance statistics from SQLite.
type AttendanceStats struct {
	TotalRecords     int64            `json:"total_records"`
	TotalUsers       int64            `json:"total_users"`
	TodayRecords     int64            `json:"today_records"`
	TodayUniqueUsers int64            `json:"today_unique_users"`
	StatusCounts     map[string]int64 `json:"status_counts"`
}

// SyncRecord represents an audit log entry for database synchronization from a device.
type SyncRecord struct {
	ID           int64     `json:"id"`
	DeviceIP     string    `json:"device_ip"`
	SyncedAt     time.Time `json:"synced_at"`
	TotalRecords int       `json:"total_records"`
	NewRecords   int       `json:"new_records"`
	Status       string    `json:"status"` // "success", "partial", "failed"
	ErrorMessage string    `json:"error_message,omitempty"`
}
