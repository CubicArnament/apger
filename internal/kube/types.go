package kube

import (
	"context"
	"time"
)

type Build struct {
	ID        string     `json:"id"`
	Package   string     `json:"package"`
	State     string     `json:"state"`
	CreatedAt time.Time  `json:"createdAt"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	Message   string     `json:"message,omitempty"`
}

type SubmitOptions struct {
	Package  string
	PKGBUILD string
}

type Builds interface {
	Submit(context.Context, SubmitOptions) (Build, error)
	Get(context.Context, string) (Build, error)
	List(context.Context) ([]Build, error)
	Logs(context.Context, string) (string, error)
	Delete(context.Context, string) error
}
