package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/grafana/alloy/tools/release/internal/git"
	gh "github.com/grafana/alloy/tools/release/internal/github"
)

func main() {
	var (
		prNumber int
		dryRun   bool
	)
	flag.IntVar(&prNumber, "pr", 0, "Release-please PR number that was merged")
	flag.BoolVar(&dryRun, "dry-run", false, "Dry run (do not merge)")
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

	// Check if the release branch is already fully merged into main
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
		fmt.Printf("Would merge: %s → main\n", releaseBranch)
		return
	}

	// Get the app identity for git commits
	appIdentity, err := client.GetAppIdentity(ctx)
	if err != nil {
		log.Fatalf("Failed to get app identity: %v", err)
	}

	// Configure git with app identity for commit authorship
	if err := git.ConfigureUser(appIdentity.Name, appIdentity.Email); err != nil {
		log.Fatalf("Failed to configure git: %v", err)
	}

	// Checkout main (assumes branches are already fetched)
	fmt.Println("🔀 Checking out main...")
	if err := git.Checkout("main"); err != nil {
		log.Fatalf("Failed to checkout main: %v", err)
	}

	// Merge the release branch into main using "ours" strategy.
	// This creates a merge commit that records the release branch history (including tags)
	// but keeps main's content unchanged.
	commitMessage := fmt.Sprintf("chore: forwardport %s to main\n\nForwardports the %s branch to main after the %s release.\n\nTriggered by release-please PR #%d: %s\n\nThis merge uses the 'ours' strategy to record the release branch history\n(including tags) without changing main's content.",
		releaseBranch,
		releaseBranch,
		version,
		originalPR.GetNumber(),
		originalPR.GetTitle(),
	)

	fmt.Printf("🔀 Merging %s into main (ours strategy)...\n", releaseBranch)
	if err := git.MergeOurs(releaseBranch, commitMessage); err != nil {
		log.Fatalf("Failed to merge %s into main: %v", releaseBranch, err)
	}

	// Push the result
	fmt.Println("📤 Pushing to origin...")
	if err := git.Push("main"); err != nil {
		log.Fatalf("Failed to push main: %v", err)
	}

	fmt.Printf("✅ Merged %s into main (ours strategy)\n", releaseBranch)
}
