# CloudDump: Distributed Cloud-Storage Application

CloudDump is a distributed cloud-storage system that combines multiple independent, heterogeneous storage nodes/providers into a single unified logical storage pool.

## Key Concept & Problem Statement

In modern cloud computing, storage resources are often fragmented across multiple accounts, tiers, and providers:
- Node A: 10 GB free
- Node B: 15 GB free
- Node C: 8 GB free
- **Logical Unified Pool: 33 GB**

A traditional file system cannot place a 20 GB file onto any of those individual nodes because each node's capacity is lower than the file size.

**CloudDump solves this** by streaming and splitting large files into configurable cryptographic chunks (default: 256 MB) and distributing them across storage nodes using a capacity-aware allocation algorithm. The user interacts with a single unified file (e.g. `movie.mp4` — 20 GB), while CloudDump seamlessly handles chunking, distribution, checksum integrity verification (SHA-256), parallel uploads, node health monitoring, and streaming reconstruction.

---

## Architectural Highlights

- **StorageProvider Abstraction**: Decoupled interface supporting `LocalStorageProvider`, with architectural pluggability for `GoogleDriveProvider`, OneDrive, Amazon S3, and MinIO.
- **Capacity-Aware Storage Manager**: Dynamic node capacity tracking; chunks are allocated proportional to available space rather than fixed naive ratios.
- **Streaming Chunker & Reconstructor**: Constant low memory footprint without buffering large gigabyte-scale files into RAM.
- **Resumable & Parallel Uploads**: Worker pool concurrency (`MAX_UPLOAD_WORKERS`) and chunk-level resume capability.
- **Data Integrity**: SHA-256 validation per chunk during storage and retrieval to detect and report bit rot or missing chunks.
- **Dual-Stack Architecture**:
  - **Backend**: Go (Go 1.24+), REST API, streaming I/O, controlled goroutine workers.
  - **Database**: PostgreSQL with structured relational migrations (files, chunks, storage nodes).
  - **Queue / Async**: Redis background workers.
  - **Frontend**: Next.js (App Router), TypeScript, Tailwind CSS, TanStack Query, shadcn/ui.
  - **DevOps**: Docker & Docker Compose.

---

## Project Structure

```
clouddump/
├── backend/
│   ├── cmd/
│   │   └── server/
│   │       └── main.go
│   ├── internal/
│   │   ├── api/          # HTTP handlers and routing
│   │   ├── auth/         # Authentication and authorization
│   │   ├── chunks/       # Streaming chunker & verification
│   │   ├── database/     # DB models, connection, repository
│   │   ├── files/        # File management & metadata service
│   │   ├── providers/    # StorageProvider interface & implementations
│   │   ├── storage/      # StorageManager & allocation algorithms
│   │   └── workers/      # Concurrency & background processing
│   ├── migrations/       # PostgreSQL migration scripts
│   ├── go.mod
│   └── go.sum
├── frontend/
│   ├── app/              # Next.js App Router (Dashboard, Files, Nodes, etc.)
│   ├── components/       # UI & reusable components
│   ├── hooks/            # Custom React hooks (upload, status)
│   ├── lib/              # API clients & utilities
│   ├── package.json
│   └── tsconfig.json
├── storage/              # Local storage nodes for simulation
│   ├── node1/
│   ├── node2/
│   ├── node3/
│   └── node4/
├── docker-compose.yml
├── .env.example
├── .gitignore
└── README.md
```

---

## Development Milestones

- **Phase 1**: Initialize repository, Go server skeleton, Next.js frontend, verify environments.
- **Phase 2**: StorageProvider interface, LocalStorageProvider, StorageManager.
- **Phase 3**: Streaming file chunking, chunk metadata, dynamic allocation.
- **Phase 4**: File reconstruction, SHA-256 checksums, deletion.
- **Phase 5**: PostgreSQL schema, migrations, metadata persistence.
- **Phase 6**: REST API endpoints for files and storage nodes.
- **Phase 7**: Resumable uploads, progress tracking, parallel worker pools.
- **Phase 8**: Redis background worker queues.
- **Phase 9**: Node failure detection, offline handling, replication.
- **Phase 10**: Google Drive storage provider integration.
- **Phase 11**: Frontend dashboard, file manager, node monitor, upload UI.
- **Phase 12**: Docker compose, end-to-end integration tests, documentation.
