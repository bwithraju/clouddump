# CloudDump Production Deployment Guide

This guide details instructions, architectural considerations, and best practices for deploying CloudDump to production environments.

---

## 1. Production Architecture Overview

In production, CloudDump is deployed as a resilient, multi-container system behind an SSL/TLS termination reverse proxy:

```
[ Internet / HTTPS Clients ]
            │
            ▼
┌──────────────────────────────────────┐
│  Reverse Proxy (Caddy / Nginx)       │
│  - SSL/TLS Termination (Let's Encrypt)│
│  - HSTS & Security Headers           │
│  - Streaming Proxy (no buffer)       │
└───────────┬──────────────────────────┘
            │
      ┌─────┴──────────────────┐
      │ :3000                  │ :8080
      ▼                        ▼
┌──────────────┐         ┌──────────────┐
│   Frontend   │         │   Backend    │
│  (Next.js)   │         │ (Go 1.24 API)│
└──────────────┘         └──────┬───────┘
                                │
                 ┌──────────────┴──────────────┐
                 ▼                             ▼
         ┌──────────────┐              ┌──────────────┐
         │  PostgreSQL  │              │    Redis     │
         │ (Metadata)   │              │ (Job Queue)  │
         └──────────────┘              └──────────────┘
```

---

## 2. Pre-Deployment Checklist

- [ ] **Generate Secrets**: Create strong, cryptographically random secrets for `JWT_SECRET`, `DB_PASSWORD`, and `REDIS_PASSWORD` (minimum 32 characters).
- [ ] **Configure Domain & DNS**: Route your domain (e.g. `clouddump.example.com`) to the host public IP.
- [ ] **Provision Persistent Storage**: Ensure persistent disk volumes or NVMe mounts are attached for `/var/lib/postgresql/data`, `/data` (Redis), and `/app/storage` (Chunks).
- [ ] **Google Drive Credentials (Optional)**: If using Google Drive storage nodes, register an OAuth 2.0 Web Application in the Google Cloud Console and set redirect URI to `https://<your-domain>/api/oauth/google/callback`.
- [ ] **Firewall Rules**: Expose ports `80` (HTTP redirect) and `443` (HTTPS) externally. Keep ports `5432` (PostgreSQL), `6379` (Redis), and `8080` (Backend) private.

---

## 3. Environment Configuration

Create a production `.env` file on the deployment host (never commit `.env` into git):

```bash
# Server & Environment
PORT=8080
ENVIRONMENT=production
LOG_FORMAT=json

# Domain & CORS
FRONTEND_PORT=3000
NEXT_PUBLIC_API_URL=https://clouddump.example.com/api
ALLOWED_ORIGINS=https://clouddump.example.com

# Database (PostgreSQL)
DB_HOST=postgres
DB_PORT=5432
DB_USER=clouddump_user
DB_PASSWORD=USE_A_STRONG_RANDOM_PASSWORD_HERE_32CHARS
DB_NAME=clouddump
DB_SSLMODE=disable
AUTO_MIGRATE=true

# Redis & Background Workers
REDIS_ADDR=redis:6379
REDIS_PASSWORD=USE_A_STRONG_REDIS_PASSWORD_HERE

# Security & Cookies
JWT_SECRET=USE_A_SUPER_SECRET_RANDOM_KEY_AT_LEAST_32_BYTES_LONG
TOKEN_EXPIRY_HOURS=24
COOKIE_SECURE=true

# Storage Engine
STORAGE_ROOT=/app/storage
STORAGE_LOCAL_ROOT=/app/storage
CHUNK_SIZE_BYTES=268435456
MAX_UPLOAD_WORKERS=8

# Google Drive (Optional)
GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=your-client-secret
GOOGLE_REDIRECT_URL=https://clouddump.example.com/api/oauth/google/callback
```

---

## 4. Database Migration Strategy

CloudDump includes a dual-strategy migration framework:

### Strategy A: Automated In-App Migrations (`AUTO_MIGRATE=true`)
By default, the backend server executes `database.Migrate(ctx, db)` inside an atomic transaction on startup. It applies table creation DDL and index generation idempotently with `IF NOT EXISTS`.

### Strategy B: Standalone Migration Files
For enterprise pipelines, Kubernetes init containers, or CI/CD pipelines, standalone SQL migrations are maintained in `backend/migrations/`:
- `000001_init_schema.up.sql` (Creates `users`, `storage_nodes`, `files`, `chunks`, `chunk_replicas`, and `oauth_tokens`).
- `000001_init_schema.down.sql` (Drops tables in reverse foreign key order).

