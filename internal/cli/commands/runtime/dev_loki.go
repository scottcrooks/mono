package runtimecmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// lokiSink serializes pushes and timestamps across the service streams.
type lokiSink struct {
	endpoint      string
	client        *http.Client
	mu            sync.Mutex
	lastTimestamp int64
}

func newLokiSink(endpoint string) *lokiSink {
	if endpoint == "" {
		return nil
	}
	return &lokiSink{endpoint: strings.TrimRight(endpoint, "/") + "/loki/api/v1/push", client: &http.Client{Timeout: 2 * time.Second}}
}

func (s *lokiSink) push(job, line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	timestamp := time.Now().UnixNano()
	if timestamp <= s.lastTimestamp {
		timestamp = s.lastTimestamp + 1
	}
	s.lastTimestamp = timestamp
	payload := struct {
		Streams []struct {
			Stream map[string]string `json:"stream"`
			Values [][2]string       `json:"values"`
		} `json:"streams"`
	}{}
	payload.Streams = append(payload.Streams, struct {
		Stream map[string]string `json:"stream"`
		Values [][2]string       `json:"values"`
	}{Stream: map[string]string{"job": job}, Values: [][2]string{{strconv.FormatInt(timestamp, 10), line}}})
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

// Each writer belongs to one service and one output stream; fragments are not
// combined with another stream and are flushed only after the child is reaped.
type lokiLineWriter struct {
	sink    *lokiSink
	job     string
	mu      sync.Mutex
	pending []byte
}

func (w *lokiLineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, p...)
	for {
		i := bytes.IndexByte(w.pending, '\n')
		if i < 0 {
			break
		}
		w.sink.push(w.job, string(w.pending[:i]))
		w.pending = w.pending[i+1:]
	}
	return len(p), nil
}

func (w *lokiLineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) > 0 {
		w.sink.push(w.job, string(w.pending))
		w.pending = nil
	}
}
