package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
)

type Config struct {
	Owner  string
	Repo   string
	Token  string
	DryRun bool
}

func main() {
	cfg := parseFlags()

	if cfg.Token == "" {
		log.Fatal("GITHUB_TOKEN environment variable is required")
	}

	ctx := context.Background()
	client := newGitHubClient(ctx, cfg.Token)

	// Find the release-please PR
	pr, err := findReleasePleasePR(ctx, client, cfg.Owner, cfg.Repo)
	if err != nil {
		log.Fatalf("Failed to find release-please PR: %v", err)
	}

	fmt.Printf("Found release-please PR #%d: %s\n", pr.GetNumber(), pr.GetTitle())
	fmt.Printf("Branch: %s\n", pr.GetHead().GetRef())

	// Extract version from PR title
	version, err := extractVersionFromTitle(pr.GetTitle())
	if err != nil {
		log.Fatalf("Failed to extract version from PR title: %v", err)
	}
	fmt.Printf("Target version: %s\n", version)

	// Find existing RC tags and determine next RC number
	rcNumber, err := findNextRCNumber(ctx, client, cfg.Owner, cfg.Repo, version)
	if err != nil {
		log.Fatalf("Failed to determine next RC number: %v", err)
	}

	rcTag := fmt.Sprintf("v%s-rc.%d", version, rcNumber)
	fmt.Printf("Next RC tag: %s\n", rcTag)

	if cfg.DryRun {
		fmt.Println("\n🏃 DRY RUN - No changes made")
		fmt.Printf("Would create tag: %s\n", rcTag)
		fmt.Printf("From branch: %s\n", pr.GetHead().GetRef())
		return
	}

	// Get the SHA of the PR branch head
	branchSHA := pr.GetHead().GetSHA()
	fmt.Printf("Branch HEAD SHA: %s\n", branchSHA)

	// Create the tag
	err = createTag(ctx, client, cfg.Owner, cfg.Repo, rcTag, branchSHA)
	if err != nil {
		log.Fatalf("Failed to create tag: %v", err)
	}
	fmt.Printf("✅ Created tag: %s\n", rcTag)

	// Create draft prerelease
	releaseURL, err := createDraftPrerelease(ctx, client, cfg.Owner, cfg.Repo, rcTag, version, rcNumber, pr.GetNumber())
	if err != nil {
		log.Fatalf("Failed to create draft prerelease: %v", err)
	}
	fmt.Printf("✅ Created draft prerelease: %s\n", releaseURL)
}

func parseFlags() Config {
	var cfg Config

	flag.BoolVar(&cfg.DryRun, "dry-run", false, "Dry run (do not create tag or release)")
	flag.StringVar(&cfg.Owner, "owner", "", "GitHub repository owner")
	flag.StringVar(&cfg.Repo, "repo", "", "GitHub repository name")
	flag.Parse()

	cfg.Token = os.Getenv("GITHUB_TOKEN")

	// Try to parse from GITHUB_REPOSITORY if owner/repo not provided
	if cfg.Owner == "" || cfg.Repo == "" {
		if ghRepo := os.Getenv("GITHUB_REPOSITORY"); ghRepo != "" {
			parts := strings.SplitN(ghRepo, "/", 2)
			if len(parts) == 2 {
				cfg.Owner = parts[0]
				cfg.Repo = parts[1]
			}
		}
	}

	if cfg.Owner == "" || cfg.Repo == "" {
		log.Fatal("Repository owner and name are required (use -owner and -repo flags, or set GITHUB_REPOSITORY)")
	}

	return cfg
}

func newGitHubClient(ctx context.Context, token string) *github.Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

