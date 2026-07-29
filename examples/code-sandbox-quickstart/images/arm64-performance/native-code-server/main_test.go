package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSplitStdout(t *testing.T) {
	events := splitStdout("hello\n" + eventPrefix + `{"type":"result","text":"2"}` + "\n")
	if len(events) != 2 || !bytes.Contains(events[0], []byte(`"type":"stdout"`)) || !bytes.Contains(events[1], []byte(`"type":"result"`)) {
		t.Fatalf("unexpected events: %q", events)
	}
}

func TestHealthRequiresEnvd(t *testing.T) {
	status := http.StatusServiceUnavailable
	envd := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(status)
	}))
	defer envd.Close()

	handler := &codeServer{envdURL: envd.URL, envdClient: &http.Client{Timeout: time.Second}}
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d before envd became ready", response.Code)
	}

	status = http.StatusNoContent
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("got %d after envd became ready", response.Code)
	}
}

func TestHealthWaitsForEnvd(t *testing.T) {
	var ready atomic.Bool
	envd := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	defer envd.Close()

	handler := &codeServer{
		envdURL: envd.URL, envdClient: &http.Client{Timeout: time.Second}, healthWait: 100 * time.Millisecond,
	}
	time.AfterFunc(10*time.Millisecond, func() { ready.Store(true) })
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("got %d after waiting for envd", response.Code)
	}
}

func TestExecuteCode(t *testing.T) {
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	t.Setenv("CODE_INTERPRETER_WORKDIR", t.TempDir())
	events := executeCode(map[string]any{"code": "print('hello')\n1 + 1", "timeout": float64(5)})
	joined := bytes.Join(events, nil)
	if !bytes.Contains(joined, []byte(`"type":"stdout"`)) || !bytes.Contains(joined, []byte(`"type":"result"`)) || !bytes.Contains(joined, []byte(`"text":"2"`)) {
		t.Fatalf("unexpected execute response: %s", joined)
	}
	for _, item := range events {
		var payload map[string]any
		if err := json.Unmarshal(item, &payload); err != nil {
			t.Fatalf("invalid JSON event %q: %v", item, err)
		}
	}
}

func TestBadExecuteRequest(t *testing.T) {
	handler := &codeServer{}
	request := httptest.NewRequest(http.MethodPost, "/execute", strings.NewReader("{"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("got status %d", response.Code)
	}
}
