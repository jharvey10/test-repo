package runner

import (
	"fmt"

	"github.com/jharvey10/test-repo/internal/component"
)

// Runner manages the lifecycle of components.
type Runner struct {
	components []component.Component
}

// New creates a new Runner instance.
func New() *Runner {
	return &Runner{
		components: make([]component.Component, 0),
	}
}

// Add registers a component with the runner.
func (r *Runner) Add(c component.Component) {
	r.components = append(r.components, c)
}

// Run starts all registered components.
func (r *Runner) Run() error {
	fmt.Printf("Runner started with %d registered component types\n", len(component.All()))

	for _, c := range r.components {
		fmt.Printf("Running component: %s\n", c.Name())
		if err := c.Run(); err != nil {
			return fmt.Errorf("component %s failed: %w", c.Name(), err)
		}
	}

	fmt.Println("All components completed successfully")
	return nil
}
