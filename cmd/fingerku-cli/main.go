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
	dbPath := flagSet.String("db", "fingerku.db", "SQLite database file path")
	ip := flagSet.String("ip", "", "IP address of the ZKTeco device (default: from DB config)")
	port := flagSet.Int("port", 0, "Port number (default: from DB config)")
	password := flagSet.Int("password", -1, "Communication password / commkey (default: from DB config)")
	udp := flagSet.Bool("udp", false, "Force UDP protocol")
	omitPing := flagSet.Bool("omit-ping", false, "Omit ICMP ping check")
	verbose := flagSet.Bool("verbose", false, "Enable verbose logging")
	autoSyncInterval := flagSet.Int("auto-sync-interval", 0, "Auto-sync interval in seconds for run command")

	// Subcommand specific flags
	unlockSec := flagSet.Int("seconds", 3, "Unlock duration in seconds (unlock command)")
	voiceIdx := flagSet.Int("index", 0, "Voice index to test (voice command)")
	userFilter := flagSet.String("user", "", "Filter by User ID (db-logs command)")
	fromFilter := flagSet.String("from", "", "Start date filter YYYY-MM-DD (db-logs command)")
	toFilter := flagSet.String("to", "", "End date filter YYYY-MM-DD (db-logs command)")
	limitFilter := flagSet.Int("limit", 50, "Limit number of records (db-logs command)")
	statusFilter := flagSet.Int("status", -1, "Filter by status code 0-5 (db-logs command)")

	_ = flagSet.Parse(os.Args[2:])

	// Check which flags were explicitly set via CLI
	isFlagSet := make(map[string]bool)
	flagSet.Visit(func(f *flag.Flag) {
		isFlagSet[f.Name] = true
	})

	// 1. Open SQLite Database (defaults to fingerku.db)
	db, err := storage.Open(*dbPath)
	if err != nil {
		fmt.Printf("Error opening SQLite database '%s': %v\n", *dbPath, err)
		os.Exit(1)
	}
	defer db.Close()

	// 2. Load stored device configuration from DB
	dbCfg, err := db.GetDeviceConfig()
	if err != nil {
		dbCfg = storage.DefaultDeviceConfig()
	}

	// 3. Merge: CLI flags explicitly passed override the stored DB configuration
	effectiveCfg := dbCfg
	if isFlagSet["ip"] {
		effectiveCfg.IP = *ip
	}
	if isFlagSet["port"] {
		effectiveCfg.Port = *port
	}
	if isFlagSet["password"] {
		effectiveCfg.Password = *password
	}
	if isFlagSet["udp"] {
		effectiveCfg.UDP = *udp
	}
	if isFlagSet["omit-ping"] {
		effectiveCfg.OmitPing = *omitPing
	}
	if isFlagSet["auto-sync-interval"] {
		effectiveCfg.AutoSyncIntervalSec = *autoSyncInterval
	}

	switch command {
	case "run", "daemon", "service":
		runRunner(db, effectiveCfg, *dbPath, *verbose)
	case "config", "get-config":
		runGetConfig(db, *dbPath)
	case "set-config":
		runSetConfig(db, effectiveCfg, *dbPath)
	case "info":
		client := createClient(effectiveCfg, *verbose)
		runInfo(client, effectiveCfg)
	case "users":
		client := createClient(effectiveCfg, *verbose)
		runUsers(client, db)
	case "attendance":
		client := createClient(effectiveCfg, *verbose)
		runAttendance(client)
	case "templates":
		client := createClient(effectiveCfg, *verbose)
		runTemplates(client)
	case "live":
		client := createClient(effectiveCfg, *verbose)
		runLive(client, db, effectiveCfg)
	case "sync-logs", "pull-logs":
		client := createClient(effectiveCfg, *verbose)
		runSyncLogs(client, db, effectiveCfg, *dbPath)
	case "db-logs":
		runDBLogs(db, *dbPath, *userFilter, *fromFilter, *toFilter, *statusFilter, *limitFilter)
	case "db-stats":
		runDBStats(db, *dbPath)
	case "unlock":
		client := createClient(effectiveCfg, *verbose)
		runUnlock(client, *unlockSec)
	case "synctime":
		client := createClient(effectiveCfg, *verbose)
		runSyncTime(client)
	case "voice":
		client := createClient(effectiveCfg, *verbose)
		runVoice(client, *voiceIdx)
	case "restart":
		client := createClient(effectiveCfg, *verbose)
		runRestart(client)
	case "poweroff":
		client := createClient(effectiveCfg, *verbose)
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
	fmt.Println("Fingerku CLI - ZKTeco Device Management & SQLite Sync Tool")
	fmt.Println("\nUsage:")
	fmt.Println("  fingerku-cli <command> [flags]")
	fmt.Println("\nCore & Database Commands:")
	fmt.Println("  run         Run background listener & auto-sync service using DB configuration")
	fmt.Println("  config      Display device configuration saved in SQLite database")
	fmt.Println("  set-config  Update and save default device configuration in SQLite database")
	fmt.Println("  sync-logs   Fetch attendance logs from machine & save to SQLite database")
	fmt.Println("  db-logs     Query and display attendance logs stored in SQLite database")
	fmt.Println("  db-stats    Display statistical summary from SQLite database")
	fmt.Println("\nDevice & Hardware Commands:")
	fmt.Println("  info        Show device hardware, firmware, network and memory capacity")
	fmt.Println("  users       List all enrolled users (and cache to SQLite)")
	fmt.Println("  attendance  Fetch attendance records directly from machine RAM (Read-Only)")
	fmt.Println("  templates   List fingerprint biometric templates")
	fmt.Println("  live        Monitor punch events in real-time and save to SQLite")
	fmt.Println("  unlock      Trigger door access relay (--seconds 3)")
	fmt.Println("  synctime    Synchronize machine time with server RTC")
	fmt.Println("  voice       Play speaker voice prompt (--index 0)")
	fmt.Println("  restart     Reboot device")
	fmt.Println("  poweroff    Shutdown device")
	fmt.Println("\nGlobal Flags:")
	fmt.Println("  --db                 SQLite database file path (default: fingerku.db)")
	fmt.Println("  --ip                 Device IP address (overrides DB config)")
	fmt.Println("  --port               Device port (overrides DB config, default: 4370)")
	fmt.Println("  --password           Commkey password (overrides DB config, default: 0)")
	fmt.Println("  --udp                Force UDP transport (overrides DB config)")
	fmt.Println("  --omit-ping          Skip ICMP ping before connecting")
	fmt.Println("  --auto-sync-interval Periodic sync interval in seconds for run command (0 = disabled)")
	fmt.Println("  --verbose            Show packet debugging information")
}

