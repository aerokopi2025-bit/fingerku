package main

import (
	"fmt"
	"log"

	"fingerku/zk"
)

func main() {
	deviceIP := "192.168.1.201"

	// Create a new ZKTeco client
	client := zk.New(deviceIP, zk.WithPort(4370))

	fmt.Println("Connecting to device...")
	if err := client.Connect(); err != nil {
		log.Fatalf("Connection failed: %v", err)
	}
	defer client.Disconnect()

	fmt.Println("Successfully connected!")

	// Disable device during operations
	_ = client.DisableDevice()
	defer client.EnableDevice()

	// Get Firmware & Serial Number
	fw, _ := client.GetFirmwareVersion()
	sn, _ := client.GetSerialNumber()
	machTime, _ := client.GetTime()

	fmt.Printf("Firmware : %s\n", fw)
	fmt.Printf("Serial   : %s\n", sn)
	fmt.Printf("Time     : %s\n", machTime.Format("2006-01-02 15:04:05"))

	// List enrolled users
	users, err := client.GetUsers()
	if err != nil {
		log.Fatalf("Failed to fetch users: %v", err)
	}

	fmt.Printf("\nFound %d users:\n", len(users))
	for _, u := range users {
		fmt.Printf("- UID: %d | ID: %s | Name: %s | Role: %s\n", u.UID, u.UserID, u.Name, u.PrivilegeName())
	}

	// Play voice "Thank you"
	_ = client.TestVoice(0)
}
