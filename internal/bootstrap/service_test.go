package bootstrap

import (
	"context"
	"testing"
	"time"
)

func TestRunValidatesRequiredFields(t *testing.T) {
	service := Service{}
	tests := []struct {
		name string
		req  Request
		want string
	}{
		{name: "host", req: Request{User: "root", Port: 22, Identity: "/tmp/id", TTL: time.Hour, Timeout: time.Second, HostKeyPolicy: "accept-new", StateDir: t.TempDir()}, want: "invalid_args"},
		{name: "port low", req: Request{Host: "example.com", User: "root", Port: 0, Identity: "/tmp/id", TTL: time.Hour, Timeout: time.Second, HostKeyPolicy: "accept-new", StateDir: t.TempDir()}, want: "invalid_args"},
		{name: "ttl", req: Request{Host: "example.com", User: "root", Port: 22, Identity: "/tmp/id", TTL: 0, Timeout: time.Second, HostKeyPolicy: "accept-new", StateDir: t.TempDir()}, want: "invalid_args"},
		{name: "policy", req: Request{Host: "example.com", User: "root", Port: 22, Identity: "/tmp/id", TTL: time.Hour, Timeout: time.Second, HostKeyPolicy: "bad", StateDir: t.TempDir()}, want: "invalid_args"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.Run(context.Background(), tt.req)
			if err == nil {
				t.Fatal("expected error")
			}
			bootErr, ok := err.(Error)
			if !ok {
				t.Fatalf("expected bootstrap.Error, got %T", err)
			}
			if bootErr.Code != tt.want {
				t.Fatalf("code=%q want %q", bootErr.Code, tt.want)
			}
		})
	}
}
