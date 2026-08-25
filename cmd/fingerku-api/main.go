package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fingerku/api"
	"fingerku/storage"
)

func main() {
	port := flag.Int("port", 8080, "HTTP server port")
	host := flag.String("host", "0.0.0.0", "HTTP server bind address")
	dbPath := flag.String("db", "fingerku.db", "Path to SQLite database")
	verbose := flag.Bool("verbose", false, "Enable verbose debug packet logs")
	flag.Parse()

	fmt.Println("================================================================")
	fmt.Println("       🚀 Fingerku REST API Server (Powered by Chi)             ")
	fmt.Println("================================================================")
	fmt.Printf(" • SQLite Database : %s\n", *dbPath)
	fmt.Printf(" • API Listening   : http://%s:%d\n", *host, *port)
	fmt.Println(" • Endpoints Prefix: /api/v1")
	fmt.Println(" • Health Check    : /health")
	fmt.Println("================================================================")

	db, err := storage.Open(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open SQLite database (%s): %v", *dbPath, err)
	}
	defer db.Close()

	srv, err := api.NewServer(db, *verbose)
	if err != nil {
		log.Fatalf("Failed to initialize API server: %v", err)
	}

	addr := fmt.Sprintf("%s:%d", *host, *port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      srv.Routes(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown listener
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-stopChan
	fmt.Println("\nShutting down API server gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = srv.Disconnect()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced shutdown: %v", err)
	}
	fmt.Println("Server stopped.")
}
