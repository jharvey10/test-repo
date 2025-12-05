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

	// Use a separate sync branch for the PR to avoid auto-closing when the release branch is updated.
	// We create a single squashed commit containing the release branch's file state on top of main.
	releaseBranch := fmt.Sprintf("release/v%s", minorVersion)
	syncBranch := fmt.Sprintf("sync/release-v%s", minorVersion)
	fmt.Printf("Release branch: %s\n", releaseBranch)
	fmt.Printf("Sync branch: %s\n", syncBranch)

	exists, err := gh.BranchExists(ctx, client, cfg.Owner, cfg.Repo, releaseBranch)
	if err != nil {
		log.Fatalf("Failed to check if release branch exists: %v", err)
	}
	if !exists {
		log.Fatalf("Release branch %s does not exist", releaseBranch)
	}

	mainSHA, err := gh.GetRefSHA(ctx, client, cfg.Owner, cfg.Repo, "main")
	if err != nil {
		log.Fatalf("Failed to get main branch SHA: %v", err)
	}
	fmt.Printf("Main branch SHA: %s\n", mainSHA)

	releaseSHA, err := gh.GetRefSHA(ctx, client, cfg.Owner, cfg.Repo, releaseBranch)
	if err != nil {
		log.Fatalf("Failed to get release branch SHA: %v", err)
	}
	fmt.Printf("Release branch SHA: %s\n", releaseSHA)

	if err := ensureSyncBranch(ctx, client, cfg.Owner, cfg.Repo, syncBranch, mainSHA, releaseSHA, releaseBranch, dryRun); err != nil {
		log.Fatalf("Failed to ensure sync branch: %v", err)
	}

	if err := ensureSyncPR(ctx, client, cfg.Owner, cfg.Repo, syncBranch, releaseBranch, minorVersion, dryRun); err != nil {
		log.Fatalf("Failed to ensure sync PR: %v", err)
	}
}

// ensureSyncBranch creates a sync branch with a single squashed commit containing the
// release branch's file state on top of main. This produces a clean single-commit PR.
func ensureSyncBranch(ctx context.Context, client *github.Client, owner, repo, syncBranch, mainSHA, releaseSHA, releaseBranch string, dryRun bool) error {
	if dryRun {
		fmt.Printf("🏃 DRY RUN - Would create squashed sync commit from %s on top of main\n", releaseBranch)
		return nil
	}

	// Get the tree from the release branch (this is the file state we want)
	releaseCommit, _, err := client.Git.GetCommit(ctx, owner, repo, releaseSHA)
	if err != nil {
		return fmt.Errorf("getting release commit: %w", err)
	}
	releaseTreeSHA := releaseCommit.GetTree().GetSHA()

	// Create a new commit with the release tree but main as the parent
	// This is effectively a squash of all release branch changes
	commitMessage := fmt.Sprintf("chore: sync %s to main\n\nSquashed commit containing all changes from %s.", releaseBranch, releaseBranch)
	newCommit := &github.Commit{
		Message: github.String(commitMessage),
		Tree:    &github.Tree{SHA: github.String(releaseTreeSHA)},
		Parents: []*github.Commit{{SHA: github.String(mainSHA)}},
	}

	createdCommit, _, err := client.Git.CreateCommit(ctx, owner, repo, newCommit, nil)
	if err != nil {
		return fmt.Errorf("creating squashed commit: %w", err)
	}
	squashSHA := createdCommit.GetSHA()
	fmt.Printf("✅ Created squashed commit %s\n", squashSHA[:7])

	// Create or update the sync branch to point to our new commit
	syncBranchExists, err := gh.BranchExists(ctx, client, owner, repo, syncBranch)
	if err != nil {
		return fmt.Errorf("checking if sync branch exists: %w", err)
	}

	if syncBranchExists {
		if err := updateBranchRef(ctx, client, owner, repo, syncBranch, squashSHA); err != nil {
			return fmt.Errorf("updating sync branch: %w", err)
		}
		fmt.Printf("✅ Updated sync branch %s\n", syncBranch)
	} else {
		if err := gh.CreateBranch(ctx, client, owner, repo, syncBranch, squashSHA); err != nil {
			return fmt.Errorf("creating sync branch: %w", err)
		}
		fmt.Printf("✅ Created sync branch %s\n", syncBranch)
	}

	return nil
}

// updateBranchRef force-updates a branch to point to a new SHA.
func updateBranchRef(ctx context.Context, client *github.Client, owner, repo, branch, sha string) error {
	ref := &github.Reference{
		Ref: github.String("refs/heads/" + branch),
		Object: &github.GitObject{
			SHA: github.String(sha),
		},
	}

	_, _, err := client.Git.UpdateRef(ctx, owner, repo, ref, true) // force update
	if err != nil {
		return fmt.Errorf("updating branch ref: %w", err)
	}
	return nil
}

// ensureSyncPR creates a sync PR if one doesn't already exist.
func ensureSyncPR(ctx context.Context, client *github.Client, owner, repo, syncBranch, releaseBranch, minorVersion string, dryRun bool) error {
	existingPR, err := gh.FindOpenPR(ctx, client, owner, repo, syncBranch, "main")
	if err != nil {
		return fmt.Errorf("checking for existing PR: %w", err)
	}

	if existingPR != nil {
		fmt.Printf("ℹ️  Sync PR already exists: %s\n", existingPR.GetHTMLURL())
		return nil
	}

	if dryRun {
		fmt.Printf("🏃 DRY RUN - Would create sync PR: %s → main\n", syncBranch)
		return nil
	}

	pr, err := createSyncPR(ctx, client, owner, repo, syncBranch, releaseBranch, minorVersion)
	if err != nil {
		return fmt.Errorf("creating sync PR: %w", err)
	}

	fmt.Printf("✅ Created sync PR: %s\n", pr.GetHTMLURL())
	return nil
}

func createSyncPR(ctx context.Context, client *github.Client, owner, repo, syncBranch, releaseBranch, minorVersion string) (*github.PullRequest, error) {
	title := fmt.Sprintf("chore: sync v%s release branch to main", minorVersion)
	body := fmt.Sprintf(`## Sync Release Branch

This PR syncs the file state from `+"`%s`"+` back into `+"`main`"+`.

This is a squashed commit containing all changes from the release branch, so the diff shows only the net file changes.

### Review Checklist
- [ ] Review all changes for conflicts
- [ ] Ensure no release-specific changes are being merged back inappropriately
- [ ] Verify CI passes

---
*This PR was automatically created after the v%s.x release.*
`, releaseBranch, minorVersion)

	newPR := &github.NewPullRequest{
		Title: github.String(title),
		Head:  github.String(syncBranch),
		Base:  github.String("main"),
		Body:  github.String(body),
	}

	pr, _, err := client.PullRequests.Create(ctx, owner, repo, newPR)
	if err != nil {
		return nil, fmt.Errorf("creating pull request: %w", err)
	}

	return pr, nil
}
