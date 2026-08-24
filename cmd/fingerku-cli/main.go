package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fingerku/storage"
	"fingerku/zk"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	flagSet := flag.NewFlagSet(command, flag.ExitOnError)
	ip := flagSet.String("ip", "192.168.1.201", "IP address of the ZKTeco device")
	port := flagSet.Int("port", 4370, "Port number")
	password := flagSet.Int("password", 0, "Communication password (commkey)")
	udp := flagSet.Bool("udp", false, "Force UDP protocol")
	omitPing := flagSet.Bool("omit-ping", false, "Omit ICMP ping check")
	verbose := flagSet.Bool("verbose", false, "Enable verbose logging")

	// Subcommand specific flags
	unlockSec := flagSet.Int("seconds", 3, "Unlock duration in seconds (unlock command)")
	voiceIdx := flagSet.Int("index", 0, "Voice index to test (voice command)")
	dbPath := flagSet.String("db", "fingerku.db", "SQLite database file path")
	userFilter := flagSet.String("user", "", "Filter by User ID (db-logs command)")
	fromFilter := flagSet.String("from", "", "Start date filter YYYY-MM-DD (db-logs command)")
	toFilter := flagSet.String("to", "", "End date filter YYYY-MM-DD (db-logs command)")
	limitFilter := flagSet.Int("limit", 50, "Limit number of records (db-logs command)")
	statusFilter := flagSet.Int("status", -1, "Filter by status code 0-5 (db-logs command)")

	_ = flagSet.Parse(os.Args[2:])

	client := zk.New(*ip,
		zk.WithPort(*port),
		zk.WithPassword(*password),
		zk.WithForceUDP(*udp),
		zk.WithOmitPing(*omitPing),
		zk.WithVerbose(*verbose),
		zk.WithTimeout(10*time.Second),
	)

	switch command {
	case "info":
		runInfo(client)
	case "users":
		runUsers(client)
	case "attendance":
		runAttendance(client)
	case "templates":
		runTemplates(client)
	case "live":
		runLive(client, *dbPath, *ip)
	case "sync-logs", "pull-logs":
		runSyncLogs(client, *dbPath, *ip)
	case "db-logs":
		runDBLogs(*dbPath, *userFilter, *fromFilter, *toFilter, *statusFilter, *limitFilter)
	case "db-stats":
		runDBStats(*dbPath)
	case "unlock":
		runUnlock(client, *unlockSec)
	case "synctime":
		runSyncTime(client)
	case "voice":
		runVoice(client, *voiceIdx)
	case "restart":
		runRestart(client)
	case "poweroff":
		runPoweroff(client)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Fingerku CLI - ZKTeco Device Management Tool")
	fmt.Println("\nUsage:")
	fmt.Println("  fingerku-cli <command> [flags]")
	fmt.Println("\nAvailable Commands:")
	fmt.Println("  sync-logs   Fetch attendance logs from machine & save to SQLite database (--db fingerku.db)")
	fmt.Println("  db-logs     Query and display attendance logs stored in SQLite database")
	fmt.Println("  db-stats    Display statistical summary from SQLite database")
	fmt.Println("  info        Show device hardware, firmware, network and memory capacity")
	fmt.Println("  users       List all enrolled users")
	fmt.Println("  attendance  Fetch attendance records directly from machine RAM (Read-Only)")
	fmt.Println("  templates   List fingerprint biometric templates")
	fmt.Println("  live        Monitor punch events in real-time (and optionally save to SQLite)")
	fmt.Println("  unlock      Trigger door access relay (--seconds 3)")
	fmt.Println("  synctime    Synchronize machine time with server RTC")
	fmt.Println("  voice       Play speaker voice prompt (--index 0)")
	fmt.Println("  restart     Reboot device")
	fmt.Println("  poweroff    Shutdown device")
	fmt.Println("\nGlobal Flags:")
	fmt.Println("  --ip        Device IP address (default: 192.168.1.201)")
	fmt.Println("  --port      Device port (default: 4370)")
	fmt.Println("  --password  Commkey password (default: 0)")
	fmt.Println("  --udp       Force UDP transport")
	fmt.Println("  --omit-ping Skip ICMP ping before connecting")
	fmt.Println("  --verbose   Show packet debugging information")
	fmt.Println("  --db        SQLite database file path (default: fingerku.db)")
}