func findReleasePleasePR(ctx context.Context, client *github.Client, owner, repo string) (*github.PullRequest, error) {
	// Search for open PRs with the release-please label
	opts := &github.PullRequestListOptions{
		State: "open",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	prs, _, err := client.PullRequests.List(ctx, owner, repo, opts)
	if err != nil {
		return nil, fmt.Errorf("listing pull requests: %w", err)
	}

	// Look for PR with "autorelease: pending" label
	for _, pr := range prs {
		for _, label := range pr.Labels {
			if label.GetName() == "autorelease: pending" {
				return pr, nil
			}
		}
	}

	// Fallback: look for PR with release-please title pattern
	titlePattern := regexp.MustCompile(`^chore\(main\): release`)
	for _, pr := range prs {
		if titlePattern.MatchString(pr.GetTitle()) {
			return pr, nil
		}
	}

	return nil, fmt.Errorf("no release-please PR found (looked for 'autorelease: pending' label or 'chore(main): release' title)")
}

func extractVersionFromTitle(title string) (string, error) {
	// Match "chore(main): release X.Y.Z" or similar patterns
	pattern := regexp.MustCompile(`release\s+(\d+\.\d+\.\d+)`)
	matches := pattern.FindStringSubmatch(title)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not extract version from title: %s", title)
	}
	return matches[1], nil
}

func findNextRCNumber(ctx context.Context, client *github.Client, owner, repo, version string) (int, error) {
	// List all tags
	opts := &github.ListOptions{PerPage: 100}
	var allTags []*github.RepositoryTag

	for {
		tags, resp, err := client.Repositories.ListTags(ctx, owner, repo, opts)
		if err != nil {
			return 0, fmt.Errorf("listing tags: %w", err)
		}
		allTags = append(allTags, tags...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	// Find existing RC tags for this version
	rcPattern := regexp.MustCompile(fmt.Sprintf(`^v%s-rc\.(\d+)$`, regexp.QuoteMeta(version)))
	var rcNumbers []int

	for _, tag := range allTags {
		matches := rcPattern.FindStringSubmatch(tag.GetName())
		if len(matches) == 2 {
			num, _ := strconv.Atoi(matches[1])
			rcNumbers = append(rcNumbers, num)
		}
	}

	if len(rcNumbers) == 0 {
		return 0, nil
	}

	sort.Ints(rcNumbers)
	return rcNumbers[len(rcNumbers)-1] + 1, nil
}

func createTag(ctx context.Context, client *github.Client, owner, repo, tag, sha string) error {
	// Create annotated tag object
	tagObj := &github.Tag{
		Tag:     github.String(tag),
		Message: github.String(fmt.Sprintf("Release candidate %s", tag)),
		Object: &github.GitObject{
			Type: github.String("commit"),
			SHA:  github.String(sha),
		},
	}

	createdTag, _, err := client.Git.CreateTag(ctx, owner, repo, tagObj)
	if err != nil {
		return fmt.Errorf("creating tag object: %w", err)
	}

	// Create reference for the tag
	ref := &github.Reference{
		Ref: github.String("refs/tags/" + tag),
		Object: &github.GitObject{
			SHA: createdTag.SHA,
		},
	}

	_, _, err = client.Git.CreateRef(ctx, owner, repo, ref)
	if err != nil {
		return fmt.Errorf("creating tag reference: %w", err)
	}

	return nil
}

func createDraftPrerelease(ctx context.Context, client *github.Client, owner, repo, tag, version string, rcNumber, prNumber int) (string, error) {
	body := fmt.Sprintf(`## Release Candidate %d for v%s

This is a **release candidate** and should be used for testing purposes only.

**⚠️ This is a pre-release. Do not use in production.**

### Changes

See the [release PR #%d](https://github.com/%s/%s/pull/%d) for the full changelog.

### Testing

Please test this release candidate and report any issues before the final release.
`, rcNumber, version, prNumber, owner, repo, prNumber)

	release := &github.RepositoryRelease{
		TagName:    github.String(tag),
		Name:       github.String(tag),
		Body:       github.String(body),
		Draft:      github.Bool(true),
		Prerelease: github.Bool(true),
	}

	created, _, err := client.Repositories.CreateRelease(ctx, owner, repo, release)
	if err != nil {
		return "", fmt.Errorf("creating release: %w", err)
	}

	return created.GetHTMLURL(), nil
}
