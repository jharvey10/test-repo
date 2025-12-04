// Package github provides shared GitHub client utilities for release tools.
package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
)

// RepoConfig holds repository configuration.
type RepoConfig struct {
	Owner string
	Repo  string
	Token string
}

// NewRepoConfigFromEnv creates a RepoConfig from environment variables.
// It reads GITHUB_TOKEN for the token and GITHUB_REPOSITORY for owner/repo.
func NewRepoConfigFromEnv() (RepoConfig, error) {
	cfg := RepoConfig{
		Token: os.Getenv("GITHUB_TOKEN"),
	}

	if cfg.Token == "" {
		return cfg, fmt.Errorf("GITHUB_TOKEN environment variable is required")
	}

	if ghRepo := os.Getenv("GITHUB_REPOSITORY"); ghRepo != "" {
		parts := strings.SplitN(ghRepo, "/", 2)
		if len(parts) == 2 {
			cfg.Owner = parts[0]
			cfg.Repo = parts[1]
		}
	}

	if cfg.Owner == "" || cfg.Repo == "" {
		return cfg, fmt.Errorf("GITHUB_REPOSITORY environment variable is required (format: owner/repo)")
	}

	return cfg, nil
}

// NewClient creates a new GitHub client with the given token.
func NewClient(ctx context.Context, token string) *github.Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

// BranchExists checks if a branch exists in the repository.
func BranchExists(ctx context.Context, client *github.Client, owner, repo, branch string) (bool, error) {
	_, _, err := client.Repositories.GetBranch(ctx, owner, repo, branch, 0)
	if err != nil {
		if errResp, ok := err.(*github.ErrorResponse); ok && errResp.Response.StatusCode == 404 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GetRefSHA resolves a ref (branch, tag, or commit SHA) to its SHA.
func GetRefSHA(ctx context.Context, client *github.Client, owner, repo, ref string) (string, error) {
	// Try as a branch first
	branch, _, err := client.Repositories.GetBranch(ctx, owner, repo, ref, 0)
	if err == nil {
		return branch.GetCommit().GetSHA(), nil
	}

	// Try as a tag
	tagRef, _, err := client.Git.GetRef(ctx, owner, repo, "tags/"+ref)
	if err == nil {
		return tagRef.GetObject().GetSHA(), nil
	}

	// Try as a commit SHA
	commit, _, err := client.Git.GetCommit(ctx, owner, repo, ref)
	if err == nil {
		return commit.GetSHA(), nil
	}

	return "", fmt.Errorf("could not resolve ref: %s", ref)
}

// CreateBranch creates a new branch from the given SHA.
func CreateBranch(ctx context.Context, client *github.Client, owner, repo, branch, sha string) error {
	ref := &github.Reference{
		Ref: github.String("refs/heads/" + branch),
		Object: &github.GitObject{
			SHA: github.String(sha),
		},
	}

	_, _, err := client.Git.CreateRef(ctx, owner, repo, ref)
	if err != nil {
		return fmt.Errorf("creating branch ref: %w", err)
	}

	return nil
}

// CreateTag creates an annotated tag.
func CreateTag(ctx context.Context, client *github.Client, owner, repo, tag, sha, message string) error {
	tagObj := &github.Tag{
		Tag:     github.String(tag),
		Message: github.String(message),
		Object: &github.GitObject{
			Type: github.String("commit"),
			SHA:  github.String(sha),
		},
	}

	createdTag, _, err := client.Git.CreateTag(ctx, owner, repo, tagObj)
	if err != nil {
		return fmt.Errorf("creating tag object: %w", err)
	}

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

// ReadManifest reads the release-please manifest from the repository.
func ReadManifest(ctx context.Context, client *github.Client, owner, repo, ref string) (map[string]string, error) {
	fileContent, _, _, err := client.Repositories.GetContents(
		ctx, owner, repo,
		".release-please-manifest.json",
		&github.RepositoryContentGetOptions{Ref: ref},
	)
	if err != nil {
		return nil, fmt.Errorf("getting manifest file: %w", err)
	}

	content, err := base64.StdEncoding.DecodeString(*fileContent.Content)
	if err != nil {
		return nil, fmt.Errorf("decoding manifest content: %w", err)
	}

	var manifest map[string]string
	if err := json.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest JSON: %w", err)
	}

	return manifest, nil
}

// FindOpenPR finds an open PR with the given head and base branches.
func FindOpenPR(ctx context.Context, client *github.Client, owner, repo, head, base string) (*github.PullRequest, error) {
	opts := &github.PullRequestListOptions{
		State: "open",
		Head:  fmt.Sprintf("%s:%s", owner, head),
		Base:  base,
		ListOptions: github.ListOptions{
			PerPage: 10,
		},
	}

	prs, _, err := client.PullRequests.List(ctx, owner, repo, opts)
	if err != nil {
		return nil, fmt.Errorf("listing pull requests: %w", err)
	}

	if len(prs) > 0 {
		return prs[0], nil
	}
	return nil, nil
}
