package prometheus

import (
	"fmt"

	"github.com/jharvey10/test-repo/internal/component"
)

func init() {
	component.Register(component.Registration{
		Name:        "prometheus.scrape",
		Description: "Scrapes Prometheus metrics from targets",
		Build:       func() component.Component { return New() },
	})
}

// Scraper implements a Prometheus metrics scraper component.
type Scraper struct {
	targets []string
}

// New creates a new Prometheus scraper. Oh hi.
func New() *Scraper {
	return &Scraper{
		targets: make([]string, 0),
	}
}

// Name returns the component name.
func (s *Scraper) Name() string {
	return "prometheus.scrape"
}

// Run starts the scraper.
func (s *Scraper) Run() error {
	fmt.Printf("[%s] Starting with %d targets\n", s.Name(), len(s.targets))
	return nil
}

// AddTarget adds a scrape target.
func (s *Scraper) AddTarget(target string) {
	s.targets = append(s.targets, target)
}
