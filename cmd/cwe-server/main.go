// cwe-server is the CWL Workflow Engine server.
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

	"github.com/BV-BRC/cwe-cwl/internal/api"
	"github.com/BV-BRC/cwe-cwl/internal/config"
	"github.com/BV-BRC/cwe-cwl/internal/state"
)

func main() {
	configPath := flag.String("config", "", "Path to configuration file")
	devMode := flag.Bool("dev", false, "Enable development mode (no auth, in-memory store)")
	port := flag.Int("port", 8080, "Server port")
	flag.Parse()

	// Load configuration (uses defaults if no config file found)
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Override with flags
	if *port != 8080 {
		cfg.Server.Port = *port
	}

	// Development mode
	if *devMode {
		log.Println("Running in development mode")
		cfg.Auth.ValidateUserTokens = false
	}

	// Connect to MongoDB
	var store *state.Store
	if *devMode {
		log.Println("Development mode: MongoDB required but using empty URI will fail on first DB operation")
		// For development, we still need MongoDB but can skip auth
	}

	store, err = state.NewStore(cfg.MongoDB.URI, cfg.MongoDB.Database)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		store.Close(ctx)
	}()
	log.Printf("Connected to MongoDB: %s", cfg.MongoDB.Database)

	// Create server
	server := api.NewServer(cfg, store)

	// Configure HTTP server
	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      server,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Starting CWE server on port %d", cfg.Server.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
