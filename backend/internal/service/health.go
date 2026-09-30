package service

import "context"

// Pinger is satisfied by repository.DB.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthStatus is the result of a health check.
type HealthStatus struct {
	OK       bool
	Database string
}

// HealthService reports whether the app's dependencies are usable.
type HealthService struct {
	db Pinger
}

// NewHealthService returns a HealthService checking db.
func NewHealthService(db Pinger) *HealthService { return &HealthService{db: db} }

// Check pings the database.
func (s *HealthService) Check(ctx context.Context) HealthStatus {
	if err := s.db.Ping(ctx); err != nil {
		return HealthStatus{OK: false, Database: "unavailable"}
	}
	return HealthStatus{OK: true, Database: "ok"}
}
