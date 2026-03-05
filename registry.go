package strands

import (
	"fmt"
	"regexp"
	"sync"
)

var toolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// ToolRegistry is the central store for registered tools.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates an empty tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry. Returns an error if the name is invalid or already taken.
func (r *ToolRegistry) Register(tool Tool) error {
	spec := tool.Spec()
	if !toolNamePattern.MatchString(spec.Name) {
		return fmt.Errorf("strands: invalid tool name %q (must match %s)", spec.Name, toolNamePattern.String())
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[spec.Name]; exists {
		return fmt.Errorf("strands: tool %q already registered", spec.Name)
	}
	r.tools[spec.Name] = tool
	return nil
}

// Get returns the tool with the given name, or (nil, false) if not found.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Specs returns ToolSpecs for all registered tools, suitable for passing to the model.
func (r *ToolRegistry) Specs() []ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	specs := make([]ToolSpec, 0, len(r.tools))
	for _, t := range r.tools {
		specs = append(specs, t.Spec())
	}
	return specs
}

// Names returns the names of all registered tools.
func (r *ToolRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Len returns the number of registered tools.
func (r *ToolRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}
