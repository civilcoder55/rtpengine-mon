package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"rtpengine-mon/internal/api"
	"rtpengine-mon/internal/config"
	"rtpengine-mon/internal/rtpengine"
	"rtpengine-mon/internal/spy"
	"rtpengine-mon/pkg/telemetry"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("application failure: %v", err)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config load failed: %w", err)
	}

	// 2. Setup Telemetry
	tracerProvider, err := telemetry.InitTracer(ctx, cfg.TelemetryEndpoint)
	if err != nil {
		return fmt.Errorf("telemetry init failed: %w", err)
	}
	if tracerProvider != nil {
		defer func() {
			if err := tracerProvider.Shutdown(context.Background()); err != nil {
				log.Printf("Error shutting down tracer provider: %v", err)
			}
		}()
		log.Printf("Telemetry enabled with endpoint: %s", cfg.TelemetryEndpoint)
	} else {
		log.Println("Telemetry disabled (no endpoint configured)")
	}

	// 3. Connect to RTPEngine
	rtpClient, err := rtpengine.NewClient(cfg.RTPEngineAddr)
	if err != nil {
		return fmt.Errorf("rtpengine client init failed: %w", err)
	}
	defer rtpClient.Close()
	log.Printf("Connected to RTPEngine at %s", cfg.RTPEngineAddr)

	// 4. Start Spy Service (Handles WebRTC)
	tcpListener, err := net.ListenTCP("tcp", &net.TCPAddr{
		IP:   net.ParseIP(cfg.WebRTCICEAddress),
		Port: cfg.WebRTCICEPort,
	})
	if err != nil {
		return fmt.Errorf("failed to listen on TCP %s:%d: %w", cfg.WebRTCICEAddress, cfg.WebRTCICEPort, err)
	}
	log.Printf("WebRTC Listening for ICE TCP at %s", tcpListener.Addr())

	spyService, err := spy.NewService(cfg, rtpClient, tcpListener)
	if err != nil {
		return fmt.Errorf("spy service init failed: %w", err)
	}

	// 5. Setup HTTP Server
	apiHandler := api.NewHandler(rtpClient, spyService)
	mux := http.NewServeMux()
	apiHandler.RegisterRoutes(mux)
	
	// Serve static files
	mux.Handle("/", http.FileServer(http.Dir("./static")))

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: mux,
	}

	// 6. Start Server in goroutine
	srvErr := make(chan error, 1)
	go func() {
		log.Printf("Starting HTTP server on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErr <- fmt.Errorf("http server failed: %w", err)
		}
	}()

	// 7. Wait for signal or error
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-srvErr:
		return err
	case <-stop:
		log.Println("Shutting down...")
	}

	// 8. Graceful Shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	return nil
}
