package tasks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	taskTimingFile       = ".mono"
	taskTimingLockFile   = ".mono.lock"
	taskTimingVersion    = 1
	taskTimingWindowSize = 10
)

var taskTimingProcessLock sync.Mutex

type taskTimingKey struct {
	Service     string `json:"service"`
	Task        string `json:"task"`
	Integration bool   `json:"integration"`
}

type taskTimingRecord struct {
	Service     string  `json:"service"`
	Task        string  `json:"task"`
	Integration bool    `json:"integration"`
	DurationsMs []int64 `json:"durationsMs"`
}

type taskTimingFileData struct {
	Version int                `json:"version"`
	Timings []taskTimingRecord `json:"timings"`
}

type taskTimingStore struct {
	root string
	ops  taskTimingFileOps
}

type taskTimingTempFile interface {
	io.Writer
	Name() string
	Sync() error
	Close() error
}

type taskTimingFileOps interface {
	ReadFile(string) ([]byte, error)
	CreateTemp(string, string) (taskTimingTempFile, error)
	Rename(string, string) error
	Remove(string) error
}

type osTaskTimingFileOps struct{}

func (osTaskTimingFileOps) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }
func (osTaskTimingFileOps) CreateTemp(dir, pattern string) (taskTimingTempFile, error) {
	return os.CreateTemp(dir, pattern)
}
func (osTaskTimingFileOps) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }
func (osTaskTimingFileOps) Remove(path string) error             { return os.Remove(path) }

var errTaskTimingLockUnsupported = errors.New("advisory timing-history locks are unsupported on this platform")

func newTaskTimingStore(root string) taskTimingStore {
	absolute, err := filepath.Abs(root)
	if err == nil {
		root = absolute
	}
	return taskTimingStore{root: root, ops: osTaskTimingFileOps{}}
}

func (s taskTimingStore) path() string { return filepath.Join(s.root, taskTimingFile) }

func (s taskTimingStore) load() (map[taskTimingKey][]time.Duration, error) {
	data, err := s.ops.ReadFile(s.path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[taskTimingKey][]time.Duration{}, nil
		}
		return map[taskTimingKey][]time.Duration{}, fmt.Errorf("read timing history: %w", err)
	}
	file, err := decodeTaskTimingFile(data)
	if err != nil {
		return map[taskTimingKey][]time.Duration{}, err
	}
	return taskTimingMap(file), nil
}

