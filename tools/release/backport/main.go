package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/google/go-github/v57/github"

	gh "github.com/grafana/alloy/tools/release/internal/github"
)

func main() {
	var (
		prNumber int
		label    string
		dryRun   bool
	)
	flag.IntVar(&prNumber, "pr", 0, "PR number to backport")
	flag.StringVar(&label, "label", "", "Backport label (e.g., backport/v1.15)")
	flag.BoolVar(&dryRun, "dry-run", false, "Dry run (do not create PR)")
	flag.Parse()

	if prNumber == 0 {
		log.Fatal("PR number is required (use --pr flag)")
	}
	if label == "" {
		log.Fatal("Label is required (use --label flag)")
	}

	// Parse version from label (backport/v1.15 -> v1.15)
	version := strings.TrimPrefix(label, "backport/")
	if version == label {
		log.Fatalf("Invalid backport label format: %s (expected backport/vX.Y)", label)
	}
	if !strings.HasPrefix(version, "v") {
		log.Fatalf("Invalid version format: %s (expected vX.Y)", version)
	}

	targetBranch := fmt.Sprintf("release/%s", version)
	backportBranch := fmt.Sprintf("backport/pr-%d-to-%s", prNumber, version)
	backportMarker := fmt.Sprintf("chore: backport #%d", prNumber)

	fmt.Printf("🍒 Backporting PR #%d to %s\n", prNumber, targetBranch)

	cfg, err := gh.NewRepoConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	client := gh.NewClient(ctx, cfg.Token)

	// Verify the target release branch exists
	exists, err := gh.BranchExists(ctx, client, cfg.Owner, cfg.Repo, targetBranch)
	if err != nil {
		log.Fatalf("Failed to check if target branch exists: %v", err)
	}
	if !exists {
		log.Fatalf("Target branch %s does not exist", targetBranch)
	}

	// Check if backport was already merged by looking for the marker in the release branch history
	alreadyMerged, err := commitExistsWithPattern(ctx, client, cfg.Owner, cfg.Repo, targetBranch, backportMarker)
	if err != nil {
		log.Fatalf("Failed to check for existing backport commit: %v", err)
	}
	if alreadyMerged {
		fmt.Printf("ℹ️  Backport already merged (found commit with %s in %s)\n", backportMarker, targetBranch)
		return
	}

	// Check if backport branch already exists (means there's an open PR or work in progress)
	branchExists, err := gh.BranchExists(ctx, client, cfg.Owner, cfg.Repo, backportBranch)
	if err != nil {
		log.Fatalf("Failed to check if backport branch exists: %v", err)
	}
	if branchExists {
		fmt.Printf("ℹ️  Backport branch %s already exists\n", backportBranch)
		return
	}

	// Get the original PR details
	originalPR, _, err := client.PullRequests.Get(ctx, cfg.Owner, cfg.Repo, prNumber)
	if err != nil {
		log.Fatalf("Failed to get original PR: %v", err)
	}

	// Find the commit on main that corresponds to this PR
	commitSHA, err := findCommitWithPattern(ctx, client, cfg.Owner, cfg.Repo, "main", fmt.Sprintf("(#%d)", prNumber))
	if err != nil {
		log.Fatalf("Failed to find commit for PR #%d: %v", prNumber, err)
	}
	fmt.Printf("   Found commit: %s\n", commitSHA)
	fmt.Printf("   Backport branch: %s\n", backportBranch)

	if dryRun {
		fmt.Println("\n🏃 DRY RUN - No changes made")
		fmt.Printf("Would create backport branch: %s\n", backportBranch)
		fmt.Printf("Would cherry-pick commit: %s\n", commitSHA)
		fmt.Printf("Would create PR: %s → %s\n", backportBranch, targetBranch)
		return
	}

	// Configure git for the cherry-pick
	if err := configureGit(); err != nil {
		log.Fatalf("Failed to configure git: %v", err)
	}

	// Fetch the target branch
	if err := gitFetch(targetBranch); err != nil {
		log.Fatalf("Failed to fetch target branch: %v", err)
	}

	// Create backport branch from target branch
	if err := gitCreateBranch(backportBranch, "origin/"+targetBranch); err != nil {
		log.Fatalf("Failed to create backport branch: %v", err)
	}

	// Cherry-pick the commit
	if err := gitCherryPick(commitSHA); err != nil {
		log.Fatalf("Failed to cherry-pick commit: %v\n\nThis may be due to conflicts. Please create the backport manually.", err)
	}

	// Push the backport branch
	if err := gitPush(backportBranch); err != nil {
		log.Fatalf("Failed to push backport branch: %v", err)
	}

	fmt.Printf("✅ Pushed backport branch: %s\n", backportBranch)

	// Create the backport PR
	backportPR, err := createBackportPR(ctx, client, cfg.Owner, cfg.Repo, originalPR, backportBranch, targetBranch, backportMarker)
	if err != nil {
		log.Fatalf("Failed to create backport PR: %v", err)
	}

	fmt.Printf("✅ Created backport PR: %s\n", backportPR.GetHTMLURL())
}

