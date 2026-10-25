package workers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/bwithraju/clouddump/backend/internal/providers"
	"github.com/bwithraju/clouddump/backend/internal/storage"
)

func setupBenchEnvironment(b *testing.B) (*storage.StorageManager, map[string]string) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())

	dirs := make(map[string]string)
	for i := 1; i <= 4; i++ {
		nodeID := fmt.Sprintf("bench_node%d", i)
		d, err := os.MkdirTemp("", nodeID+"_*")
		if err != nil {
			b.Fatalf("failed creating temp dir: %v", err)
		}
		p, err := providers.NewLocalStorageProvider(d, 500*1024*1024)
		if err != nil {
			b.Fatalf("failed creating provider: %v", err)
		}
		n, err := storage.NewStorageNode(nodeID, "Bench Node "+fmt.Sprint(i), p)
		if err != nil {
			b.Fatalf("failed creating node: %v", err)
		}
		if err := mgr.RegisterNode(n); err != nil {
			b.Fatalf("failed registering node: %v", err)
		}
		dirs[nodeID] = d
	}

	return mgr, dirs
}

func cleanupBenchDirs(dirs map[string]string) {
	for _, d := range dirs {
		_ = os.RemoveAll(d)
	}
}

// Benchmark: Sequential upload (1 worker) vs Parallel upload (2, 4, 8 workers)
func BenchmarkUpload_Sequential_vs_Parallel(b *testing.B) {
	chunkSize := int64(64 * 1024) // 64 KB per chunk
	numChunks := 16
	fileSize := chunkSize * int64(numChunks)

	payload := make([]byte, fileSize)
	for i := range payload {
		payload[i] = byte(i % 127)
	}

	workerConfigs := []struct {
		name    string
		workers int
	}{
		{"Sequential_1Worker", 1},
		{"Parallel_2Workers", 2},
		{"Parallel_4Workers", 4},
		{"Parallel_8Workers", 8},
	}

	for _, wc := range workerConfigs {
		b.Run(wc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(fileSize)

			for n := 0; n < b.N; n++ {
				b.StopTimer()
				mgr, dirs := setupBenchEnvironment(b)
				uploader, err := NewParallelUploader(mgr, chunkSize, wc.workers)
				if err != nil {
					b.Fatalf("failed initializing uploader: %v", err)
				}
				fileID := fmt.Sprintf("bench_file_%d_%d", wc.workers, n)
				reader := bytes.NewReader(payload)
				b.StartTimer()

				_, err = uploader.Distribute(context.Background(), fileID, reader, fileSize, nil)
				if err != nil {
					b.Fatalf("upload failed: %v", err)
				}

				b.StopTimer()
				cleanupBenchDirs(dirs)
				b.StartTimer()
			}
		})
	}
}