func (s taskTimingStore) append(observations map[taskTimingKey][]time.Duration) error {
	if len(observations) == 0 {
		return nil
	}
	deadline := time.Now().Add(time.Second)
	for !taskTimingProcessLock.TryLock() {
		if time.Now().After(deadline) {
			return fmt.Errorf("lock timing history: timed out after 1s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer taskTimingProcessLock.Unlock()

	lock, err := acquireTaskTimingLock(s.root, time.Until(deadline))
	if err != nil {
		return fmt.Errorf("lock timing history: %w", err)
	}
	defer lock()

	current := map[taskTimingKey][]time.Duration{}
	data, err := s.ops.ReadFile(s.path())
	if err == nil {
		file, decodeErr := decodeTaskTimingFile(data)
		if decodeErr != nil {
			var versionErr unknownTaskTimingVersionError
			if errors.As(decodeErr, &versionErr) {
				return decodeErr
			}
			// A corrupt or invalid version-1 file is advisory. Replace it with
			// only the observations from this successful update.
		} else {
			current = taskTimingMap(file)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read timing history before update: %w", err)
	}
	for key, values := range observations {
		for _, value := range values {
			if value < 0 {
				return fmt.Errorf("invalid negative task duration for %s/%s", key.Service, key.Task)
			}
			current[key] = append(current[key], value)
			if len(current[key]) > taskTimingWindowSize {
				current[key] = append([]time.Duration(nil), current[key][len(current[key])-taskTimingWindowSize:]...)
			}
		}
	}
	return s.writeLocked(current)
}

func (s taskTimingStore) writeLocked(timings map[taskTimingKey][]time.Duration) error {
	file := taskTimingFileData{Version: taskTimingVersion}
	keys := make([]taskTimingKey, 0, len(timings))
	for key := range timings {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Service != keys[j].Service {
			return keys[i].Service < keys[j].Service
		}
		if keys[i].Task != keys[j].Task {
			return keys[i].Task < keys[j].Task
		}
		return !keys[i].Integration && keys[j].Integration
	})
	for _, key := range keys {
		values := timings[key]
		record := taskTimingRecord{Service: key.Service, Task: key.Task, Integration: key.Integration}
		for _, value := range values {
			record.DurationsMs = append(record.DurationsMs, value.Milliseconds())
		}
		file.Timings = append(file.Timings, record)
	}
	encoded, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode timing history: %w", err)
	}
	encoded = append(encoded, '\n')
	tmp, err := s.ops.CreateTemp(s.root, taskTimingFile+".tmp-*")
	if err != nil {
		return fmt.Errorf("create timing history temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer s.ops.Remove(tmpPath)
	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write timing history temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync timing history temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close timing history temporary file: %w", err)
	}
	if err := s.ops.Rename(tmpPath, s.path()); err != nil {
		return fmt.Errorf("replace timing history: %w", err)
	}
	return nil
}

type unknownTaskTimingVersionError struct{ version int }

func (e unknownTaskTimingVersionError) Error() string {
	return fmt.Sprintf("unsupported timing history version %d", e.version)
}

func decodeTaskTimingFile(data []byte) (taskTimingFileData, error) {
	// Check the version before decoding its schema: future versions can contain
	// new fields and must still be preserved by the update path.
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err == nil && header.Version != taskTimingVersion {
		return taskTimingFileData{}, unknownTaskTimingVersionError{version: header.Version}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var file taskTimingFileData
	if err := decoder.Decode(&file); err != nil {
		return taskTimingFileData{}, fmt.Errorf("invalid timing history: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return taskTimingFileData{}, fmt.Errorf("invalid timing history: trailing JSON data")
	}
	if file.Version != taskTimingVersion {
		return taskTimingFileData{}, unknownTaskTimingVersionError{version: file.Version}
	}
	seen := make(map[taskTimingKey]struct{}, len(file.Timings))
	for _, record := range file.Timings {
		if strings.TrimSpace(record.Service) == "" || strings.TrimSpace(record.Task) == "" || record.DurationsMs == nil || len(record.DurationsMs) > taskTimingWindowSize {
			return taskTimingFileData{}, fmt.Errorf("invalid timing history: malformed task record")
		}
		key := taskTimingKey{Service: record.Service, Task: record.Task, Integration: record.Integration}
		if _, ok := seen[key]; ok {
			return taskTimingFileData{}, fmt.Errorf("invalid timing history: duplicate task record")
		}
		seen[key] = struct{}{}
		for _, value := range record.DurationsMs {
			if value < 0 || value > math.MaxInt64/int64(time.Millisecond) {
				return taskTimingFileData{}, fmt.Errorf("invalid timing history: duration out of range")
			}
		}
	}
	return file, nil
}

func taskTimingMap(file taskTimingFileData) map[taskTimingKey][]time.Duration {
	result := make(map[taskTimingKey][]time.Duration, len(file.Timings))
	for _, record := range file.Timings {
		key := taskTimingKey{Service: record.Service, Task: record.Task, Integration: record.Integration}
		for _, millis := range record.DurationsMs {
			result[key] = append(result[key], time.Duration(millis)*time.Millisecond)
		}
	}
	return result
}

func meanTaskDuration(samples []time.Duration) (time.Duration, bool) {
	if len(samples) == 0 {
		return 0, false
	}
	var total float64
	for _, sample := range samples {
		if sample < 0 {
			return 0, false
		}
		total += float64(sample)
	}
	mean := total / float64(len(samples))
	if mean >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64), true
	}
	return time.Duration(mean), true
}