Run migrations with `golang-migrate`:
```bash
migrate -path backend/migrations -database "postgres://clouddump_user:PASSWORD@localhost:5432/clouddump?sslmode=disable" up
```

---

## 5. Deployment with Docker Compose

Deploy the stack using the base compose file and the production overlay:

```bash
# 1. Pull latest code
git clone https://github.com/bwithraju/clouddump.git /opt/clouddump
cd /opt/clouddump

# 2. Setup production environment variables
cp .env.example .env
# Edit .env with your production secrets
nano .env

# 3. Build and launch services in detached mode
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build

# 4. Verify running services and health status
docker compose ps
```

---

## 6. Reverse Proxy Configuration

### Caddy (Recommended)
Caddy automatically manages SSL/TLS certificates via Let's Encrypt and supports streaming without buffering:

```caddyfile
clouddump.example.com {
    # Route API and health endpoints to backend
    handle /api/* {
        reverse_proxy backend:8080 {
            flush_interval -1
        }
    }
    handle /health {
        reverse_proxy backend:8080
    }
    handle /ready {
        reverse_proxy backend:8080
    }

    # Route all other traffic to Next.js frontend
    handle {
        reverse_proxy frontend:3000
    }
}
```

### Nginx
```nginx
server {
    listen 80;
    server_name clouddump.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name clouddump.example.com;

    ssl_certificate /etc/letsencrypt/live/clouddump.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/clouddump.example.com/privkey.pem;

    client_max_body_size 0; # Unlimited upload size for streaming
    proxy_request_buffering off; # Critical for streaming uploads without disk buffering
    proxy_buffering off; # Critical for streaming chunk downloads

    # Health & Readiness checks
    location /health {
        proxy_pass http://127.0.0.1:8080/health;
        proxy_set_header Host $host;
    }
    location /ready {
        proxy_pass http://127.0.0.1:8080/ready;
        proxy_set_header Host $host;
    }

    # API Endpoints
    location /api/ {
        proxy_pass http://127.0.0.1:8080/api/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }

    # Frontend
    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

---

## 7. Health & Readiness Monitoring

### Liveness Probe
`GET /health` (or `GET /api/health`):
- Verifies server process is responsive.
- Returns `200 OK` with:
```json
{
  "status": "healthy",
  "service": "clouddump-backend",
  "version": "1.0.0-prod",
  "timestamp": "2026-10-26T00:00:00Z"
}
```

### Readiness Probe
`GET /ready` (or `GET /api/ready`):
- Actively pings PostgreSQL (`db.PingContext`) and Redis (`redis.Ping`).
- Returns `200 OK` when all dependencies are connected:
```json
{
  "status": "ready",
  "checks": {
    "database": "ready",
    "job_queue": "ready"
  },
  "timestamp": "2026-10-26T00:00:00Z"
}
```
- If a dependency is offline, returns `503 Service Unavailable`:
```json
{
  "status": "not_ready",
  "checks": {
    "database": "unhealthy: connection refused",
    "job_queue": "ready"
  },
  "timestamp": "2026-10-26T00:00:00Z"
}
```

---

## 8. Backup & Disaster Recovery

### 1. PostgreSQL Metadata Backup
Automate daily database snapshots:
```bash
docker exec -t clouddump-postgres pg_dump -U clouddump_user clouddump | gzip > /backups/clouddump_meta_$(date +%Y%m%d_%H%M%S).sql.gz
```

### 2. Physical Chunk Data Backup
The `./storage` directory contains all stored chunks. Synchronize to external backup storage:
```bash
rsync -avz --delete /opt/clouddump/storage/ /backup-server/clouddump-storage/
```

### 3. Restoration Procedure
```bash
# Restore PostgreSQL
gunzip < /backups/clouddump_meta_YYYYMMDD_HHMMSS.sql.gz | docker exec -i clouddump-postgres psql -U clouddump_user -d clouddump

# Restore Chunks
rsync -avz /backup-server/clouddump-storage/ /opt/clouddump/storage/
```

---

## 9. Security & Hardening

1. **Non-Root Execution**: Both `backend` (UID 10001 `appuser`) and `frontend` (UID 1001 `nextjs`) run as unprivileged system users inside Alpine Linux containers.
2. **Secure Cookies**: When `ENVIRONMENT=production` or `COOKIE_SECURE=true`, cookies are strictly transmitted with `Secure`, `HttpOnly`, and `SameSite=Lax`.
3. **CORS Validation**: Cross-Origin Resource Sharing is validated against `ALLOWED_ORIGINS` rather than using wildcard origins.
4. **Credential Isolation**: OAuth tokens and database passwords are never exposed in responses or client code.
