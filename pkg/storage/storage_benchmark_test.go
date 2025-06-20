package storage

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkMemoryStorage benchmarks memory storage operations
func BenchmarkMemoryStorage(t *testing.B) {
	storage := NewMemoryStorage()
	
	// Create test sessions
	sessions := make([]*SessionData, 1000)
	for i := 0; i < 1000; i++ {
		sessions[i] = &SessionData{
			ID:        fmt.Sprintf("bench-session-%d", i),
			Port:      9000 + i,
			StartedAt: time.Now(),
			UserID:    fmt.Sprintf("user-%d", i),
			Status:    "active",
			Environment: map[string]string{
				"VAR1": fmt.Sprintf("value-%d", i),
				"VAR2": "constant-value",
			},
			Tags: map[string]string{
				"env":  "benchmark",
				"iter": fmt.Sprintf("%d", i),
			},
		}
	}
	
	t.Run("Save", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			session := sessions[i%len(sessions)]
			session.ID = fmt.Sprintf("bench-save-%d-%d", i, session.Port)
			if err := storage.Save(session); err != nil {
				b.Errorf("Save failed: %v", err)
			}
		}
	})
	
	t.Run("Load", func(b *testing.B) {
		// Pre-populate storage
		for _, session := range sessions {
			if err := storage.Save(session); err != nil {
				b.Errorf("Pre-populate save failed: %v", err)
			}
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			sessionID := sessions[i%len(sessions)].ID
			if _, err := storage.Load(sessionID); err != nil {
				b.Errorf("Load failed: %v", err)
			}
		}
	})
	
	t.Run("LoadAll", func(b *testing.B) {
		// Pre-populate storage
		for _, session := range sessions {
			if err := storage.Save(session); err != nil {
				b.Errorf("Pre-populate save failed: %v", err)
			}
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := storage.LoadAll(); err != nil {
				b.Errorf("LoadAll failed: %v", err)
			}
		}
	})
	
	t.Run("Update", func(b *testing.B) {
		// Pre-populate storage
		for _, session := range sessions {
			if err := storage.Save(session); err != nil {
				b.Errorf("Pre-populate save failed: %v", err)
			}
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			session := sessions[i%len(sessions)]
			session.Status = fmt.Sprintf("updated-%d", i)
			if err := storage.Update(session); err != nil {
				b.Errorf("Update failed: %v", err)
			}
		}
	})
	
	t.Run("Delete", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Create and save session
			session := &SessionData{
				ID:     fmt.Sprintf("bench-delete-%d", i),
				Port:   9000 + i,
				UserID: "user",
				Status: "active",
			}
			if err := storage.Save(session); err != nil {
				b.Errorf("Save failed: %v", err)
			}
			
			// Delete it
			if err := storage.Delete(session.ID); err != nil {
				b.Errorf("Delete failed: %v", err)
			}
		}
	})
}

// BenchmarkFileStorage benchmarks file storage operations
func BenchmarkFileStorage(t *testing.B) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "benchmark.json")
	
	storage, err := NewFileStorage(tmpFile, 0, false) // Disable sync for benchmarks
	if err != nil {
		t.Fatalf("Failed to create file storage: %v", err)
	}
	defer storage.Close()
	
	// Create test sessions
	sessions := make([]*SessionData, 100) // Smaller set for file operations
	for i := 0; i < 100; i++ {
		sessions[i] = &SessionData{
			ID:        fmt.Sprintf("file-bench-session-%d", i),
			Port:      9000 + i,
			StartedAt: time.Now(),
			UserID:    fmt.Sprintf("user-%d", i),
			Status:    "active",
			Environment: map[string]string{
				"VAR1": fmt.Sprintf("value-%d", i),
				"VAR2": "constant-value",
			},
			Tags: map[string]string{
				"env":  "benchmark",
				"iter": fmt.Sprintf("%d", i),
			},
		}
	}
	
	t.Run("Save", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			session := sessions[i%len(sessions)]
			session.ID = fmt.Sprintf("file-bench-save-%d-%d", i, session.Port)
			if err := storage.Save(session); err != nil {
				b.Errorf("Save failed: %v", err)
			}
		}
	})
	
	t.Run("Load", func(b *testing.B) {
		// Pre-populate storage
		for _, session := range sessions {
			if err := storage.Save(session); err != nil {
				b.Errorf("Pre-populate save failed: %v", err)
			}
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			sessionID := sessions[i%len(sessions)].ID
			if _, err := storage.Load(sessionID); err != nil {
				b.Errorf("Load failed: %v", err)
			}
		}
	})
	
	t.Run("LoadAll", func(b *testing.B) {
		// Pre-populate storage
		for _, session := range sessions {
			if err := storage.Save(session); err != nil {
				b.Errorf("Pre-populate save failed: %v", err)
			}
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := storage.LoadAll(); err != nil {
				b.Errorf("LoadAll failed: %v", err)
			}
		}
	})
}

