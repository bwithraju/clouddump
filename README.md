# CloudDump: Distributed Cloud-Storage Application

CloudDump is a distributed cloud-storage system that combines multiple independent, heterogeneous storage nodes/providers into a single unified logical storage pool.

## Key Concept & Problem Statement

In modern cloud computing, storage resources are often fragmented across multiple accounts, tiers, and providers:
- Node A: 10 GB free
- Node B: 15 GB free
- Node C: 8 GB free
- **Logical Unified Pool: 33 GB**

A traditional file system cannot place a 20 GB file onto any of those individual nodes because each node's capacity is lower than the file size.

**CloudDump solves this** by streaming and splitting large files into configurable cryptographic chunks (default: 256 MB) and distributing them across storage nodes using a capacity-aware allocation algorithm. The user interacts with a single unified file (e.g. `movie.mp4` — 20 GB), while CloudDump seamlessly handles chunking, distribution, checksum integrity verification (SHA-256), parallel uploads, node health monitoring, chunk replication, and streaming reconstruction.

---

## Architectural Highlights

- **StorageProvider Abstraction**: Decoupled interface supporting `LocalStorageProvider`, `GoogleDriveProvider`, with future extensibility for OneDrive, Amazon S3, and MinIO.
- **Capacity-Aware Storage Manager**: Dynamic node capacity tracking; chunks are allocated proportional to available space rather than naive fixed ratios.
- **Streaming Chunker & Reconstructor**: Constant low memory footprint without buffering large gigabyte-scale files into RAM.
- **Resumable & Parallel Uploads**: Controlled goroutine worker pool (`MAX_UPLOAD_WORKERS`) and chunk-level resume capability.
- **Data Integrity & Replication**: SHA-256 validation per chunk during storage and retrieval to detect bit rot or missing chunks; optional replication factor (primary + replicas) across distinct physical nodes.
- **Background Job Engine**: Redis-backed asynchronous worker queues (`large_upload_processing`, `replication`, `node_health_check`, `cleanup_abandoned_uploads`, `failed_chunk_retry`).
- **Authentication & Multi-Tenant Isolation**: Secure session authentication via HTTP-only cookies; strict tenant isolation on files, chunks, nodes, and sessions.
- **Modern Web Dashboard**: Next.js (App Router), TypeScript, Tailwind CSS, TanStack Query, shadcn/ui.
- **Full Dockerization**: One-command development environment via Docker Compose (`frontend`, `backend`, `postgres`, `redis`).

---

## Quick Start: Docker Development Environment

