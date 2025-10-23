package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	appConfig "github.com/rapidbuildapp/rapidbuild/config"
	"github.com/rapidbuildapp/rapidbuild/internal/api"
	"github.com/rapidbuildapp/rapidbuild/internal/db"
	"github.com/rapidbuildapp/rapidbuild/internal/middleware"
	"github.com/rapidbuildapp/rapidbuild/internal/services"
	"github.com/rapidbuildapp/rapidbuild/internal/worker"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	// Load configuration
	cfg := appConfig.Load()

	// Initialize PostgreSQL client
	pgClient, err := db.NewPostgresClient(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pgClient.Close()

	log.Println("Successfully connected to PostgreSQL database")

	// Initialize MongoDB client
	mongoCtx, mongoCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer mongoCancel()

	mongoClient, err := mongo.Connect(mongoCtx, options.Client().ApplyURI(cfg.MongoURL))
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer func() {
		if err := mongoClient.Disconnect(context.Background()); err != nil {
			log.Printf("Error disconnecting MongoDB: %v", err)
		}
	}()

	// Ping MongoDB to verify connection
	if err := mongoClient.Ping(mongoCtx, nil); err != nil {
		log.Fatalf("Failed to ping MongoDB: %v", err)
	}

	log.Println("Successfully connected to MongoDB")

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

	// Initialize services
	emailService := services.NewEmailService(cfg)
	authService := services.NewAuthService(pgClient, cfg, emailService)
	oauthService := services.NewOAuthService(pgClient, cfg, authService)

	// Initialize services
	appService := services.NewAppService(pgClient)
	versionService := services.NewVersionService(pgClient)
	commentService := services.NewCommentService(pgClient)
	uploadService := services.NewUploadService(pgClient, s3Client, cfg)
	vercelService := services.NewVercelService(cfg)

	// Initialize Redis client (Upstash)
	var redisClient *redis.Client
	if cfg.RedisURL != "" {
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			log.Fatalf("Failed to parse Redis URL: %v", err)
		}
		redisClient = redis.NewClient(opt)

		// Test connection
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := redisClient.Ping(ctx).Err(); err != nil {
			log.Fatalf("Failed to connect to Redis: %v", err)
		}
		log.Println("Successfully connected to Redis (Upstash)")
	} else {
		log.Println("Warning: Redis URL not configured, SSE will not work")
	}

	// Initialize worker
	builder := worker.NewBuilder(cfg, appService, versionService, vercelService, s3Client, redisClient)

	// Initialize API handlers
	authHandler := api.NewAuthHandler(authService, oauthService, cfg)
	appHandler := api.NewAppHandler(appService, versionService, commentService, builder)
	uploadHandler := api.NewUploadHandler(uploadService)
	previewHandler := api.NewPreviewHandler(appService, versionService, mongoClient)

	// Setup router
	r := mux.NewRouter()

	// Apply CORS middleware globally
	r.Use(middleware.CORSMiddleware)

	// Public routes (no auth required)
	r.HandleFunc("/health", healthCheck).Methods("GET")

	// Auth routes (public)
	authRoutes := r.PathPrefix("/api/v1/auth").Subrouter()
	authRoutes.HandleFunc("/signup", authHandler.Signup).Methods("POST", "OPTIONS")
	authRoutes.HandleFunc("/login", authHandler.Login).Methods("POST", "OPTIONS")
	authRoutes.HandleFunc("/verify-email", authHandler.VerifyEmail).Methods("GET", "OPTIONS")
	authRoutes.HandleFunc("/forgot-password", authHandler.ForgotPassword).Methods("POST", "OPTIONS")
	authRoutes.HandleFunc("/reset-password", authHandler.ResetPassword).Methods("POST", "OPTIONS")
	authRoutes.HandleFunc("/google", authHandler.GoogleAuth).Methods("GET", "OPTIONS")
	authRoutes.HandleFunc("/google/callback", authHandler.GoogleCallback).Methods("GET", "OPTIONS")
	authRoutes.HandleFunc("/refresh", authHandler.RefreshToken).Methods("POST", "OPTIONS")
	authRoutes.HandleFunc("/logout", authHandler.Logout).Methods("POST", "OPTIONS")

	// Protected routes (require authentication)
	protectedAuth := r.PathPrefix("/api/v1/auth").Subrouter()
	protectedAuth.Use(middleware.AuthMiddleware(cfg))
	protectedAuth.HandleFunc("/me", authHandler.GetCurrentUser).Methods("GET", "OPTIONS")

	// Protected app routes
	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(middleware.AuthMiddleware(cfg))

	// App routes
	api.HandleFunc("/apps", appHandler.ListApps).Methods("GET", "OPTIONS")
	api.HandleFunc("/apps", appHandler.CreateApp).Methods("POST", "OPTIONS")
	api.HandleFunc("/apps/{id}", appHandler.GetApp).Methods("GET", "OPTIONS")
	api.HandleFunc("/apps/{id}", appHandler.DeleteApp).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/apps/{id}/preview-token", previewHandler.GeneratePreviewToken).Methods("POST", "OPTIONS")

	// Version routes
	api.HandleFunc("/apps/{appId}/versions", appHandler.ListVersions).Methods("GET", "OPTIONS")
	api.HandleFunc("/apps/{appId}/versions", appHandler.CreateVersion).Methods("POST", "OPTIONS")
	api.HandleFunc("/apps/{appId}/versions/{versionId}", appHandler.GetVersion).Methods("GET", "OPTIONS")
	api.HandleFunc("/apps/{appId}/versions/{versionId}", appHandler.DeleteVersion).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/apps/{appId}/versions/{versionId}/promote", appHandler.PromoteVersion).Methods("POST", "OPTIONS")

	// Comment routes
	api.HandleFunc("/apps/{appId}/comments", appHandler.ListComments).Methods("GET", "OPTIONS")
	api.HandleFunc("/apps/{appId}/comments", appHandler.AddComment).Methods("POST", "OPTIONS")
	api.HandleFunc("/apps/{appId}/comments/{commentId}", appHandler.DeleteComment).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/apps/{appId}/versions/{versionId}/comments", appHandler.GetVersionComments).Methods("GET", "OPTIONS")

	// Upload routes
	api.HandleFunc("/apps/{appId}/versions/{versionId}/upload", uploadHandler.UploadRequirementFile).Methods("POST", "OPTIONS")

	// SSE route for build progress
	api.HandleFunc("/versions/{versionId}/progress", appHandler.SSEHandler).Methods("GET", "OPTIONS")

	// Serve static files from frontend build
	frontendPath := "../frontend/dist"
	r.PathPrefix("/assets/").Handler(http.StripPrefix("/assets/", http.FileServer(http.Dir(frontendPath+"/assets"))))

	// Serve index.html for all other routes (SPA fallback)
	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, frontendPath+"/index.html")
	})

	// Create server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // Disable write timeout for SSE support (long-running connections)
		IdleTimeout:  60 * time.Second,
	}

	// Start server
	go func() {
		log.Printf("Starting server on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"healthy"}`)
}
