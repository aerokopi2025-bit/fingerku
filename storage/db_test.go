package storage

import (
	"os"
	"testing"
	"time"

	"fingerku/zk"
)

func TestSQLiteStorage(t *testing.T) {
	dbFile := "test_fingerku.db"
	defer os.Remove(dbFile)

	db, err := Open(dbFile)
	if err != nil {
		t.Fatalf("Failed to open sqlite db: %v", err)
	}
	defer db.Close()

	users := []zk.User{
		{UID: 1, UserID: "1001", Name: "Budi Santoso", Privilege: zk.UserDefault, GroupID: "1"},
		{UID: 2, UserID: "1002", Name: "Siti Rahma", Privilege: zk.UserAdmin, GroupID: "1"},
	}

	if err := db.SaveUsersBatch(users); err != nil {
		t.Fatalf("Failed to save users batch: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	records := []zk.Attendance{
		{UID: 1, UserID: "1001", Timestamp: now, Status: 0, Punch: 0},
		{UID: 2, UserID: "1002", Timestamp: now, Status: 0, Punch: 0},
	}

	userMap := map[string]string{
		"1001": "Budi Santoso",
		"1002": "Siti Rahma",
	}

	inserted, err := db.SaveAttendanceBatch(records, userMap, "192.168.1.201", "test")
	if err != nil {
		t.Fatalf("Failed to save attendance batch: %v", err)
	}
	if inserted != 2 {
		t.Errorf("Expected 2 inserted, got %d", inserted)
	}

	// Test duplicate insertion (should be ignored)
	inserted2, err := db.SaveAttendanceBatch(records, userMap, "192.168.1.201", "test")
	if err != nil {
		t.Fatalf("Failed duplicate test: %v", err)
	}
	if inserted2 != 0 {
		t.Errorf("Expected 0 inserted for duplicates, got %d", inserted2)
	}

	// Query Attendance
	filter := AttendanceFilter{
		UserID: "1001",
		Limit:  10,
	}
	results, total, err := db.GetAttendance(filter)
	if err != nil {
		t.Fatalf("Failed to get attendance: %v", err)
	}
	if total != 1 || len(results) != 1 {
		t.Errorf("Expected 1 result for user 1001, got %d total, %d returned", total, len(results))
	}
	if results[0].UserName != "Budi Santoso" {
		t.Errorf("Expected user name 'Budi Santoso', got '%s'", results[0].UserName)
	}

	// Test Stats
	stats, err := db.GetAttendanceStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}
	if stats.TotalRecords != 2 {
		t.Errorf("Expected 2 total records in stats, got %d", stats.TotalRecords)
	}
	if stats.TotalUsers != 2 {
		t.Errorf("Expected 2 total users in stats, got %d", stats.TotalUsers)
	}
}
