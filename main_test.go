package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testKey = "demo-secret-key-123456"

func request(q *Queue, method, path, body, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("X-API-Key", key)
	w := httptest.NewRecorder()
	q.handler().ServeHTTP(w, r)
	return w
}
func decodeJob(t *testing.T, w *httptest.ResponseRecorder) Job {
	t.Helper()
	var j Job
	if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	return j
}
func TestJobLifecycle(t *testing.T) {
	q := newQueue(testKey, 4)
	q.start(2)
	defer q.stop()
	w := request(q, "POST", "/jobs", `{"text":"Hello Go 世界"}`, testKey)
	if w.Code != 202 {
		t.Fatalf("submit: %d %s", w.Code, w.Body)
	}
	j := decodeJob(t, w)
	if w.Header().Get("Location") != "/jobs/"+j.ID {
		t.Fatal("missing location")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		w = request(q, "GET", "/jobs/"+j.ID, "", testKey)
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
		j = decodeJob(t, w)
		if j.Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if j.Result == nil || j.Result.Words != 3 || j.Result.Characters != 11 || j.CompletedAt == nil {
		t.Fatalf("bad result: %+v", j)
	}
	if strings.Contains(w.Body.String(), "Hello") {
		t.Fatal("input leaked")
	}
}
func TestValidationAndAuth(t *testing.T) {
	q := newQueue(testKey, 2)
	defer q.stop()
	for _, body := range []string{`{}`, `{"text":"   "}`, `{"text":"ok","extra":true}`, `{"text":"ok"} {}`, `not-json`, fmt.Sprintf(`{"text":%q}`, strings.Repeat("x", 10001))} {
		if w := request(q, "POST", "/jobs", body, testKey); w.Code != 400 {
			t.Fatalf("expected 400 got %d", w.Code)
		}
	}
	if w := request(q, "POST", "/jobs", `{"text":"ok"}`, "wrong"); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(q, "GET", "/health", "", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request(q, "GET", "/jobs/missing", "", testKey); w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func TestQueueFull(t *testing.T) {
	// No workers: filling the buffer is deterministic.
	q := newQueue(testKey, 1)
	defer q.stop()
	if w := request(q, "POST", "/jobs", `{"text":"first"}`, testKey); w.Code != 202 {
		t.Fatal(w.Code)
	}
	w := request(q, "POST", "/jobs", `{"text":"second"}`, testKey)
	if w.Code != 503 || w.Header().Get("Retry-After") != "1" || len(q.jobs) != 1 {
		t.Fatal("queue overflow must reject without storing")
	}
}
func TestShutdownDrainsJobs(t *testing.T) {
	q := newQueue(testKey, 8)
	for i := 0; i < 8; i++ {
		if w := request(q, "POST", "/jobs", `{"text":"go workers"}`, testKey); w.Code != 202 {
			t.Fatal(w.Code)
		}
	}
	q.start(2)
	q.stop()
	q.stop()
	for _, j := range q.jobs {
		if j.Status != "completed" {
			t.Fatal("accepted job not drained")
		}
	}
	if w := request(q, "POST", "/jobs", `{"text":"late"}`, testKey); w.Code != 503 {
		t.Fatal(w.Code)
	}
}
func TestConcurrentSubmissions(t *testing.T) {
	q := newQueue(testKey, 32)
	q.start(2)
	defer q.stop()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := request(q, "POST", "/jobs", `{"text":"concurrent job"}`, testKey); w.Code != 202 {
				t.Errorf("submit: %d", w.Code)
			}
		}()
	}
	wg.Wait()
	q.stop()
	if len(q.jobs) != 20 {
		t.Fatal("lost jobs")
	}
	for _, j := range q.jobs {
		if j.Status != "completed" {
			t.Fatal("unfinished job")
		}
	}
}
