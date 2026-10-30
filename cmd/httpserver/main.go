package main

import (
	"encoding/json"
	"fmt"
	"httpServer/internal/http1"
	"httpServer/internal/router"
	"httpServer/internal/telemetry"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"

	"github.com/VictoriaMetrics/metrics"
)

const addr = ":8080"

func main() {
	r := router.NewCustomMux()
	userRepo := NewUserRepo()


	r.Handle(http.MethodGet, "/users", userRepo)
	r.Handle(http.MethodPost, "/users", userRepo)


	r.HandleFunc(http.MethodGet, "/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})


	r.HandleFunc(http.MethodGet, "/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		metrics.WriteProcessMetrics(w)
		metrics.WritePrometheus(w, true)
	})



	handlerWithTelemetry := telemetry.Middleware(r)

	fmt.Printf("HTTP server listening on %s...\n", addr)
	if err := http1.ListenAndServe(addr, handlerWithTelemetry); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

type User struct {
	ID   uint   `json:"id"`
	Age  uint   `json:"age"`
	Name string `json:"name"`
}

type UserRepo struct {
	mu    sync.RWMutex
	users []*User
	gauge *metrics.Gauge
}

func NewUserRepo() *UserRepo {
	repo := &UserRepo{
		users: make([]*User, 0),
		gauge: metrics.NewGauge("app_users_total", nil),
	}

	repo.addUser(1, 24, "Alice")
	repo.addUser(2, 30, "Bob")
	return repo
}

func (u *UserRepo) addUser(id uint, age uint, name string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()

	for _, user := range u.users {
		if user.ID == id {
			return false
		}
	}
	u.users = append(u.users, &User{ID: id, Age: age, Name: name})
	u.gauge.Set(float64(len(u.users)))
	return true
}

func (u *UserRepo) getUsers(queryID string) []*User {
	u.mu.RLock()
	defer u.mu.RUnlock()

	if queryID == "" {
		result := make([]*User, len(u.users))
		copy(result, u.users)
		return result
	}

	id, err := strconv.ParseUint(queryID, 10, 64)
	if err != nil {
		return []*User{}
	}

	for _, user := range u.users {
		if user.ID == uint(id) {
			return []*User{user}
		}
	}
	return []*User{}
}

func (u *UserRepo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		idParam := r.URL.Query().Get("id")


		_, fetchSpan := telemetry.StartSpan(r.Context(), "db.fetch_users")
		fetchSpan.SetAttribute("db.operation", "SELECT")
		fetchSpan.SetAttribute("query.filter_id", idParam)


		users := u.getUsers(idParam)
		fetchSpan.SetAttribute("results.count", strconv.Itoa(len(users)))
		fetchSpan.End()


		telemetry.Logger.Info("users_queried",
			"trace_id", r.Context().Value(telemetry.TraceIDKey),
			"filter_id", idParam,
			"results_count", len(users),
		)

		_ = json.NewEncoder(w).Encode(users)

	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "empty or invalid request body"})
			return
		}

		ctx, parseSpan := telemetry.StartSpan(r.Context(), "app.parse_payload")
		var payload struct {
			Age  uint   `json:"age"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			parseSpan.SetError()
			parseSpan.End()
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid json"})
			return
		}
		parseSpan.End()


		_, insertSpan := telemetry.StartSpan(ctx, "db.insert_user")
		insertSpan.SetAttribute("db.operation", "INSERT")
		insertSpan.SetAttribute("user.name", payload.Name)
		insertSpan.SetAttribute("user.age", strconv.FormatUint(uint64(payload.Age), 10))


		newID := uint(rand.UintN(10000) + 10)
		u.addUser(newID, payload.Age, payload.Name)
		insertSpan.SetAttribute("user.id", strconv.FormatUint(uint64(newID), 10))
		insertSpan.End()


		telemetry.Logger.Info("user_registered",
			"trace_id", r.Context().Value(telemetry.TraceIDKey),
			"user_id", newID,
			"name", payload.Name,
			"age", payload.Age,
		)

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "created", "id": newID})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
