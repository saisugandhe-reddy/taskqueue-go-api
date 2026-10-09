package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

type Result struct {
	Words      int `json:"words"`
	Characters int `json:"characters"`
}
type Job struct {
	ID          string     `json:"id"`
	Text        string     `json:"-"` // Never expose input in status responses.
	Status      string     `json:"status"`
	Result      *Result    `json:"result,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}
type Queue struct {
	mu      sync.RWMutex
	jobs    map[string]Job
	pending chan string
	wg      sync.WaitGroup
	stopped bool
	key     string
}

func newQueue(key string, capacity int) *Queue {
	return &Queue{jobs: make(map[string]Job), pending: make(chan string, capacity), key: key}
}
func (q *Queue) start(workers int) {
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go func() {
			defer q.wg.Done()
			for id := range q.pending {
				q.mu.Lock()
				j := q.jobs[id]
				j.Status = "processing"
				q.jobs[id] = j
				q.mu.Unlock()
				result := Result{len(strings.Fields(j.Text)), utf8.RuneCountInString(j.Text)}
				now := time.Now().UTC()
				q.mu.Lock()
				j.Status = "completed"
				j.Text = ""
				j.Result = &result
				j.CompletedAt = &now
				q.jobs[id] = j
				q.mu.Unlock()
				slog.Info("job completed", "job_id", id)
			}
		}()
	}
}

// Closing the channel lets workers finish all accepted jobs before exiting.
func (q *Queue) stop() {
	q.mu.Lock()
	if !q.stopped {
		q.stopped = true
		close(q.pending)
	}
	q.mu.Unlock()
	q.wg.Wait()
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
func (q *Queue) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /jobs", q.submit)
	mux.HandleFunc("GET /jobs/{id}", q.get)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-API-Key")), []byte(q.key)) != 1 {
			problem(w, 401, "invalid API key")
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func (q *Queue) submit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()
	if err := d.Decode(&body); err != nil {
		problem(w, 400, "invalid JSON or body exceeds 16 KiB")
		return
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		problem(w, 400, "one JSON object required")
		return
	}
	if strings.TrimSpace(body.Text) == "" || len(body.Text) > 10000 {
		problem(w, 400, "text must contain 1 to 10000 bytes of nonblank input")
		return
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		problem(w, 500, "ID generation failed")
		return
	}
	job := Job{ID: hex.EncodeToString(b), Text: body.Text, Status: "queued", CreatedAt: time.Now().UTC()}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped {
		problem(w, 503, "queue shutting down")
		return
	}
	if len(q.jobs) >= 1000 {
		problem(w, 503, "demo job limit reached; restart to clear history")
		return
	}
	q.jobs[job.ID] = job
	select {
	case q.pending <- job.ID:
		w.Header().Set("Location", "/jobs/"+job.ID)
		respond(w, 202, job)
	default:
		delete(q.jobs, job.ID)
		w.Header().Set("Retry-After", "1")
		problem(w, 503, "queue full; retry later")
	}
}
func (q *Queue) get(w http.ResponseWriter, r *http.Request) {
	q.mu.RLock()
	job, ok := q.jobs[r.PathValue("id")]
	q.mu.RUnlock()
	if !ok {
		problem(w, 404, "job not found")
		return
	}
	respond(w, 200, job)
}
func main() {
	key := os.Getenv("API_KEY")
	if len(key) < 16 {
		slog.Error("set API_KEY to at least 16 characters")
		os.Exit(1)
	}
	q := newQueue(key, 32)
	q.start(2)
	srv := &http.Server{Addr: ":8081", Handler: q.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	serverErr := make(chan error, 1)
	go func() { slog.Info("TaskQueue listening", "address", srv.Addr); serverErr <- srv.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown", "error", err)
			_ = srv.Close()
		}
		done()
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server", "error", err)
			q.stop()
			os.Exit(1)
		}
	}
	q.stop()
	slog.Info("accepted jobs drained; stopped")
}
