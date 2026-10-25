-- CloudDump PostgreSQL Initial Schema Migration
-- Down Migration: drop all tables in reverse dependency order

DROP TABLE IF EXISTS oauth_tokens CASCADE;
DROP TABLE IF EXISTS chunk_replicas CASCADE;
DROP TABLE IF EXISTS chunks CASCADE;
DROP TABLE IF EXISTS files CASCADE;
DROP TABLE IF EXISTS storage_nodes CASCADE;
DROP TABLE IF EXISTS users CASCADE;
