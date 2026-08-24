package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"fingerku/zk"
)

func main() {
	client := zk.New("192.168.1.201", zk.WithPort(4370))

	if err := client.Connect(); err != nil {
		log.Fatalf("Cannot connect: %v", err)
	}
	defer client.Disconnect()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nReceived stop signal...")
		cancel()
	}()

	fmt.Println("Listening for live punch events... (Press Ctrl+C to stop)")
	events, errs := client.LiveCapture(ctx)

	for {
		select {
		case err, ok := <-errs:
			if ok && err != nil {
				log.Printf("Live capture error: %v", err)
			}
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			fmt.Printf("🔔 [REALTIME] User %s punched at %s (%s)\n",
				ev.UserID, ev.Timestamp.Format("2006-01-02 15:04:05"), ev.StatusName())
		}
	}
}
