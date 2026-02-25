package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type execRequest struct {
	Args           []string `json:"args"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	MergeStderr    bool     `json:"mergeStderr"`
}

type execResponse struct {
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	TimedOut bool   `json:"timedOut"`
}

func main() {
	token := strings.TrimSpace(os.Getenv("RUNNER_AGENT_TOKEN"))
	if token == "" {
		log.Fatal("RUNNER_AGENT_TOKEN is required")
	}

	addr := strings.TrimSpace(os.Getenv("RUNNER_AGENT_ADDR"))
	if addr == "" {
		addr = ":9000"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/exec", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorized(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var req execRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		if err := validateArgs(req.Args); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		timeout := req.TimeoutSeconds
		if timeout <= 0 {
			timeout = 30
		}
		if timeout > 1800 {
			timeout = 1800
		}

		resp := runCommand(req.Args, timeout, req.MergeStderr)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           withAccessLog(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("Runner agent listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func authorized(r *http.Request, token string) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return false
	}
	return auth == "Bearer "+token
}

func validateArgs(args []string) error {
	if len(args) == 0 {
		return errors.New("args is required")
	}
	if args[0] != "docker" {
		return errors.New("only docker commands are allowed")
	}
	return nil
}

func runCommand(args []string, timeoutSeconds int, mergeStderr bool) execResponse {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	if mergeStderr {
		cmd.Stderr = &stdout
	} else {
		cmd.Stderr = &stderr
	}

	err := cmd.Run()
	timedOut := ctx.Err() == context.DeadlineExceeded
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else if timedOut {
			exitCode = 124
		} else {
			exitCode = 1
			if stderr.Len() == 0 {
				stderr.WriteString(err.Error())
			}
		}
	}

	return execResponse{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		TimedOut: timedOut,
	}
}

func withAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start))
	})
}

