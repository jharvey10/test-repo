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

	// Use a separate sync branch for the PR to avoid auto-closing when the release branch is updated
	// for patch releases
	releaseBranch := fmt.Sprintf("release/v%s", minorVersion)
	syncBranch := fmt.Sprintf("sync/release-v%s", minorVersion)
	fmt.Printf("Release branch: %s\n", releaseBranch)
	fmt.Printf("Sync branch: %s\n", syncBranch)

	exists, err := gh.BranchExists(ctx, client, cfg.Owner, cfg.Repo, releaseBranch)
	if err != nil {
		log.Fatalf("Failed to check if branch exists: %v", err)
	}
	if !exists {
		log.Fatalf("Release branch %s does not exist", releaseBranch)
	}

	releaseBranchSHA, err := gh.GetRefSHA(ctx, client, cfg.Owner, cfg.Repo, releaseBranch)
	if err != nil {
		log.Fatalf("Failed to get release branch SHA: %v", err)
	}
	fmt.Printf("Release branch SHA: %s\n", releaseBranchSHA)

	if err := ensureSyncBranch(ctx, client, cfg.Owner, cfg.Repo, syncBranch, releaseBranchSHA, dryRun); err != nil {
		log.Fatalf("Failed to ensure sync branch: %v", err)
	}

	if err := ensureSyncPR(ctx, client, cfg.Owner, cfg.Repo, syncBranch, releaseBranch, minorVersion, dryRun); err != nil {
		log.Fatalf("Failed to ensure sync PR: %v", err)
	}
}

// ensureSyncBranch creates the sync branch if it doesn't exist, or updates it
// to point to the given SHA if it does.
func ensureSyncBranch(ctx context.Context, client *github.Client, owner, repo, syncBranch, targetSHA string, dryRun bool) error {
	exists, err := gh.BranchExists(ctx, client, owner, repo, syncBranch)
	if err != nil {
		return fmt.Errorf("checking if sync branch exists: %w", err)
	}

	if dryRun {
		if exists {
			fmt.Printf("🏃 DRY RUN - Would update sync branch %s to %s\n", syncBranch, targetSHA[:7])
		} else {
			fmt.Printf("🏃 DRY RUN - Would create sync branch %s at %s\n", syncBranch, targetSHA[:7])
		}
		return nil
	}

	if exists {
		if err := updateBranchRef(ctx, client, owner, repo, syncBranch, targetSHA); err != nil {
			return fmt.Errorf("updating sync branch: %w", err)
		}
		fmt.Printf("✅ Updated sync branch %s to %s\n", syncBranch, targetSHA[:7])
	} else {
		if err := gh.CreateBranch(ctx, client, owner, repo, syncBranch, targetSHA); err != nil {
			return fmt.Errorf("creating sync branch: %w", err)
		}
		fmt.Printf("✅ Created sync branch %s at %s\n", syncBranch, targetSHA[:7])
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

This PR merges changes from `+"`%s`"+` back into `+"`main`"+`.

**Note:** This PR uses a dedicated sync branch (`+"`%s`"+`) to avoid auto-close issues when the release branch is updated.

### Review Checklist
- [ ] Review all changes for conflicts
- [ ] Ensure no release-specific changes are being merged back inappropriately
- [ ] Verify CI passes

---
*This PR was automatically created after the v%s.x release.*
`, releaseBranch, syncBranch, minorVersion)

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
