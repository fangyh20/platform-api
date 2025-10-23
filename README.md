# RapidBuild Platform API

Go backend API for the RapidBuild platform. Handles app creation, version management, deployments, and integrations with AWS S3, Vercel, and RESTHeart.

## Tech Stack

- **Go 1.24** - Backend language
- **Gorilla Mux** - HTTP router
- **PostgreSQL** (Neon) - Primary database for apps/versions/users
- **MongoDB Atlas** - App data via RESTHeart
- **AWS S3** - Code storage
- **Vercel** - App deployments
- **Redis** (Upstash) - Caching and sessions
- **JWT** - Authentication

## Features

### App Management
- Create new apps with AI-generated code
- List user's apps
- Delete apps (with cascade to versions, S3, Vercel)

### Version Control
- Create new app versions with code changes
- Track build status and logs
- Deploy versions to Vercel
- Promote versions to production
- Real-time build progress via SSE

### File Management
- Upload files to AWS S3
- Generate presigned URLs for secure access
- Organize by app and version

### Authentication
- JWT-based authentication
- Google OAuth integration
- Email/password auth
- Token refresh mechanism
- Password reset flow

### Integrations
- **Vercel**: Automated deployments
- **RESTHeart**: App database management
- **AWS S3**: Code and asset storage
- **SMTP**: Email notifications

## Environment Variables

Copy `.env.example` to `.env` and configure:

```bash
# Server
PORT=8092

# AWS S3
AWS_ACCESS_KEY=your_access_key
AWS_SECRET_KEY=your_secret_key
AWS_REGION=us-west-2
S3_BUCKET=your-bucket-name

# Vercel
VERCEL_TOKEN=your_vercel_token

# RESTHeart (MongoDB REST API)
RESTHEART_URL=https://data.rapidbuild.app
RESTHEART_API_KEY=your_api_key

# Workspace (for code generation)
WORKSPACE_DIR=/tmp/rapidbuild-workspaces
STARTER_CODE_DIR=../react-app

# PostgreSQL (Neon)
DATABASE_URL=postgresql://user:pass@host:5432/db?sslmode=require

# JWT
JWT_SECRET=your-secret-min-32-chars

# SMTP (for emails)
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USERNAME=your-email@gmail.com
SMTP_PASSWORD=your-app-password
SMTP_FROM=noreply@yourdomain.com

# Google OAuth
GOOGLE_OAUTH_CLIENT_ID=your-client-id
GOOGLE_OAUTH_CLIENT_SECRET=your-client-secret
GOOGLE_OAUTH_REDIRECT_URL_PROD=https://api.rapidbuild.app/api/v1/auth/google/callback

# Frontend URL
FRONTEND_URL=https://app.rapidbuild.app

# Redis (Upstash)
REDIS_URL=rediss://default:password@host:6379
```

## Development

### Prerequisites
- Go 1.24+
- PostgreSQL (or Neon account)
- AWS S3 account
- Vercel account
- Redis instance
- RESTHeart instance

### Setup

1. **Install dependencies:**
   ```bash
   go mod download
   ```

2. **Set up database:**
   ```bash
   # Create database schema
   psql $DATABASE_URL < config/neon_schema.sql
   ```

3. **Configure environment:**
   ```bash
   cp .env.example .env
   # Edit .env with your credentials
   ```

4. **Run development server:**
   ```bash
   go run cmd/server/main.go
   ```

Server will start on `http://localhost:8092`

### Build

```bash
# Build binary
go build -o rapidbuild cmd/server/main.go

# Run binary
./rapidbuild
```

### Testing

```bash
# Run tests
go test ./...

# Test build process (useful for debugging)
go run cmd/test_build/main.go
```

## Project Structure

```
platform-api/
├── cmd/
│   ├── server/          # Main application entry point
│   └── test_build/      # Testing utility for build process
├── config/
│   ├── config.go        # Configuration loader
│   ├── neon_schema.sql  # PostgreSQL schema
│   └── schema.sql       # Legacy schema
├── internal/
│   ├── api/             # HTTP handlers
│   │   ├── apps.go      # App CRUD endpoints
│   │   ├── auth.go      # Auth endpoints
│   │   ├── comments.go  # Comment endpoints
│   │   ├── preview.go   # Preview token generation
│   │   ├── sse.go       # Server-Sent Events for build progress
│   │   ├── upload.go    # File upload to S3
│   │   └── versions.go  # Version management
│   ├── db/
│   │   └── postgres.go  # PostgreSQL connection
│   ├── middleware/
│   │   ├── auth.go      # JWT authentication
│   │   └── cors.go      # CORS configuration
│   ├── models/
│   │   └── models.go    # Data models
│   ├── services/        # Business logic
│   │   ├── app_service.go     # App operations
│   │   ├── auth_service.go    # Authentication
│   │   ├── comment_service.go # Comments
│   │   ├── email_service.go   # Email sending
│   │   ├── oauth_service.go   # OAuth flows
│   │   ├── upload_service.go  # S3 uploads
│   │   ├── vercel_service.go  # Vercel deployments
│   │   └── version_service.go # Version management
│   └── worker/
│       └── builder.go   # Background build worker
├── go.mod               # Go dependencies
├── go.sum               # Dependency checksums
└── Dockerfile           # Docker configuration
```

