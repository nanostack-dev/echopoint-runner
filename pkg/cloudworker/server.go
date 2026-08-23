package cloudworker

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/google/uuid"
)

const (
	assignRoute = "POST /assign"
	statusRoute = "GET /status"
	healthRoute = "GET /health"
)

// JobLauncher runs one assigned Cloud job and returns when that job is over.
type JobLauncher func(ctx context.Context, jobID uuid.UUID, jobToken string) error

// StatusResponse tells the Cloudflare Worker how much of this container is in
// use. Idle sleep reads Busy, and assign routing reads Running against Capacity.
type StatusResponse struct {
	Busy     bool     `json:"busy"`
	Running  int      `json:"running"`
	Capacity int      `json:"capacity"`
	JobIDs   []string `json:"job_ids"`
}

type assignRequest struct {
	JobID    string `json:"job_id"`
	JobToken string `json:"job_token"`
}

// Server runs up to Capacity Cloud jobs at once, each in its own runner
// process, and reports how many it holds. Idle sleep asks GET /status, so a
// held assign request is not what keeps the container alive.
type Server struct {
	launch   JobLauncher
	capacity int

	mu       sync.Mutex
	running  map[uuid.UUID]struct{}
	draining bool
	idle     chan struct{}
}

// NewServer builds a container server that runs at most capacity jobs at once.
// A capacity below one is raised to one.
func NewServer(launch JobLauncher, capacity int) *Server {
	return &Server{
		launch:   launch,
		capacity: max(capacity, 1),
		running:  make(map[uuid.UUID]struct{}),
		idle:     make(chan struct{}, 1),
	}
}

// StopAccepting refuses every later assign. The jobs already running keep
// running: the platform sends SIGTERM before it replaces a container, and a
// killed Cloud job is a customer run that never runs again.
func (s *Server) StopAccepting() {
	s.mu.Lock()
	s.draining = true
	empty := len(s.running) == 0
	s.mu.Unlock()
	if empty {
		s.signalIdle()
	}
}

// Running reports how many jobs this container holds.
func (s *Server) Running() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.running)
}

// WaitIdle blocks until no job is running, or until ctx is done.
func (s *Server) WaitIdle(ctx context.Context) error {
	for {
		if s.Running() == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.idle:
		}
	}
}

func (s *Server) signalIdle() {
	select {
	case s.idle <- struct{}{}:
	default:
	}
}

// Handler serves assign, status, and health.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(healthRoute, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc(statusRoute, s.handleStatus)
	mux.HandleFunc(assignRoute, s.handleAssign)
	return mux
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(s.status())
}

func (s *Server) status() StatusResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobIDs := make([]string, 0, len(s.running))
	for jobID := range s.running {
		jobIDs = append(jobIDs, jobID.String())
	}
	return StatusResponse{
		Busy:     len(s.running) > 0,
		Running:  len(s.running),
		Capacity: s.capacity,
		JobIDs:   jobIDs,
	}
}

func (s *Server) handleAssign(w http.ResponseWriter, r *http.Request) {
	var body assignRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid assign body", http.StatusBadRequest)
		return
	}
	if body.JobToken == "" {
		http.Error(w, "job_id and job_token are required", http.StatusBadRequest)
		return
	}
	jobID, err := uuid.Parse(body.JobID)
	if err != nil {
		http.Error(w, "job_id is not a uuid", http.StatusBadRequest)
		return
	}
	if !s.take(jobID) {
		http.Error(w, "container cannot accept another job", http.StatusTooManyRequests)
		return
	}

	go s.run(jobID, body.JobToken)
	w.WriteHeader(http.StatusAccepted)
}

// take reserves a slot for jobID. A job already running keeps its slot rather
// than taking a second one, so a repeated assign is not counted twice.
func (s *Server) take(jobID uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draining {
		return false
	}
	if _, held := s.running[jobID]; held {
		return false
	}
	if len(s.running) >= s.capacity {
		return false
	}
	s.running[jobID] = struct{}{}
	return true
}

func (s *Server) release(jobID uuid.UUID) {
	s.mu.Lock()
	delete(s.running, jobID)
	empty := len(s.running) == 0
	s.mu.Unlock()
	if empty {
		s.signalIdle()
	}
}

func (s *Server) run(jobID uuid.UUID, jobToken string) {
	defer s.release(jobID)
	_ = s.launch(context.Background(), jobID, jobToken)
}
