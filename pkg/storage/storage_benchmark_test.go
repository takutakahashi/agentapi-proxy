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
			storage.Save(session)
		}
	})
	
	t.Run("Load", func(b *testing.B) {
		// Pre-populate storage
		for _, session := range sessions {
			storage.Save(session)
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			sessionID := sessions[i%len(sessions)].ID
			storage.Load(sessionID)
		}
	})
	
	t.Run("LoadAll", func(b *testing.B) {
		// Pre-populate storage
		for _, session := range sessions {
			storage.Save(session)
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			storage.LoadAll()
		}
	})
	
	t.Run("Update", func(b *testing.B) {
		// Pre-populate storage
		for _, session := range sessions {
			storage.Save(session)
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			session := sessions[i%len(sessions)]
			session.Status = fmt.Sprintf("updated-%d", i)
			storage.Update(session)
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
			storage.Save(session)
			
			// Delete it
			storage.Delete(session.ID)
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
			storage.Save(session)
		}
	})
	
	t.Run("Load", func(b *testing.B) {
		// Pre-populate storage
		for _, session := range sessions {
			storage.Save(session)
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			sessionID := sessions[i%len(sessions)].ID
			storage.Load(sessionID)
		}
	})
	
	t.Run("LoadAll", func(b *testing.B) {
		// Pre-populate storage
		for _, session := range sessions {
			storage.Save(session)
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			storage.LoadAll()
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
			storage.Save(session)
		}
	})
	
	t.Run("LoadWithDecryption", func(b *testing.B) {
		// Pre-populate storage
		for _, session := range sessions {
			storage.Save(session)
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			sessionID := sessions[i%len(sessions)].ID
			storage.Load(sessionID)
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
				storage.Close()
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
		storage.Save(session)
	}
	
	t.Run("ConcurrentReads", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				sessionID := fmt.Sprintf("concurrent-session-%d", i%100)
				storage.Load(sessionID)
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
				storage.Save(session)
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
					storage.Save(session)
				case 1: // Load
					sessionID := fmt.Sprintf("concurrent-session-%d", i%100)
					storage.Load(sessionID)
				case 2: // Update
					session := &SessionData{
						ID:     fmt.Sprintf("concurrent-session-%d", i%100),
						Port:   9000 + (i % 100),
						UserID: "updated-user",
						Status: "updated",
					}
					storage.Update(session)
				case 3: // LoadAll
					storage.LoadAll()
				}
				i++
			}
		})
	})
}