func createClient(cfg storage.DeviceConfig, verbose bool) *zk.Client {
	return zk.New(cfg.IP,
		zk.WithPort(cfg.Port),
		zk.WithPassword(cfg.Password),
		zk.WithForceUDP(cfg.UDP),
		zk.WithOmitPing(cfg.OmitPing),
		zk.WithVerbose(verbose),
		zk.WithTimeout(10*time.Second),
	)
}

func connect(client *zk.Client) {
	fmt.Print("Connecting to machine... ")
	if err := client.Connect(); err != nil {
		fmt.Printf("FAILED: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("OK")
}

// runRunner executes the continuous service daemon using configuration saved in SQLite DB.
func runRunner(db *storage.DB, cfg storage.DeviceConfig, dbPath string, verbose bool) {
	fmt.Println("================================================================")
	fmt.Println("       🌟 Fingerku Runner - ZKTeco Background Service 🌟        ")
	fmt.Println("================================================================")
	fmt.Printf(" SQLite Database : %s\n", dbPath)
	fmt.Printf(" Device Target   : %s:%d (CommKey: %d, UDP: %v)\n", cfg.IP, cfg.Port, cfg.Password, cfg.UDP)
	if cfg.AutoSyncIntervalSec > 0 {
		fmt.Printf(" Auto-Sync Period: Every %d seconds\n", cfg.AutoSyncIntervalSec)
	} else {
		fmt.Printf(" Auto-Sync Period: On Start & Live Stream\n")
	}
	fmt.Println("================================================================")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n[Runner] Shutdown signal received. Stopping gracefully...")
		cancel()
	}()

	// 1. Initial Device Connection
	client := createClient(cfg, verbose)
	connect(client)
	defer client.Disconnect()

	// 2. Initial Sync
	fmt.Println("[Runner] Performing initial synchronization...")
	userMap := syncUsersAndLogs(client, db, cfg.IP, dbPath)

	// 3. Optional Periodic Auto-Sync Timer
	if cfg.AutoSyncIntervalSec > 0 {
		go func() {
			ticker := time.NewTicker(time.Duration(cfg.AutoSyncIntervalSec) * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					fmt.Println("\n[Auto-Sync] Running periodic synchronization...")
					_ = syncUsersAndLogs(client, db, cfg.IP, dbPath)
				}
			}
		}()
	}

	// 4. Live Capture Loop
	fmt.Println("\n[Runner] [LIVE CAPTURE ACTIVE] Monitoring punches in real-time... (Press Ctrl+C to stop)")
	events, errs := client.LiveCapture(ctx)

	for {
		select {
		case <-ctx.Done():
			fmt.Println("[Runner] Service stopped.")
			return
		case err, ok := <-errs:
			if ok && err != nil {
				fmt.Printf("[Runner] Live capture error: %v\n", err)
			}
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			userName := userMap[ev.UserID]
			if userName == "" {
				userName = fmt.Sprintf("User %s", ev.UserID)
			}

			_, _ = db.SaveSinglePunch(ev, userName, cfg.IP, "live_stream")
			fmt.Printf("👉 [%s] Punch Event! User: %s (ID: %s, UID: %d) -> %s (Punch: %d) [Saved to SQLite]\n",
				ev.Timestamp.Format("15:04:05"), userName, ev.UserID, ev.UID, ev.StatusName(), ev.Punch)
		}
	}
}

