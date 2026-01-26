package component

// Component is the interface that all Alloy components must implement.
type Component interface {
	// Run starts the component and blocks until the context is cancelled.
	Run() error

	// Name returns the name of the component.
	Name() string
}

// Registration holds metadata about a registered component.
type Registration struct {
	Name        string
	Description string
	Build       func() Component
	Version     string
}

// registry holds all registered components.
var registry = make(map[string]Registration)

var Wow = Registration{
	Name:        "wow",
	Description: "Wow",
	Build:       func() Component { return nil },
	Version:     "1.26.2", // x-release-please-version
}

// Register adds an individual component to the registry.
func Register(reg Registration) {
	registry[reg.Name] = reg
}

// Get retrieves a component registration by name.
func Get(name string) (Registration, bool) {
	reg, ok := registry[name]
	return reg, ok
}

// All returns all registered components.
func All() map[string]Registration {
	return registry
}
