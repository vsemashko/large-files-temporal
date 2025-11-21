package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/vsemashko/large-files-temporal/codec-server/internal/codec"
	"github.com/vsemashko/large-files-temporal/codec-server/internal/config"
	grpcserver "github.com/vsemashko/large-files-temporal/codec-server/internal/grpc"
	"github.com/vsemashko/large-files-temporal/codec-server/internal/storage"
	pb "github.com/vsemashko/large-files-temporal/codec-server/pkg/proto"
	"google.golang.org/grpc"
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

	// Create gRPC server
	grpcServer := grpc.NewServer()
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

	// Start server in a goroutine
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			log.Fatalf("Failed to serve gRPC: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down gracefully...")
	grpcServer.GracefulStop()
	log.Println("Server stopped")
}