### Prerequisites
- [Docker](https://docs.docker.com/get-docker/) (v20.10+)
- [Docker Compose](https://docs.docker.com/compose/) (v2.0+)

### 1. Clone & Configure Environment
```bash
git clone https://github.com/bwithraju/clouddump.git
cd clouddump

# Copy the example environment variables
cp .env.example .env
```

> **Security Note**: Never commit `.env` to version control. It is already included in `.gitignore`.

### 2. Start the Development Stack
```bash
docker compose up --build
```

This single command starts all 4 integrated services:
- **`postgres`**: PostgreSQL 16 database (`port 5432`) with persistent data volume and health checks.
- **`redis`**: Redis 7 cache and job queue (`port 6379`) with persistent data volume and health checks.
- **`backend`**: Go 1.24 API server (`http://localhost:8080`) with auto-migrations, parallel upload pool, background job workers, and mounted `./storage` directory.
- **`frontend`**: Next.js 16 dashboard (`http://localhost:3000`) compiled with standalone output and real-time TanStack Query API hooks.

To run in the background (detached mode):
```bash
docker compose up -d
```

To stop all services:
```bash
docker compose down
```

To stop and remove data volumes:
```bash
docker compose down -v
```

---

## Service Endpoints

| Service | URL | Description |
| :--- | :--- | :--- |
| **Frontend Web UI** | `http://localhost:3000` | CloudDump Web Dashboard, File Manager, Node Manager & Upload Interface |
| **Backend REST API** | `http://localhost:8080/api` | Go API endpoints (Files, Nodes, Uploads, Auth, OAuth, Jobs) |
| **API Health Check** | `http://localhost:8080/api/health` | Service health status and version probe |
| **PostgreSQL** | `localhost:5432` | Relational metadata store (`clouddump` database) |
| **Redis** | `localhost:6379` | Asynchronous worker queue and event cache |

---

## Local Development (Without Docker)

If you prefer running services directly on your host machine:

### 1. Start Infrastructure Services
You can run just PostgreSQL and Redis using Docker:
```bash
docker compose up -d postgres redis
```

### 2. Run Go Backend
```bash
cd backend
go run cmd/server/main.go
```
The server will bind to `http://localhost:8080`.

### 3. Run Next.js Frontend
```bash
cd frontend
npm install
npm run dev
```
The frontend will start at `http://localhost:3000`.

### 4. Running Backend Tests
```bash
cd backend
go test -v ./...
```

---

## Environment Variables

All variables are configured in `.env` (derived from `.env.example`):

| Variable | Default | Purpose |
| :--- | :--- | :--- |
| `PORT` | `8080` | Port for the Go backend API |
| `FRONTEND_PORT` | `3000` | Port for the Next.js frontend |
| `NEXT_PUBLIC_API_URL` | `http://localhost:8080/api` | API URL accessed by frontend browser/client |
| `CHUNK_SIZE_BYTES` | `268435456` (256 MB) | Chunk size for file streaming & distribution |
| `MAX_UPLOAD_WORKERS` | `4` | Concurrency worker pool size for parallel uploads |
| `STORAGE_ROOT` | `./storage` | Directory hosting simulated local storage nodes |
| `DB_HOST` | `postgres` (or `localhost`) | PostgreSQL hostname |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | Database username |
| `DB_PASSWORD` | `postgres` | Database password |
| `DB_NAME` | `clouddump` | Database name |
| `DB_SSLMODE` | `disable` | SSL mode for Postgres connection |
| `REDIS_ADDR` | `redis:6379` | Redis address for background job queues |
| `JWT_SECRET` | `super-secret-jwt-key...` | Secret key for signing authentication tokens |
| `TOKEN_EXPIRY_HOURS`| `24` | Session token validity duration |
| `GOOGLE_CLIENT_ID` | `""` | Google OAuth Client ID (optional, Phase 16) |
| `GOOGLE_CLIENT_SECRET` | `""` | Google OAuth Client Secret (optional, Phase 16) |
| `GOOGLE_REDIRECT_URL` | `http://localhost:8080/api/oauth/google/callback` | OAuth redirect URI |

---

## Project Structure

```
clouddump/
├── backend/
│   ├── cmd/server/main.go       # Server entrypoint & dependency injection
│   ├── internal/
│   │   ├── api/                 # REST HTTP handlers, auth middleware, router
│   │   ├── auth/                # Session management, JWT, Google OAuth service
│   │   ├── chunks/              # Streaming chunker, reconstructor, hasher
│   │   ├── database/            # Models, PostgreSQL repository, in-memory repo
│   │   ├── jobs/                # Redis worker pool, job queue & handlers
│   │   ├── providers/           # StorageProvider (Local, Google Drive)
│   │   ├── storage/             # StorageManager, strategies, node lifecycle
│   │   ├── uploads/             # Resumable upload sessions & chunk tracker
│   │   └── workers/             # Parallel upload worker pool
│   ├── Dockerfile               # Multi-stage Go production container
│   ├── go.mod
│   └── go.sum
├── frontend/
│   ├── app/                     # Next.js App Router (Dashboard, Files, Nodes, Upload)
│   ├── components/              # UI components & dialogs
│   ├── hooks/                   # React hooks (useUpload, useNodes, useFiles)
│   ├── lib/                     # API client & TypeScript interfaces
│   ├── public/                  # Static assets
│   ├── Dockerfile               # Multi-stage Next.js standalone container
│   ├── package.json
│   └── tsconfig.json
├── storage/                     # Simulated heterogeneous storage nodes
│   ├── node1/                   # 500 MB capacity
│   ├── node2/                   # 1024 MB capacity
│   ├── node3/                   # 2048 MB capacity
│   └── node4/                   # 700 MB capacity
├── docker-compose.yml           # Multi-service development composition
├── .env.example                 # Template for environment variables
├── .gitignore                   # Excludes .env, build output, chunk payloads
└── README.md                    # System documentation
```
