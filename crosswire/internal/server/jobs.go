package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// jobGrace is how long a finished async job stays replayable in memory.
const jobGrace = 2 * time.Minute

type sseEvent struct {
	name string
	data []byte
}

// job is an in-flight async run or intruder campaign. Late subscribers
// replay everything emitted so far.
type job struct {
	mu     sync.Mutex
	events []sseEvent
	wake   chan struct{}
	done   bool
}

type jobs struct {
	mu sync.Mutex
	m  map[string]*job
}

func newJobs() *jobs { return &jobs{m: map[string]*job{}} }

func (js *jobs) start(id string) *job {
	j := &job{wake: make(chan struct{})}
	js.mu.Lock()
	js.m[id] = j
	js.mu.Unlock()
	return j
}

func (js *jobs) get(id string) *job {
	js.mu.Lock()
	defer js.mu.Unlock()
	return js.m[id]
}

func (js *jobs) forget(id string) {
	time.AfterFunc(jobGrace, func() {
		js.mu.Lock()
		delete(js.m, id)
		js.mu.Unlock()
	})
}

func (j *job) emit(name string, v any, final bool) {
	data, _ := json.Marshal(v)
	j.mu.Lock()
	j.events = append(j.events, sseEvent{name, data})
	j.done = final
	close(j.wake)
	j.wake = make(chan struct{})
	j.mu.Unlock()
}

// stream writes the job's events as SSE until the final event is sent or
// the client goes away.
func (j *job) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, fmt.Errorf("streaming unsupported"))
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	sent := 0
	for {
		j.mu.Lock()
		pending, done, wake := j.events[sent:], j.done, j.wake
		j.mu.Unlock()
		for _, e := range pending {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.name, e.data)
		}
		sent += len(pending)
		fl.Flush()
		if done {
			return
		}
		select {
		case <-wake:
		case <-r.Context().Done():
			return
		case <-time.After(15 * time.Second):
			fmt.Fprint(w, ": keepalive\n\n")
			fl.Flush()
		}
	}
}

// writeOneShot sends a single final event, for jobs that already left memory.
func writeOneShot(w http.ResponseWriter, name string, v any) {
	data, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data)
}