func syncUsersAndLogs(client *zk.Client, db *storage.DB, deviceIP string, dbPath string) map[string]string {
	userMap := make(map[string]string)

	_ = client.DisableDevice()
	defer client.EnableDevice()

	fmt.Print(" -> Fetching users from machine... ")
	users, err := client.GetUsers()
	if err == nil {
		_ = db.SaveUsersBatch(users)
		for _, u := range users {
			userMap[u.UserID] = u.Name
		}
		fmt.Printf("OK (%d users saved to DB)\n", len(users))
	} else {
		fmt.Printf("Warning: %v\n", err)
	}

	fmt.Print(" -> Fetching attendance records from machine RAM... ")
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
		return userMap
	}
	fmt.Printf("OK (%d records found)\n", len(records))

	inserted, err := db.SaveAttendanceBatch(records, userMap, deviceIP, "runner_sync")
	if err != nil {
		fmt.Printf(" -> Database write error: %v\n", err)
		_ = db.LogSync(storage.SyncRecord{
			DeviceIP:     deviceIP,
			TotalRecords: len(records),
			NewRecords:   inserted,
			Status:       "failed",
			ErrorMessage: err.Error(),
		})
		return userMap
	}

	fmt.Printf(" -> Sync finished: %d total on device, %d new inserted, %d skipped duplicates.\n",
		len(records), inserted, len(records)-inserted)

	_ = db.LogSync(storage.SyncRecord{
		DeviceIP:     deviceIP,
		TotalRecords: len(records),
		NewRecords:   inserted,
		Status:       "success",
	})

	return userMap
}

func runGetConfig(db *storage.DB, dbPath string) {
	cfg, err := db.GetDeviceConfig()
	if err != nil {
		fmt.Printf("Error retrieving device config from DB: %v\n", err)
		return
	}

	proto := "TCP"
	if cfg.UDP {
		proto = "UDP"
	}

	fmt.Printf("\n=== Stored Device Configuration (%s) ===\n", dbPath)
	fmt.Printf("IP Address            : %s\n", cfg.IP)
	fmt.Printf("Port                  : %d\n", cfg.Port)
	fmt.Printf("CommKey Password      : %d\n", cfg.Password)
	fmt.Printf("Transport Protocol    : %s\n", proto)
	fmt.Printf("Omit Ping Check       : %v\n", cfg.OmitPing)
	fmt.Printf("Auto-Connect On Start : %v\n", cfg.AutoConnect)
	fmt.Printf("Auto-Sync Interval    : %d seconds\n", cfg.AutoSyncIntervalSec)
	if !cfg.UpdatedAt.IsZero() {
		fmt.Printf("Last Updated In DB    : %s\n", cfg.UpdatedAt.Format("2006-01-02 15:04:05"))
	}
	fmt.Println("----------------------------------------------------------------")
}

func runSetConfig(db *storage.DB, cfg storage.DeviceConfig, dbPath string) {
	if err := db.SaveDeviceConfig(cfg); err != nil {
		fmt.Printf("Failed to save device configuration to SQLite '%s': %v\n", dbPath, err)
		return
	}
	fmt.Printf("Device configuration successfully saved to SQLite database '%s'!\n", dbPath)
	runGetConfig(db, dbPath)
}

func runSyncLogs(client *zk.Client, db *storage.DB, cfg storage.DeviceConfig, dbPath string) {
	connect(client)
	defer client.Disconnect()

	_ = syncUsersAndLogs(client, db, cfg.IP, dbPath)
}

func runDBLogs(db *storage.DB, dbPath string, userFilter, fromFilter, toFilter string, statusFilter, limit int) {
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

func runDBStats(db *storage.DB, dbPath string) {
	stats, err := db.GetAttendanceStats()
	if err != nil {
		fmt.Printf("Error querying SQLite stats: %v\n", err)
		return
	}

	fmt.Printf("\n=== SQLite Attendance Statistics (%s) ===\n", dbPath)
	fmt.Printf("Total Attendance Logs     : %d\n", stats.TotalRecords)
	fmt.Printf("Total Unique Enrolled     : %d\n", stats.TotalUsers)
	fmt.Printf("Punches Recorded Today    : %d\n", stats.TodayRecords)
	fmt.Printf("Unique Users Present Today: %d\n", stats.TodayUniqueUsers)

	fmt.Println("\n--- Breakdown by Status ---")
	for statusName, count := range stats.StatusCounts {
		fmt.Printf("%-16s : %d\n", statusName, count)
	}
}

func runInfo(client *zk.Client, cfg storage.DeviceConfig) {
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

func runUsers(client *zk.Client, db *storage.DB) {
	connect(client)
	defer client.Disconnect()

	_ = client.DisableDevice()
	defer client.EnableDevice()

	users, err := client.GetUsers()
	if err != nil {
		fmt.Printf("Error getting users: %v\n", err)
		return
	}

	_ = db.SaveUsersBatch(users)

	fmt.Printf("\nTotal Enrolled Users: %d (Cached to SQLite)\n", len(users))
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

func runLive(client *zk.Client, db *storage.DB, cfg storage.DeviceConfig) {
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
				_, _ = db.SaveSinglePunch(ev, "", cfg.IP, "live_stream")
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
