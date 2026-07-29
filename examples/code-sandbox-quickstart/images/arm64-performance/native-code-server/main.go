package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const eventPrefix = "\x1eCUBE_EVENT "

const runnerSource = `
import ast
import json
import pathlib
import sys
import traceback

event_prefix = "\x1eCUBE_EVENT "
source_path = pathlib.Path(sys.argv[1])
source = source_path.read_text()
namespace = {"__name__": "__main__"}

try:
    tree = ast.parse(source, filename="<cube-execute>", mode="exec")
    has_result = bool(tree.body and isinstance(tree.body[-1], ast.Expr))
    if has_result:
        tree.body[-1] = ast.Assign(
            targets=[ast.Name(id="__cube_result", ctx=ast.Store())],
            value=tree.body[-1].value,
        )
        ast.fix_missing_locations(tree)

    exec(compile(tree, "<cube-execute>", "exec"), namespace, namespace)

    if has_result and "__cube_result" in namespace:
        value = namespace["__cube_result"]
        if value is not None:
            print(
                event_prefix
                + json.dumps(
                    {
                        "type": "result",
                        "text": str(value),
                        "is_main_result": True,
                    },
                    ensure_ascii=False,
                ),
                flush=True,
            )
except BaseException as exc:
    traceback.print_exc()
    print(
        event_prefix
        + json.dumps(
            {
                "type": "error",
                "name": exc.__class__.__name__,
                "value": str(exc),
                "traceback": traceback.format_exc(),
            },
            ensure_ascii=False,
        ),
        flush=True,
    )
    raise SystemExit(1)
`

type codeServer struct {
	envdURL    string
	envdClient *http.Client
	envdReady  atomic.Bool
	healthWait time.Duration
}

func event(eventType string, payload map[string]any) []byte {
	if payload == nil {
		payload = make(map[string]any)
	}
	payload["type"] = eventType
	if _, ok := payload["timestamp"]; !ok {
		payload["timestamp"] = strconv.FormatFloat(float64(time.Now().UnixNano())/1e9, 'f', -1, 64)
	}
	data, _ := json.Marshal(payload)
	return append(data, '\n')
}

func splitStdout(stdout string) [][]byte {
	events := make([][]byte, 0)
	pending := make([]string, 0)
	flush := func() {
		if len(pending) == 0 {
			return
		}
		events = append(events, event("stdout", map[string]any{"text": strings.Join(pending, "")}))
		pending = pending[:0]
	}

	for _, rawLine := range strings.Split(stdout, "\n") {
		if rawLine == "" {
			continue
		}
		line := rawLine + "\n"
		if !strings.HasPrefix(line, eventPrefix) {
			pending = append(pending, line)
			continue
		}

		flush()
		payload := make(map[string]any)
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, eventPrefix))), &payload); err != nil {
			pending = append(pending, line)
			continue
		}
		eventType, _ := payload["type"].(string)
		if eventType == "" {
			eventType = "result"
		}
		delete(payload, "type")
		events = append(events, event(eventType, payload))
	}
	flush()
	return events
}

func payloadTimeout(value any) time.Duration {
	seconds := int64(60)
	switch typed := value.(type) {
	case float64:
		seconds = int64(typed)
	case string:
		if parsed, err := strconv.ParseInt(typed, 10, 64); err == nil {
			seconds = parsed
		}
	}
	if seconds < 1 {
		seconds = 1
	}
	return time.Duration(seconds) * time.Second
}