func connect(client *zk.Client) {
	fmt.Print("Connecting to machine... ")
	if err := client.Connect(); err != nil {
		fmt.Printf("FAILED: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("OK")
}

func runSyncLogs(client *zk.Client, dbPath string, deviceIP string) {
	db, err := storage.Open(dbPath)
	if err != nil {
		fmt.Printf("Error opening SQLite database '%s': %v\n", dbPath, err)
		return
	}
	defer db.Close()

	connect(client)
	defer client.Disconnect()

	_ = client.DisableDevice()
	defer client.EnableDevice()

	fmt.Print("Reading users for name mapping... ")
	users, err := client.GetUsers()
	userMap := make(map[string]string)
	if err == nil {
		_ = db.SaveUsersBatch(users)
		for _, u := range users {
			userMap[u.UserID] = u.Name
		}
		fmt.Printf("OK (%d users enrolled)\n", len(users))
	} else {
		fmt.Printf("Warning: %v\n", err)
	}

	fmt.Print("Fetching attendance logs from device memory... ")
	records, err := client.GetAttendance()
	if err != nil {
		fmt.Printf("FAILED: %v\n", err)
		_ = db.LogSync(storage.SyncRecord{
			DeviceIP:     deviceIP,
			TotalRecords: 0,
			NewRecords:   0,
			Status:       "failed",
			ErrorMessage: err.Error(),
		})
		return
	}
	fmt.Printf("OK (%d records found)\n", len(records))

	fmt.Printf("Saving to SQLite database '%s'... ", dbPath)
	inserted, err := db.SaveAttendanceBatch(records, userMap, deviceIP, "device_sync")
	if err != nil {
		fmt.Printf("FAILED: %v\n", err)
		_ = db.LogSync(storage.SyncRecord{
			DeviceIP:     deviceIP,
			TotalRecords: len(records),
			NewRecords:   inserted,
			Status:       "failed",
			ErrorMessage: err.Error(),
		})
		return
	}

	skipped := len(records) - inserted
	fmt.Printf("SUCCESS!\n\n")
	fmt.Println("=== Synchronization Summary ===")
	fmt.Printf("Total records on device : %d\n", len(records))
	fmt.Printf("New records inserted    : %d\n", inserted)
	fmt.Printf("Duplicates skipped      : %d\n", skipped)
	fmt.Printf("Database file           : %s\n", dbPath)

	_ = db.LogSync(storage.SyncRecord{
		DeviceIP:     deviceIP,
		TotalRecords: len(records),
		NewRecords:   inserted,
		Status:       "success",
	})
}

func runDBLogs(dbPath string, userFilter, fromFilter, toFilter string, statusFilter, limit int) {
	db, err := storage.Open(dbPath)
	if err != nil {
		fmt.Printf("Error opening SQLite database '%s': %v\n", dbPath, err)
		return
	}
	defer db.Close()

	var statusPtr *int
	if statusFilter >= 0 {
		statusPtr = &statusFilter
	}

	filter := storage.AttendanceFilter{
		UserID:    userFilter,
		StartDate: fromFilter,
		EndDate:   toFilter,
		Status:    statusPtr,
		Limit:     limit,
	}

	records, total, err := db.GetAttendance(filter)
	if err != nil {
		fmt.Printf("Error querying SQLite: %v\n", err)
		return
	}

	fmt.Printf("\nSQLite Database: %s (Showing %d of %d total matching records)\n", dbPath, len(records), total)
	fmt.Printf("%-6s | %-10s | %-20s | %-20s | %-12s | %-6s | %-12s\n", "ID", "User ID", "Name", "Timestamp", "Status", "Punch", "Source")
	fmt.Println("------------------------------------------------------------------------------------------------------")

	for _, r := range records {
		fmt.Printf("%-6d | %-10s | %-20s | %-20s | %-12s | %-6d | %-12s\n",
			r.ID, r.UserID, r.UserName, r.Timestamp.Format("2006-01-02 15:04:05"), r.StatusName, r.Punch, r.Source)
	}
}

func runDBStats(dbPath string) {
	db, err := storage.Open(dbPath)
	if err != nil {
		fmt.Printf("Error opening SQLite database '%s': %v\n", dbPath, err)
		return
	}
	defer db.Close()

	stats, err := db.GetAttendanceStats()
	if err != nil {
		fmt.Printf("Error querying SQLite stats: %v\n", err)
		return
	}

	fmt.Printf("\n=== SQLite Attendance Statistics (%s) ===\n", dbPath)
	fmt.Printf("Total Attendance Logs    : %d\n", stats.TotalRecords)
	fmt.Printf("Total Unique Enrolled    : %d\n", stats.TotalUsers)
	fmt.Printf("Punches Recorded Today   : %d\n", stats.TodayRecords)
	fmt.Printf("Unique Users Present Today: %d\n", stats.TodayUniqueUsers)

	fmt.Println("\n--- Breakdown by Status ---")
	for statusName, count := range stats.StatusCounts {
		fmt.Printf("%-16s : %d\n", statusName, count)
	}
}

func runInfo(client *zk.Client) {
	connect(client)
	defer client.Disconnect()

	info, err := client.GetDeviceInfo()
	if err != nil {
		fmt.Printf("Error getting device info: %v\n", err)
		return
	}

	machTime, _ := client.GetTime()

	fmt.Println("\n=== Device Information ===")
	fmt.Printf("Device Name      : %s\n", info.DeviceName)
	fmt.Printf("Firmware Version : %s\n", info.FirmwareVersion)
	fmt.Printf("Platform         : %s\n", info.Platform)
	fmt.Printf("Serial Number    : %s\n", info.SerialNumber)
	fmt.Printf("MAC Address      : %s\n", info.MAC)
	fmt.Printf("FP Version       : %d\n", info.FPVersion)
	fmt.Printf("Face Version     : %d\n", info.FaceVersion)
	fmt.Printf("Device Time      : %s\n", machTime.Format("2006-01-02 15:04:05"))
	fmt.Println("\n=== Network Settings ===")
	fmt.Printf("IP Address       : %s\n", info.Network.IP)
	fmt.Printf("Subnet Mask      : %s\n", info.Network.Mask)
	fmt.Printf("Gateway          : %s\n", info.Network.Gateway)
	fmt.Println("\n=== Capacity & Usage ===")
	fmt.Printf("Users            : %d / %d\n", info.Sizes.Users, info.Sizes.UsersCap)
	fmt.Printf("Fingers          : %d / %d\n", info.Sizes.Fingers, info.Sizes.FingersCap)
	fmt.Printf("Records (Logs)   : %d / %d\n", info.Sizes.Records, info.Sizes.RecordsCap)
	fmt.Printf("Faces            : %d / %d\n", info.Sizes.Faces, info.Sizes.FacesCap)
	fmt.Printf("Cards            : %d\n", info.Sizes.Cards)
}

func runUsers(client *zk.Client) {
	connect(client)
	defer client.Disconnect()

	_ = client.DisableDevice()
	defer client.EnableDevice()

	users, err := client.GetUsers()
	if err != nil {
		fmt.Printf("Error getting users: %v\n", err)
		return
	}

	fmt.Printf("\nTotal Enrolled Users: %d\n", len(users))
	fmt.Printf("%-6s | %-12s | %-20s | %-14s | %-10s | %-10s\n", "UID", "User ID", "Name", "Privilege", "Password", "Card")
	fmt.Println("-----------------------------------------------------------------------------------------")
	for _, u := range users {
		pwd := u.Password
		if pwd == "" {
			pwd = "-"
		}
		fmt.Printf("%-6d | %-12s | %-20s | %-14s | %-10s | %-10d\n",
			u.UID, u.UserID, u.Name, u.PrivilegeName(), pwd, u.Card)
	}
}

func runAttendance(client *zk.Client) {
	connect(client)
	defer client.Disconnect()

	_ = client.DisableDevice()
	defer client.EnableDevice()

	records, err := client.GetAttendance()
	if err != nil {
		fmt.Printf("Error getting attendance: %v\n", err)
		return
	}

	fmt.Printf("\nTotal Attendance Records (Device RAM): %d\n", len(records))
	fmt.Printf("%-6s | %-12s | %-20s | %-12s | %-6s\n", "UID", "User ID", "Timestamp", "Status", "Punch")
	fmt.Println("------------------------------------------------------------------")
	for _, r := range records {
		fmt.Printf("%-6d | %-12s | %-20s | %-12s | %-6d\n",
			r.UID, r.UserID, r.Timestamp.Format("2006-01-02 15:04:05"), r.StatusName(), r.Punch)
	}
}

func runTemplates(client *zk.Client) {
	connect(client)
	defer client.Disconnect()

	_ = client.DisableDevice()
	defer client.EnableDevice()

	templates, err := client.GetTemplates()
	if err != nil {
		fmt.Printf("Error getting templates: %v\n", err)
		return
	}

	fmt.Printf("\nTotal Biometric Templates: %d\n", len(templates))
	fmt.Printf("%-6s | %-4s | %-6s | %-6s | %-24s\n", "UID", "FID", "Valid", "Size", "Template Hex Sample")
	fmt.Println("------------------------------------------------------------------")
	for _, t := range templates {
		fmt.Printf("%-6d | %-4d | %-6d | %-6d | %-24s\n",
			t.UID, t.FID, t.Valid, t.Size, t.Mark())
	}
}

func runLive(client *zk.Client, dbPath string, deviceIP string) {
	var db *storage.DB
	if dbPath != "" {
		if d, err := storage.Open(dbPath); err == nil {
			db = d
			defer db.Close()
			fmt.Printf("[SQLite Storage Enabled] Logging incoming punches to '%s'\n", dbPath)
		}
	}

	connect(client)
	defer client.Disconnect()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nStopping live capture...")
		cancel()
	}()

	fmt.Println("\n[LIVE CAPTURE ACTIVE] Tap fingerprint, badge, or face on machine... (Ctrl+C to stop)")
	events, errs := client.LiveCapture(ctx)

	for {
		select {
		case err, ok := <-errs:
			if ok && err != nil {
				fmt.Printf("Error during live capture: %v\n", err)
			}
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			fmt.Printf("👉 [%s] Punch Event! UserID: %s (UID: %d) -> %s (Punch: %d)\n",
				ev.Timestamp.Format("15:04:05"), ev.UserID, ev.UID, ev.StatusName(), ev.Punch)

			if db != nil {
				_, _ = db.SaveSinglePunch(ev, "", deviceIP, "live_stream")
			}
		}
	}
}

func runUnlock(client *zk.Client, seconds int) {
	connect(client)
	defer client.Disconnect()

	fmt.Printf("Unlocking door relay for %d seconds... ", seconds)
	if err := client.Unlock(seconds); err != nil {
		fmt.Printf("FAILED: %v\n", err)
		return
	}
	fmt.Println("OK (Door Opened)")
}

func runSyncTime(client *zk.Client) {
	connect(client)
	defer client.Disconnect()

	now := time.Now()
	fmt.Printf("Setting machine time to %s... ", now.Format("2006-01-02 15:04:05"))
	if err := client.SetTime(now); err != nil {
		fmt.Printf("FAILED: %v\n", err)
		return
	}
	fmt.Println("OK (Clock Synchronized)")
}

func runVoice(client *zk.Client, index int) {
	connect(client)
	defer client.Disconnect()

	fmt.Printf("Playing voice prompt index %d... ", index)
	if err := client.TestVoice(index); err != nil {
		fmt.Printf("FAILED: %v\n", err)
		return
	}
	fmt.Println("OK")
}

func runRestart(client *zk.Client) {
	connect(client)
	defer client.Disconnect()

	fmt.Print("Restarting device... ")
	if err := client.Restart(); err != nil {
		fmt.Printf("FAILED: %v\n", err)
		return
	}
	fmt.Println("OK (Device is rebooting)")
}

func runPoweroff(client *zk.Client) {
	connect(client)
	defer client.Disconnect()

	fmt.Print("Shutting down device... ")
	if err := client.PowerOff(); err != nil {
		fmt.Printf("FAILED: %v\n", err)
		return
	}
	fmt.Println("OK (Device powered off)")
}
