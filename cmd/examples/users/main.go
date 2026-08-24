package main

import (
	"fmt"
	"log"

	"fingerku/zk"
)

func main() {
	client := zk.New("192.168.1.201", zk.WithPort(4370))

	if err := client.Connect(); err != nil {
		log.Fatalf("Cannot connect: %v", err)
	}
	defer client.Disconnect()

	_ = client.DisableDevice()
	defer client.EnableDevice()

	// 1. Create/Update a new user
	newUser := zk.User{
		UID:       100,
		UserID:    "100",
		Name:      "Andi Prasetyo",
		Privilege: zk.UserDefault,
		Password:  "123456",
		GroupID:   "1",
		Card:      0,
	}

	fmt.Printf("Setting user: %s (ID: %s)...\n", newUser.Name, newUser.UserID)
	if err := client.SetUser(newUser); err != nil {
		log.Fatalf("SetUser error: %v", err)
	}
	fmt.Println("User saved successfully!")

	// 2. Fetch and verify
	users, _ := client.GetUsers()
	fmt.Printf("Total users in machine: %d\n", len(users))

	// 3. Delete user (optional)
	// _ = client.DeleteUser(100, "100")
}