// commitExistsWithPattern checks if any commit in the branch history contains the pattern in its title.
func commitExistsWithPattern(ctx context.Context, client *github.Client, owner, repo, branch, pattern string) (bool, error) {
	_, err := findCommitWithPattern(ctx, client, owner, repo, branch, pattern)
	if err != nil {
		if strings.Contains(err.Error(), "no commit found") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// findCommitWithPattern searches the commit history of a branch for a commit whose title contains the pattern.
func findCommitWithPattern(ctx context.Context, client *github.Client, owner, repo, branch, pattern string) (string, error) {
	opts := &github.CommitsListOptions{
		SHA: branch,
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	// Search through recent commits
	for range 5 {
		commits, resp, err := client.Repositories.ListCommits(ctx, owner, repo, opts)
		if err != nil {
			return "", fmt.Errorf("listing commits: %w", err)
		}

		for _, commit := range commits {
			message := commit.GetCommit().GetMessage()
			// Check just the first line (title) of the commit message
			title := strings.Split(message, "\n")[0]
			if strings.Contains(title, pattern) {
				return commit.GetSHA(), nil
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return "", fmt.Errorf("no commit found with pattern %q in branch %s", pattern, branch)
}

func configureGit() error {
	cmds := [][]string{
		{"git", "config", "user.name", "github-actions[bot]"},
		{"git", "config", "user.email", "github-actions[bot]@users.noreply.github.com"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("running %v: %w", args, err)
		}
	}
	return nil
}

func gitFetch(branch string) error {
	cmd := exec.Command("git", "fetch", "origin", branch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("fetching branch %s: %w", branch, err)
	}
	return nil
}

func gitCreateBranch(branch, base string) error {
	cmd := exec.Command("git", "checkout", "-b", branch, base)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("creating branch %s from %s: %w", branch, base, err)
	}
	return nil
}

func gitCherryPick(sha string) error {
	cmd := exec.Command("git", "cherry-pick", sha)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cherry-picking commit %s: %w", sha, err)
	}
	return nil
}

func gitPush(branch string) error {
	cmd := exec.Command("git", "push", "origin", branch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pushing branch %s: %w", branch, err)
	}
	return nil
}

func createBackportPR(ctx context.Context, client *github.Client, owner, repo string, originalPR *github.PullRequest, backportBranch, targetBranch, backportMarker string) (*github.PullRequest, error) {
	title := backportMarker

	body := fmt.Sprintf(`## Backport of #%d

This PR backports #%d to %s.

### Original PR
- **Title:** %s
- **Author:** @%s

### Description
%s

---
*This backport was created automatically.*
`,
		originalPR.GetNumber(),
		originalPR.GetNumber(),
		targetBranch,
		originalPR.GetTitle(),
		originalPR.GetUser().GetLogin(),
		originalPR.GetBody(),
	)

	newPR := &github.NewPullRequest{
		Title: github.String(title),
		Head:  github.String(backportBranch),
		Base:  github.String(targetBranch),
		Body:  github.String(body),
	}

	pr, _, err := client.PullRequests.Create(ctx, owner, repo, newPR)
	if err != nil {
		return nil, fmt.Errorf("creating pull request: %w", err)
	}

	return pr, nil
}