func executeCode(payload map[string]any) [][]byte {
	code, ok := payload["code"].(string)
	if !ok {
		return [][]byte{event("error", map[string]any{
			"name": "BadRequest", "value": "field 'code' must be a string",
		})}
	}

	workdir := os.Getenv("CODE_INTERPRETER_WORKDIR")
	if workdir == "" {
		workdir = "/workspace"
	}
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return [][]byte{event("error", map[string]any{"name": "WorkdirError", "value": err.Error()})}
	}

	tmpdir, err := os.MkdirTemp("", "cube-execute-")
	if err != nil {
		return [][]byte{event("error", map[string]any{"name": "TempDirError", "value": err.Error()})}
	}
	defer os.RemoveAll(tmpdir)

	sourcePath := filepath.Join(tmpdir, "user_code.py")
	runnerPath := filepath.Join(tmpdir, "runner.py")
	if err := os.WriteFile(sourcePath, []byte(code), 0o600); err != nil {
		return [][]byte{event("error", map[string]any{"name": "WriteError", "value": err.Error()})}
	}
	if err := os.WriteFile(runnerPath, []byte(runnerSource), 0o600); err != nil {
		return [][]byte{event("error", map[string]any{"name": "WriteError", "value": err.Error()})}
	}

	python := os.Getenv("PYTHON_EXECUTABLE")
	if python == "" {
		python = "/usr/bin/python3"
	}
	ctx, cancel := context.WithTimeout(context.Background(), payloadTimeout(payload["timeout"]))
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-u", runnerPath, sourcePath)
	cmd.Dir = workdir
	cmd.Env = os.Environ()
	if envVars, ok := payload["env_vars"].(map[string]any); ok {
		for key, value := range envVars {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%v", key, value))
		}
	} else if envVars, ok := payload["envVars"].(map[string]any); ok {
		for key, value := range envVars {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%v", key, value))
		}
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return [][]byte{event("error", map[string]any{
			"name": "TimeoutExpired", "value": fmt.Sprintf("execution exceeded %.0fs", payloadTimeout(payload["timeout"]).Seconds()), "traceback": "",
		})}
	}

	events := splitStdout(stdout.String())
	if stderr.Len() > 0 {
		events = append(events, event("stderr", map[string]any{"text": stderr.String()}))
	}
	if runErr != nil {
		hasError := false
		for _, item := range events {
			if bytes.Contains(item, []byte(`"type":"error"`)) {
				hasError = true
				break
			}
		}
		if !hasError {
			exitCode := 1
			var exitErr *exec.ExitError
			if errors.As(runErr, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
			events = append(events, event("error", map[string]any{
				"name": "ProcessError", "value": fmt.Sprintf("exit code %d", exitCode), "traceback": stderr.String(),
			}))
		}
	}
	return events
}

func (s *codeServer) envdHealthy() bool {
	if s.envdReady.Load() {
		return true
	}
	response, err := s.envdClient.Get(s.envdURL)
	if err != nil {
		return false
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false
	}
	s.envdReady.Store(true)
	return true
}

func (s *codeServer) waitForEnvd(ctx context.Context) bool {
	if s.envdHealthy() {
		return true
	}
	if s.healthWait <= 0 {
		return false
	}

	deadline := time.NewTimer(s.healthWait)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
			if s.envdHealthy() {
				return true
			}
		}
	}
}

func (s *codeServer) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet && (request.URL.Path == "/" || request.URL.Path == "/health") {
		if !s.waitForEnvd(request.Context()) {
			http.Error(response, "envd not ready", http.StatusServiceUnavailable)
			return
		}
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		response.Write([]byte("ok\n"))
		return
	}
	if request.Method != http.MethodPost || request.URL.Path != "/execute" {
		http.NotFound(response, request)
		return
	}

	defer request.Body.Close()
	payload := make(map[string]any)
	if err := json.NewDecoder(io.LimitReader(request.Body, 16<<20)).Decode(&payload); err != nil {
		response.Header().Set("Content-Type", "application/x-ndjson")
		response.WriteHeader(http.StatusBadRequest)
		response.Write(event("error", map[string]any{"name": "BadRequest", "value": err.Error()}))
		return
	}

	response.Header().Set("Content-Type", "application/x-ndjson")
	response.WriteHeader(http.StatusOK)
	for _, item := range executeCode(payload) {
		response.Write(item)
	}
}

func main() {
	host := os.Getenv("CODE_INTERPRETER_HOST")
	if host == "" {
		host = "0.0.0.0"
	}
	port := os.Getenv("CODE_INTERPRETER_PORT")
	if port == "" {
		port = "49999"
	}
	envdPort := os.Getenv("ENVD_PORT")
	if envdPort == "" {
		envdPort = "49983"
	}

	handler := &codeServer{
		envdURL:    "http://127.0.0.1:" + envdPort + "/health",
		envdClient: &http.Client{Timeout: 50 * time.Millisecond},
	}
	if os.Getenv("CUBE_WAIT_ENVD_BEFORE_LISTEN") != "0" {
		handler.healthWait = 10 * time.Second
		if !handler.waitForEnvd(context.Background()) {
			log.Fatal("envd did not become ready before native code server startup")
		}
		handler.healthWait = 0
	}
	server := &http.Server{
		Addr:              host + ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("native code server listening on %s (envd ready at %s)", server.Addr, handler.envdURL)
	log.Fatal(server.ListenAndServe())
}
