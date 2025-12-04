package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	gh "github.com/grafana/alloy/tools/release/internal/github"
	"github.com/grafana/alloy/tools/release/internal/version"
)

func main() {
	var (
		dryRun    bool
		sourceRef string
	)
	flag.BoolVar(&dryRun, "dry-run", false, "Dry run (do not create branch)")
	flag.StringVar(&sourceRef, "source", "main", "Source ref to branch from")
	flag.Parse()

	cfg, err := gh.NewRepoConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	client := gh.NewClient(ctx, cfg.Token)

	// Read manifest to determine current version
	manifest, err := gh.ReadManifest(ctx, client, cfg.Owner, cfg.Repo, sourceRef)
	if err != nil {
		log.Fatalf("Failed to read manifest: %v", err)
	}

	currentVersion, ok := manifest["."]
	if !ok {
		log.Fatal("No root version found in manifest (expected '.' key)")
	}
	fmt.Printf("Current version in manifest: %s\n", currentVersion)

	// Calculate next minor version
	nextMinor, err := version.NextMinor(currentVersion)
	if err != nil {
		log.Fatalf("Failed to calculate next minor version: %v", err)
	}
	fmt.Printf("Next minor version: %s\n", nextMinor)

	branchName := fmt.Sprintf("release/v%s", nextMinor)
	fmt.Printf("Release branch: %s\n", branchName)

	// Check if branch already exists
	exists, err := gh.BranchExists(ctx, client, cfg.Owner, cfg.Repo, branchName)
	if err != nil {
		log.Fatalf("Failed to check if branch exists: %v", err)
	}
	if exists {
		log.Fatalf("Branch %s already exists", branchName)
	}

	if dryRun {
		fmt.Println("\n🏃 DRY RUN - No changes made")
		fmt.Printf("Would create branch: %s\n", branchName)
		fmt.Printf("From: %s\n", sourceRef)
		return
	}

	// Get the SHA of the source ref
	sourceSHA, err := gh.GetRefSHA(ctx, client, cfg.Owner, cfg.Repo, sourceRef)
	if err != nil {
		log.Fatalf("Failed to get SHA for %s: %v", sourceRef, err)
	}
	fmt.Printf("Source SHA: %s\n", sourceSHA)

	// Create the branch
	err = gh.CreateBranch(ctx, client, cfg.Owner, cfg.Repo, branchName, sourceSHA)
	if err != nil {
		log.Fatalf("Failed to create branch: %v", err)
	}

	fmt.Printf("✅ Created branch: %s\n", branchName)
	fmt.Printf("🔗 https://github.com/%s/%s/tree/%s\n", cfg.Owner, cfg.Repo, branchName)
}
