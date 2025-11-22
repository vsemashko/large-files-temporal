package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	nethttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vsemashko/large-files-temporal/codec-server/internal/codec"
	"github.com/vsemashko/large-files-temporal/codec-server/internal/config"
	grpcserver "github.com/vsemashko/large-files-temporal/codec-server/internal/grpc"
	httpserver "github.com/vsemashko/large-files-temporal/codec-server/internal/http"
	"github.com/vsemashko/large-files-temporal/codec-server/internal/ratelimit"
	"github.com/vsemashko/large-files-temporal/codec-server/internal/storage"
	pb "github.com/vsemashko/large-files-temporal/codec-server/pkg/proto"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

func main() {
	log.Println("Starting Temporal Codec Server...")

	// Load configuration
	cfg := config.LoadConfig()
	log.Printf("Configuration loaded: threshold=%d bytes, bucket=%s, region=%s",
		cfg.PayloadSizeThreshold, cfg.S3Bucket, cfg.S3Region)

	ctx := context.Background()

	// Initialize S3 storage
	s3Storage, err := storage.NewS3Storage(
		ctx,
		cfg.S3Bucket,
		cfg.S3Region,
		cfg.S3Endpoint,
		cfg.AWSAccessKeyID,
		cfg.AWSSecretAccessKey,
	)
	if err != nil {
		log.Fatalf("Failed to initialize S3 storage: %v", err)
	}
	log.Println("S3 storage initialized successfully")

	// Create codec
	c := codec.NewCodec(s3Storage, cfg.PayloadSizeThreshold, cfg.S3Bucket)
	log.Println("Codec initialized successfully")

	// Prepare gRPC server options
	grpcOpts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(100 * 1024 * 1024), // 100MB max receive
		grpc.MaxSendMsgSize(100 * 1024 * 1024), // 100MB max send
		grpc.ConnectionTimeout(30 * time.Second),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     15 * time.Minute,
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 5 * time.Minute,
			Time:                  5 * time.Minute,
			Timeout:               1 * time.Minute,
		}),
	}

	// Add authentication interceptor if API key is configured
	if cfg.APIKey != "" {
		grpcOpts = append(grpcOpts, grpc.UnaryInterceptor(grpcserver.AuthInterceptor(cfg.APIKey)))
		log.Println("gRPC API key authentication enabled")
	}

	// Add TLS credentials if enabled
	if cfg.TLSEnabled {
		creds, err := credentials.NewServerTLSFromFile(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			log.Fatalf("Failed to load TLS credentials: %v", err)
		}
		grpcOpts = append(grpcOpts, grpc.Creds(creds))
		log.Printf("gRPC TLS enabled with cert: %s", cfg.TLSCertFile)
	}

	// Create gRPC server with limits and timeouts
	grpcServer := grpc.NewServer(grpcOpts...)
	codecServer := grpcserver.NewServer(c)
	pb.RegisterPayloadCodecServer(grpcServer, codecServer)

	// Enable reflection for debugging with grpcurl
	reflection.Register(grpcServer)

	// Start gRPC server
	grpcAddr := fmt.Sprintf(":%d", cfg.GRPCPort)
	listener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", grpcAddr, err)
	}

	log.Printf("gRPC server listening on %s", grpcAddr)

	// Start gRPC server in a goroutine
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			log.Fatalf("Failed to serve gRPC: %v", err)
		}
	}()

	// Create HTTP server for TypeScript workers
	httpSrv := httpserver.NewServer(c, cfg.APIKey)
	httpMux := nethttp.NewServeMux()

	// Create rate limiter
	rateLimiter := ratelimit.NewIPRateLimiter(rate.Limit(cfg.RateLimitRPS), cfg.RateLimitBurst)
	log.Printf("Rate limiting enabled: %d req/s, burst %d", cfg.RateLimitRPS, cfg.RateLimitBurst)

	// Wrap endpoints with rate limiting and authentication middleware
	// Order: rate limiting -> authentication -> handler
	httpMux.HandleFunc("/encode", rateLimiter.Middleware(httpSrv.AuthMiddleware(httpSrv.HandleEncode)))
	httpMux.HandleFunc("/decode", rateLimiter.Middleware(httpSrv.AuthMiddleware(httpSrv.HandleDecode)))
	httpMux.HandleFunc("/health", httpSrv.HandleHealth) // Health is always accessible
	httpMux.HandleFunc("/metrics", rateLimiter.Middleware(httpSrv.AuthMiddleware(httpSrv.HandleMetrics)))

	if cfg.APIKey != "" {
		log.Println("API key authentication enabled")
	}

	httpAddr := fmt.Sprintf(":%d", cfg.HTTPPort)
	httpServer := &nethttp.Server{
		Addr:              httpAddr,
		Handler:           httpMux,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}

	log.Printf("HTTP server listening on %s", httpAddr)

	// Start HTTP server in a goroutine
	go func() {
		var err error
		if cfg.TLSEnabled {
			// Configure TLS
			tlsConfig := &tls.Config{
				MinVersion: tls.VersionTLS12,
				CipherSuites: []uint16{
					tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
					tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
					tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
					tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				},
			}
			httpServer.TLSConfig = tlsConfig
			log.Printf("HTTP TLS enabled with cert: %s", cfg.TLSCertFile)
			err = httpServer.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			err = httpServer.ListenAndServe()
		}
		if err != nil && err != nethttp.ErrServerClosed {
			log.Fatalf("Failed to serve HTTP: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down gracefully...")

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Shutdown HTTP server
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	// Shutdown gRPC server
	grpcServer.GracefulStop()

	log.Println("Server stopped")
}
