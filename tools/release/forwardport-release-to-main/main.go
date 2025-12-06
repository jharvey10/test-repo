package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/google/go-github/v57/github"

	gh "github.com/grafana/alloy/tools/release/internal/github"
)

func main() {
	var (
		prNumber int
		dryRun   bool
	)
	flag.IntVar(&prNumber, "pr", 0, "Release-please PR number that was merged")
	flag.BoolVar(&dryRun, "dry-run", false, "Dry run (do not create PR)")
	flag.Parse()

	if prNumber == 0 {
		log.Fatal("PR number is required (use --pr flag)")
	}

	ctx := context.Background()

	client, err := gh.NewClientFromEnv(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// Get the original release-please PR details
	originalPR, err := client.GetPR(ctx, prNumber)
	if err != nil {
		log.Fatalf("Failed to get PR #%d: %v", prNumber, err)
	}

	if originalPR.GetMergedAt().IsZero() {
		log.Fatalf("PR #%d is not merged", prNumber)
	}

	// The base branch should be a release branch (e.g., release/v1.15)
	releaseBranch := originalPR.GetBase().GetRef()
	if !strings.HasPrefix(releaseBranch, "release/") {
		log.Fatalf("PR #%d base branch %s is not a release branch", prNumber, releaseBranch)
	}

	// Extract version from release branch (release/v1.15 -> v1.15)
	version := strings.TrimPrefix(releaseBranch, "release/")

	fmt.Printf("🔀 Merging release branch to main after release-please PR #%d\n", prNumber)
	fmt.Printf("   Release branch: %s\n", releaseBranch)
	fmt.Printf("   Version: %s\n", version)

	// Check if there's already an open PR from this release branch to main
	existingPR, err := client.FindOpenPR(ctx, gh.FindOpenPRParams{
		Head: releaseBranch,
		Base: "main",
	})
	if err != nil {
		log.Fatalf("Failed to check for existing PR: %v", err)
	}
	if existingPR != nil {
		fmt.Printf("ℹ️  PR already exists: %s\n", existingPR.GetHTMLURL())
		return
	}

	// Check if the release branch is already fully merged into main
	// We do this by comparing the branches - if main contains all commits from release, skip
	alreadyMerged, err := client.IsBranchMergedInto(ctx, releaseBranch, "main")
	if err != nil {
		log.Fatalf("Failed to check if branch is merged: %v", err)
	}
	if alreadyMerged {
		fmt.Printf("ℹ️  Release branch %s is already merged into main\n", releaseBranch)
		return
	}

	if dryRun {
		fmt.Println("\n🏃 DRY RUN - No changes made")
		fmt.Printf("Would create PR: %s → main\n", releaseBranch)
		return
	}

	// Create a PR to merge the release branch into main
	forwardportPR, err := createForwardportPR(ctx, client, originalPR, releaseBranch, version)
	if err != nil {
		log.Fatalf("Failed to create forwardport PR: %v", err)
	}

	fmt.Printf("✅ Created forwardport PR: %s\n", forwardportPR.GetHTMLURL())
}

func createForwardportPR(ctx context.Context, client *gh.Client, originalPR *github.PullRequest, releaseBranch, version string) (*github.PullRequest, error) {
	title := fmt.Sprintf("chore: forwardport %s to main", releaseBranch)

	body := fmt.Sprintf(`## Forwardport Release Branch to Main

This PR forwardports the %s branch to main after the %s release.

### Triggered By
- **Release-Please PR:** #%d
- **Title:** %s

### What's Being Merged
This brings all release commits (changelog updates, version bumps, tags, etc.) from the release branch into main. This keeps main in sync with all releases and ensures subsequent release branches have access to the full history.

### Merge Strategy
This PR should be merged with a **merge commit** (not squash or rebase) to preserve the release history and tag reachability.

---
*This forwardport PR was created automatically when the release-please PR was merged.*
`,
		releaseBranch,
		version,
		originalPR.GetNumber(),
		originalPR.GetTitle(),
	)

	return client.CreatePR(ctx, gh.CreatePRParams{
		Title: title,
		Head:  releaseBranch,
		Base:  "main",
		Body:  body,
	})
}
