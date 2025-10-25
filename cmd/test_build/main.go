package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	appConfig "github.com/rapidbuildapp/rapidbuild/config"
	"github.com/rapidbuildapp/rapidbuild/internal/db"
	"github.com/rapidbuildapp/rapidbuild/internal/services"
	"github.com/rapidbuildapp/rapidbuild/internal/worker"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file found")
	}

	// Load configuration
	cfg := appConfig.Load()

	// Connect to database
	dbClient, err := db.NewPostgresClient(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbClient.Close()

	// Initialize AWS S3 client
	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(cfg.AWSRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AWSAccessKey,
			cfg.AWSSecretKey,
			"",
		)),
	)
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}
	s3Client := s3.NewFromConfig(awsCfg)

	// Create services (minimal for test)
	vercelService := services.NewVercelService(cfg)
	versionService := services.NewVersionService(dbClient, vercelService)

	// appService (nil is ok for test)
	var appService *services.AppService

	// redisClient (nil is ok for test)
	var redisClient *redis.Client

	// Create builder
	builder := worker.NewBuilder(cfg, appService, versionService, vercelService, s3Client, redisClient)

	// Test parameters
	versionID := "22222222-aaaa-bbbb-cccc-222222222222"
	appID := "11111111-aaaa-bbbb-cccc-111111111111"
	requirements := "Display VERCEL WORKFLOW TEST with the current date and time. Keep it simple."

	fmt.Printf("Starting build for version %s, app %s\n", versionID, appID)

	// Run build
	err = builder.BuildApp(context.Background(), versionID, appID, requirements, nil, "")
	if err != nil {
		log.Fatalf("Build failed: %v", err)
	}

	fmt.Println("Build completed successfully!")
	os.Exit(0)
}
