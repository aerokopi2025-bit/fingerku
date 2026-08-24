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

	records, err := client.GetAttendance()
	if err != nil {
		log.Fatalf("Cannot fetch attendance: %v", err)
	}

	fmt.Printf("Total Records: %d\n", len(records))
	for _, r := range records {
		fmt.Printf("User: %s (UID: %d) | Time: %s | State: %s\n",
			r.UserID, r.UID, r.Timestamp.Format("2006-01-02 15:04:05"), r.StatusName())
	}
}