// BenchmarkFileStorageWithEncryption benchmarks file storage with encryption
func BenchmarkFileStorageWithEncryption(t *testing.B) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "encrypted_benchmark.json")
	
	storage, err := NewFileStorage(tmpFile, 0, true) // Enable encryption
	if err != nil {
		t.Fatalf("Failed to create encrypted file storage: %v", err)
	}
	defer storage.Close()
	
	// Create test sessions with sensitive data
	sessions := make([]*SessionData, 50) // Smaller set for encryption operations
	for i := 0; i < 50; i++ {
		sessions[i] = &SessionData{
			ID:        fmt.Sprintf("encrypted-bench-session-%d", i),
			Port:      9000 + i,
			StartedAt: time.Now(),
			UserID:    fmt.Sprintf("user-%d", i),
			Status:    "active",
			Environment: map[string]string{
				"GITHUB_TOKEN": fmt.Sprintf("secret-token-%d", i),
				"API_KEY":      fmt.Sprintf("api-key-%d", i),
				"NORMAL_VAR":   fmt.Sprintf("normal-value-%d", i),
			},
		}
	}
	
	t.Run("SaveWithEncryption", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			session := sessions[i%len(sessions)]
			session.ID = fmt.Sprintf("encrypted-bench-save-%d-%d", i, session.Port)
			if err := storage.Save(session); err != nil {
				b.Errorf("Save failed: %v", err)
			}
		}
	})
	
	t.Run("LoadWithDecryption", func(b *testing.B) {
		// Pre-populate storage
		for _, session := range sessions {
			if err := storage.Save(session); err != nil {
				b.Errorf("Pre-populate save failed: %v", err)
			}
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			sessionID := sessions[i%len(sessions)].ID
			if _, err := storage.Load(sessionID); err != nil {
				b.Errorf("Load failed: %v", err)
			}
		}
	})
}

// BenchmarkStorageFactory benchmarks storage factory operations
func BenchmarkStorageFactory(t *testing.B) {
	tmpDir := t.TempDir()
	
	configs := []*StorageConfig{
		{Type: "memory"},
		{
			Type:     "file",
			FilePath: filepath.Join(tmpDir, "factory_bench.json"),
		},
		{
			Type:           "file",
			FilePath:       filepath.Join(tmpDir, "factory_bench_encrypted.json"),
			EncryptSecrets: true,
		},
	}
	
	t.Run("CreateStorage", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			config := configs[i%len(configs)]
			if config.Type == "file" {
				// Use unique file names to avoid conflicts
				config.FilePath = filepath.Join(tmpDir, fmt.Sprintf("factory_bench_%d.json", i))
			}
			
			storage, err := NewStorage(config)
			if err != nil {
				b.Errorf("Failed to create storage: %v", err)
				continue
			}
			
			if storage != nil {
				if err := storage.Close(); err != nil {
					b.Errorf("Failed to close storage: %v", err)
				}
			}
		}
	})
}

// BenchmarkConcurrentAccess benchmarks concurrent access patterns
func BenchmarkConcurrentAccess(t *testing.B) {
	storage := NewMemoryStorage()
	
	// Pre-populate with some sessions
	for i := 0; i < 100; i++ {
		session := &SessionData{
			ID:     fmt.Sprintf("concurrent-session-%d", i),
			Port:   9000 + i,
			UserID: fmt.Sprintf("user-%d", i),
			Status: "active",
		}
		if err := storage.Save(session); err != nil {
			t.Fatalf("Failed to pre-populate storage: %v", err)
		}
	}
	
	t.Run("ConcurrentReads", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				sessionID := fmt.Sprintf("concurrent-session-%d", i%100)
				_, _ = storage.Load(sessionID) // Ignore errors in benchmark
				i++
			}
		})
	})
	
	t.Run("ConcurrentWrites", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				session := &SessionData{
					ID:     fmt.Sprintf("concurrent-write-session-%d", i),
					Port:   10000 + i,
					UserID: "benchmark-user",
					Status: "active",
				}
				_ = storage.Save(session) // Ignore errors in benchmark
				i++
			}
		})
	})
	
	t.Run("MixedOperations", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				switch i % 4 {
				case 0: // Save
					session := &SessionData{
						ID:     fmt.Sprintf("mixed-session-%d", i),
						Port:   11000 + i,
						UserID: "benchmark-user",
						Status: "active",
					}
					_ = storage.Save(session) // Ignore errors in benchmark
				case 1: // Load
					sessionID := fmt.Sprintf("concurrent-session-%d", i%100)
					_, _ = storage.Load(sessionID) // Ignore errors in benchmark
				case 2: // Update
					session := &SessionData{
						ID:     fmt.Sprintf("concurrent-session-%d", i%100),
						Port:   9000 + (i % 100),
						UserID: "updated-user",
						Status: "updated",
					}
					_ = storage.Update(session) // Ignore errors in benchmark
				case 3: // LoadAll
					_, _ = storage.LoadAll() // Ignore errors in benchmark
				}
				i++
			}
		})
	})
}