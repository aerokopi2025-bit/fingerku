package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"fingerku/zk"
)

type BackupPayload struct {
	Users      []zk.User   `json:"users"`
	Templates  []zk.Finger `json:"templates"`
	Attendance []zk.Attendance `json:"attendance"`
	DeviceInfo *zk.DeviceInfo  `json:"device_info"`
}

func main() {
	client := zk.New("192.168.1.201", zk.WithPort(4370))

	if err := client.Connect(); err != nil {
		log.Fatalf("Cannot connect: %v", err)
	}
	defer client.Disconnect()

	_ = client.DisableDevice()
	defer client.EnableDevice()

	fmt.Println("Downloading device info, users, templates, and attendance logs...")

	info, _ := client.GetDeviceInfo()
	users, _ := client.GetUsers()
	templates, _ := client.GetTemplates()
	attendance, _ := client.GetAttendance()

	backup := BackupPayload{
		Users:      users,
		Templates:  templates,
		Attendance: attendance,
		DeviceInfo: info,
	}

	data, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		log.Fatalf("JSON marshal error: %v", err)
	}

	filename := "zk_backup.json"
	if err := os.WriteFile(filename, data, 0644); err != nil {
		log.Fatalf("Failed to save file: %v", err)
	}

	fmt.Printf("Backup saved to %s (Users: %d, Templates: %d, Records: %d)\n",
		filename, len(users), len(templates), len(attendance))
}