## API Endpoints

### Authentication
- `POST /api/v1/auth/signup` - Create account
- `POST /api/v1/auth/login` - Login
- `POST /api/v1/auth/refresh` - Refresh token
- `POST /api/v1/auth/logout` - Logout
- `POST /api/v1/auth/forgot-password` - Request password reset
- `POST /api/v1/auth/reset-password` - Reset password
- `GET /api/v1/auth/verify-email` - Verify email
- `GET /api/v1/auth/google` - Google OAuth
- `GET /api/v1/auth/google/callback` - OAuth callback
- `GET /api/v1/auth/me` - Get current user

### Apps
- `GET /api/v1/apps` - List user's apps
- `POST /api/v1/apps` - Create new app (with AI generation)
- `GET /api/v1/apps/:id` - Get app details
- `DELETE /api/v1/apps/:id` - Delete app

### Versions
- `GET /api/v1/apps/:appId/versions` - List app versions
- `POST /api/v1/apps/:appId/versions` - Create new version
- `GET /api/v1/apps/:appId/versions/:versionId` - Get version details
- `DELETE /api/v1/apps/:appId/versions/:versionId` - Delete version
- `POST /api/v1/apps/:appId/versions/:versionId/promote` - Promote to production
- `GET /api/v1/versions/:versionId/progress` - SSE build progress

### Comments
- `GET /api/v1/apps/:appId/comments` - List comments
- `POST /api/v1/apps/:appId/comments` - Add comment
- `DELETE /api/v1/apps/:appId/comments/:commentId` - Delete comment
- `GET /api/v1/apps/:appId/versions/:versionId/comments` - Version comments

### Uploads
- `POST /api/v1/upload` - Upload file to S3

### Preview
- `POST /api/v1/apps/:appId/preview-token` - Generate preview token

### Health
- `GET /health` - Health check

## Deployment

### Production Server

1. **Build the binary:**
   ```bash
   go build -o rapidbuild cmd/server/main.go
   ```

2. **Create systemd service:**
   ```bash
   sudo nano /etc/systemd/system/rapidbuild-api.service
   ```

   Add:
   ```ini
   [Unit]
   Description=RapidBuild Platform API
   After=network.target

   [Service]
   Type=simple
   User=ubuntu
   WorkingDirectory=/home/ubuntu/projects/rapidbuildapp/platform-api
   ExecStart=/home/ubuntu/projects/rapidbuildapp/platform-api/rapidbuild
   Restart=always
   RestartSec=10

   [Install]
   WantedBy=multi-user.target
   ```

3. **Enable and start:**
   ```bash
   sudo systemctl enable rapidbuild-api
   sudo systemctl start rapidbuild-api
   ```

### Docker Deployment

```bash
# Build image
docker build -t rapidbuild-api .

# Run container
docker run -d \
  --name rapidbuild-api \
  -p 8092:8092 \
  --env-file .env \
  rapidbuild-api
```

### Nginx Configuration

The API is exposed via nginx reverse proxy. See the root `NGINX_SETUP.md` for configuration.

**Production URL:** https://api.rapidbuild.app

## Architecture

### Build Flow

1. User creates app via UI
2. API generates starter code from `react-app` template
3. Background worker builds and deploys to Vercel
4. Real-time progress sent via SSE
5. Code uploaded to S3
6. Database schema applied via RESTHeart
7. Deployment URL returned to user

### Database Schema

**PostgreSQL (Neon):**
- `users` - Platform users
- `apps` - User applications
- `versions` - App versions
- `comments` - Collaboration comments

**MongoDB (via RESTHeart):**
- Managed per-app databases
- Schema defined by JSON Schema files
- Accessed via SDK by user apps

### Authentication Flow

1. User logs in via Platform UI
2. JWT issued by Platform API
3. Token stored in browser
4. Token included in all API requests
5. Auto-refresh on expiration

## Environment Variables Reference

| Variable | Description | Example |
|----------|-------------|---------|
| `PORT` | Server port | `8092` |
| `DATABASE_URL` | PostgreSQL connection | `postgresql://...` |
| `RESTHEART_URL` | Data service endpoint | `https://data.rapidbuild.app` |
| `AWS_ACCESS_KEY` | S3 access key | `AKIA...` |
| `VERCEL_TOKEN` | Vercel API token | `xxx` |
| `JWT_SECRET` | JWT signing secret | Min 32 chars |
| `FRONTEND_URL` | Platform UI URL | `https://app.rapidbuild.app` |

## Monitoring

### Health Check
```bash
curl https://api.rapidbuild.app/health
```

### Logs
```bash
# If using systemd
sudo journalctl -u rapidbuild-api -f

# Application logs
tail -f /path/to/rapidbuild.log
```

## Troubleshooting

### Build fails
- Check WORKSPACE_DIR is writable
- Verify STARTER_CODE_DIR points to react-app
- Check Vercel token is valid

### Database connection fails
- Verify DATABASE_URL is correct
- Check Neon database is active
- Ensure SSL mode is set

### S3 upload fails
- Verify AWS credentials
- Check bucket exists and is accessible
- Verify bucket region matches AWS_REGION

---

**Deployed at:** https://api.rapidbuild.app
**Repository:** github.com/fangyh20/platform-api
**Version:** 1.0.0
