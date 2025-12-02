package runner

import (
	"context"
	"fmt"

	"github.com/jharvey10/test-repo/internal/component"
	"golang.org/x/sync/errgroup"
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

// Run starts all registered components concurrently using errgroup.
func (r *Runner) Run() error {
	fmt.Printf("Runner started with %d registered component types\n", len(component.All()))

	g, _ := errgroup.WithContext(context.Background())

	for _, c := range r.components {
		c := c // capture for goroutine
		g.Go(func() error {
			fmt.Printf("Running component: %s\n", c.Name())
			return c.Run()
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("component failed: %w", err)
	}

	fmt.Println("All components completed successfully")
	return nil
}
