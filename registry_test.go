package strands

import (
	"context"
	"strings"
	"testing"
)

func TestToolRegistry_Register(t *testing.T) {
	r := NewToolRegistry()
	tool := echoTool()
	if err := r.Register(tool); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if r.Len() != 1 {
		t.Errorf("Len() = %d, want 1", r.Len())
	}
}

func TestToolRegistry_Get(t *testing.T) {
	r := NewToolRegistry()
	_ = r.Register(echoTool())

	tool, ok := r.Get("echo")
	if !ok || tool == nil {
		t.Fatal("Get(echo) returned not found")
	}
	if tool.Spec().Name != "echo" {
		t.Errorf("Spec().Name = %q, want %q", tool.Spec().Name, "echo")
	}
}

func TestToolRegistry_GetNotFound(t *testing.T) {
	r := NewToolRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("Get(nonexistent) should return false")
	}
}

func TestToolRegistry_DuplicateNameRejected(t *testing.T) {
	r := NewToolRegistry()
	_ = r.Register(echoTool())
	err := r.Register(echoTool())
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("error = %q, want 'already registered'", err.Error())
	}
}

func TestToolRegistry_InvalidName(t *testing.T) {
	cases := []string{
		"",                    // empty
		"hello world",         // space
		strings.Repeat("x", 65), // too long
		"foo@bar",             // special char
	}
	for _, name := range cases {
		r := NewToolRegistry()
		tool := NewFuncTool(name, "test", func(_ context.Context, input map[string]any) (any, error) {
			return nil, nil
		}, nil)
		if err := r.Register(tool); err == nil {
			t.Errorf("expected error for invalid name %q", name)
		}
	}
}

func TestToolRegistry_Specs(t *testing.T) {
	r := NewToolRegistry()
	_ = r.Register(echoTool())
	_ = r.Register(failTool())

	specs := r.Specs()
	if len(specs) != 2 {
		t.Fatalf("Specs() returned %d, want 2", len(specs))
	}

	names := map[string]bool{}
	for _, s := range specs {
		names[s.Name] = true
	}
	if !names["echo"] || !names["fail"] {
		t.Errorf("Specs() names = %v, want echo and fail", names)
	}
}

func TestToolRegistry_Names(t *testing.T) {
	r := NewToolRegistry()
	_ = r.Register(echoTool())
	_ = r.Register(failTool())

	names := r.Names()
	if len(names) != 2 {
		t.Errorf("Names() returned %d, want 2", len(names))
	}
}
