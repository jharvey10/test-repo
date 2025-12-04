package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/google/go-github/v57/github"

	gh "github.com/grafana/alloy/tools/release/internal/github"
	"github.com/grafana/alloy/tools/release/internal/version"
)

func main() {
	var (
		dryRun bool
		tag    string
	)
	flag.BoolVar(&dryRun, "dry-run", false, "Dry run (do not create PR)")
	flag.StringVar(&tag, "tag", "", "Release tag (e.g., v1.15.0)")
	flag.Parse()

	if tag == "" {
		log.Fatal("Tag is required (use --tag flag, e.g., --tag v1.15.0)")
	}

	cfg, err := gh.NewRepoConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	client := gh.NewClient(ctx, cfg.Token)

	// Parse version from tag (v1.15.0 -> 1.15)
	minorVersion, err := version.MajorMinor(tag)
	if err != nil {
		log.Fatalf("Failed to parse version from tag: %v", err)
	}
	fmt.Printf("Tag: %s\n", tag)
	fmt.Printf("Minor version: %s\n", minorVersion)

	releaseBranch := fmt.Sprintf("release/v%s", minorVersion)
	fmt.Printf("Release branch: %s\n", releaseBranch)

	// Check if branch exists
	exists, err := gh.BranchExists(ctx, client, cfg.Owner, cfg.Repo, releaseBranch)
	if err != nil {
		log.Fatalf("Failed to check if branch exists: %v", err)
	}
	if !exists {
		log.Fatalf("Release branch %s does not exist", releaseBranch)
	}

	// Check for existing sync PR
	existingPR, err := gh.FindOpenPR(ctx, client, cfg.Owner, cfg.Repo, releaseBranch, "main")
	if err != nil {
		log.Fatalf("Failed to check for existing PR: %v", err)
	}
	if existingPR != nil {
		fmt.Printf("ℹ️  Sync PR already exists: %s\n", existingPR.GetHTMLURL())
		return
	}

	if dryRun {
		fmt.Println("\n🏃 DRY RUN - No changes made")
		fmt.Printf("Would create sync PR: %s → main\n", releaseBranch)
		return
	}

	// Create sync PR
	pr, err := createSyncPR(ctx, client, cfg.Owner, cfg.Repo, releaseBranch, minorVersion)
	if err != nil {
		log.Fatalf("Failed to create sync PR: %v", err)
	}

	fmt.Printf("✅ Created sync PR: %s\n", pr.GetHTMLURL())
}

func createSyncPR(ctx context.Context, client *github.Client, owner, repo, releaseBranch, minorVersion string) (*github.PullRequest, error) {
	title := fmt.Sprintf("chore: sync v%s release branch to main", minorVersion)
	body := fmt.Sprintf(`## Sync Release Branch

This PR merges changes from `+"`%s`"+` back into `+"`main`"+`.

### Review Checklist
- [ ] Review all changes for conflicts
- [ ] Ensure no release-specific changes are being merged back inappropriately
- [ ] Verify CI passes

---
*This PR was automatically created after the v%s.x release.*
`, releaseBranch, minorVersion)

	newPR := &github.NewPullRequest{
		Title: github.String(title),
		Head:  github.String(releaseBranch),
		Base:  github.String("main"),
		Body:  github.String(body),
	}

	pr, _, err := client.PullRequests.Create(ctx, owner, repo, newPR)
	if err != nil {
		return nil, fmt.Errorf("creating pull request: %w", err)
	}

	return pr, nil
}
