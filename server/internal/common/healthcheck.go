package common

type ComponentStatus struct {
	Name    string         `json:"name"`
	Healthy bool           `json:"healthy"`
	Error   string         `json:"error,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

type HealthCheckable interface {
	HealthCheck() ComponentStatus
}
