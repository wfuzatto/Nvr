package media

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type RetentionReport struct {
	ScannedFiles int   `json:"scanned_files"`
	DeletedFiles int   `json:"deleted_files"`
	FreedBytes   int64 `json:"freed_bytes"`
	RemainingBytes int64 `json:"remaining_bytes"`
	PartialFilesRemoved int `json:"partial_files_removed"`
}

type retentionFile struct {
	path string
	size int64
	mod  time.Time
	protected bool
}

func RunRetention(root string, retentionDays int, maxBytes int64) (RetentionReport, error) {
	var report RetentionReport
	var files []retentionFile
	now := time.Now()
	cutoff := time.Time{}
	if retentionDays > 0 { cutoff = now.Add(-time.Duration(retentionDays) * 24 * time.Hour) }

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) { return nil }
			return walkErr
		}
		if entry.IsDir() { return nil }

		if strings.HasSuffix(entry.Name(), ".partial") {
			info, err := entry.Info()
			if err == nil && now.Sub(info.ModTime()) > 10*time.Minute {
				if os.Remove(path) == nil { report.PartialFilesRemoved++ }
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".h264") && !strings.HasSuffix(entry.Name(), ".h265") { return nil }

		info, err := entry.Info()
		if err != nil { return nil }
		_, protectErr := os.Stat(path + ".protected")
		files = append(files, retentionFile{
			path: path, size: info.Size(), mod: info.ModTime(),
			protected: protectErr == nil,
		})
		report.ScannedFiles++
		return nil
	})
	if err != nil && !os.IsNotExist(err) { return report, err }

	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })

	var remaining int64
	for _, file := range files { remaining += file.size }

	deleted := make(map[string]bool)
	if !cutoff.IsZero() {
		for _, file := range files {
			if file.protected || !file.mod.Before(cutoff) { continue }
			if err := os.Remove(file.path); err == nil {
				deleted[file.path] = true
				report.DeletedFiles++
				report.FreedBytes += file.size
				remaining -= file.size
			}
		}
	}

	if maxBytes > 0 && remaining > maxBytes {
		for _, file := range files {
			if remaining <= maxBytes { break }
			if file.protected || deleted[file.path] { continue }
			if err := os.Remove(file.path); err == nil {
				deleted[file.path] = true
				report.DeletedFiles++
				report.FreedBytes += file.size
				remaining -= file.size
			}
		}
	}

	report.RemainingBytes = remaining
	removeEmptyDirs(root)
	return report, nil
}

func removeEmptyDirs(root string) {
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() && path != root { dirs = append(dirs, path) }
		return nil
	})
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, dir := range dirs { _ = os.Remove(dir) }
}
