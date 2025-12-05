package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/google/go-github/v57/github"

	"github.com/grafana/alloy/tools/release/internal/git"
	gh "github.com/grafana/alloy/tools/release/internal/github"
)

type syncPRParams struct {
	OriginalPR *github.PullRequest
	SyncBranch string
	SyncMarker string
	Version    string
}

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

	// Get the app identity for git commits
	appIdentity, err := client.GetAppIdentity(ctx)
	if err != nil {
		log.Fatalf("Failed to get app identity: %v", err)
	}

	if originalPR.GetMergedAt().IsZero() {
		log.Fatalf("PR #%d is not merged", prNumber)
	}

	// The base branch should be a release branch (e.g., release/v1.15)
	baseBranch := originalPR.GetBase().GetRef()
	if !strings.HasPrefix(baseBranch, "release/") {
		log.Fatalf("PR #%d base branch %s is not a release branch", prNumber, baseBranch)
	}

	// Extract version from release branch (release/v1.15 -> v1.15)
	version := strings.TrimPrefix(baseBranch, "release/")
	syncBranch := fmt.Sprintf("sync/release-pr-%d-to-main", prNumber)
	syncMarker := fmt.Sprintf("chore: sync release-please #%d to main", prNumber)

	fmt.Printf("🔄 Syncing release-please PR #%d to main\n", prNumber)
	fmt.Printf("   Source branch: %s\n", baseBranch)
	fmt.Printf("   Version: %s\n", version)

	// Check if sync branch already exists (i.e. there's an open PR or work in progress)
	branchExists, err := git.BranchExistsOnRemote(syncBranch)
	if err != nil {
		log.Fatalf("Failed to check if sync branch exists: %v", err)
	}
	if branchExists {
		fmt.Printf("ℹ️  Sync branch %s already exists\n", syncBranch)
		return
	}

	// Check if sync was already merged by looking for the marker in main's history
	alreadyMerged, err := client.CommitExistsWithPattern(ctx, gh.FindCommitParams{
		Branch:  "main",
		Pattern: syncMarker,
	})
	if err != nil {
		log.Fatalf("Failed to check for existing sync commit: %v", err)
	}
	if alreadyMerged {
		fmt.Printf("ℹ️  Sync already merged (found commit with %q in main)\n", syncMarker)
		return
	}

	// Get the commit SHA for this PR (squashed commit with linear history)
	commitSHA := originalPR.GetMergeCommitSHA()
	if commitSHA == "" {
		log.Fatalf("Could not find commit SHA for PR #%d", prNumber)
	}
	fmt.Printf("   Commit: %s\n", commitSHA)
	fmt.Printf("   Sync branch: %s\n", syncBranch)

	if dryRun {
		fmt.Println("\n🏃 DRY RUN - No changes made")
		fmt.Printf("Would create sync branch: %s\n", syncBranch)
		fmt.Printf("Would cherry-pick commit: %s\n", commitSHA)
		fmt.Printf("Would create PR: %s → main\n", syncBranch)
		return
	}

	// Configure git with app identity for commit authorship
	if err := git.ConfigureUser(appIdentity.Name, appIdentity.Email); err != nil {
		log.Fatalf("Failed to configure git: %v", err)
	}

	// Fetch branches for cherry-pick
	if err := git.Fetch("main"); err != nil {
		log.Fatalf("Failed to fetch main: %v", err)
	}
	if err := git.Fetch(baseBranch); err != nil {
		log.Fatalf("Failed to fetch release branch: %v", err)
	}

	// Create sync branch from main
	if err := git.CreateBranchFrom(syncBranch, "origin/main"); err != nil {
		log.Fatalf("Failed to create sync branch: %v", err)
	}

	// Cherry-pick the commit
	if err := git.CherryPick(commitSHA); err != nil {
		log.Fatalf("Failed to cherry-pick commit: %v\n\nThis may be due to conflicts. Please create the sync manually.", err)
	}

	// Push the sync branch
	if err := git.Push(syncBranch); err != nil {
		log.Fatalf("Failed to push sync branch: %v", err)
	}

	fmt.Printf("✅ Pushed sync branch: %s\n", syncBranch)

	// Create the sync PR
	syncPR, err := createSyncPR(ctx, client, syncPRParams{
		OriginalPR: originalPR,
		SyncBranch: syncBranch,
		SyncMarker: syncMarker,
		Version:    version,
	})
	if err != nil {
		log.Fatalf("Failed to create sync PR: %v", err)
	}

	fmt.Printf("✅ Created sync PR: %s\n", syncPR.GetHTMLURL())
}

func createSyncPR(ctx context.Context, client *gh.Client, p syncPRParams) (*github.PullRequest, error) {
	body := fmt.Sprintf(`## Sync Release-Please Changes to Main

This PR syncs the release-please changes from #%d back to main.

### Original PR
- **Title:** %s
- **Version:** %s
- **Branch:** %s

### What's Being Synced
This cherry-picks the release-please changes (changelog updates, version bumps, etc.) from the release branch to main to keep them in sync.

---
*This sync PR was created automatically when the release-please PR was merged.*
`,
		p.OriginalPR.GetNumber(),
		p.OriginalPR.GetTitle(),
		p.Version,
		p.OriginalPR.GetBase().GetRef(),
	)

	return client.CreatePR(ctx, gh.CreatePRParams{
		Title: p.SyncMarker,
		Head:  p.SyncBranch,
		Base:  "main",
		Body:  body,
	})
}
