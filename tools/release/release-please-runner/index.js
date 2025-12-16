#!/usr/bin/env node

/**
 * Based on https://github.com/googleapis/release-please-action/blob/main/src/index.ts
 * Adapted for CLI usage with custom versioning strategy.
 */

import fs from 'fs';
import path from 'path';
import { GitHub, Manifest, VERSION } from 'release-please';
import { registerVersioningStrategy } from 'release-please/build/src/factories/versioning-strategy-factory.js';
import { MinorBreakingVersioningStrategy } from './minor-breaking-versioning.js';

// Register the custom versioning strategy
registerVersioningStrategy('minor-breaking', (options) => new MinorBreakingVersioningStrategy(options));

const DEFAULT_CONFIG_FILE = 'release-please-config.json';
const DEFAULT_MANIFEST_FILE = '.release-please-manifest.json';

function parseInputs() {
  const token = process.env.GITHUB_TOKEN;
  if (!token) {
    throw new Error('GITHUB_TOKEN environment variable is required');
  }

  const repoUrl = process.env.REPO_URL || process.env.GITHUB_REPOSITORY || '';
  if (!repoUrl) {
    throw new Error('REPO_URL or GITHUB_REPOSITORY environment variable is required');
  }

  return {
    token,
    repoUrl,
    targetBranch: process.env.TARGET_BRANCH || undefined,
    configFile: process.env.CONFIG_FILE || DEFAULT_CONFIG_FILE,
    manifestFile: process.env.MANIFEST_FILE || DEFAULT_MANIFEST_FILE,
    skipGitHubRelease: process.env.SKIP_GITHUB_RELEASE === 'true',
    skipGitHubPullRequest: process.env.SKIP_GITHUB_PULL_REQUEST === 'true',
    pullRequestTitlePattern: process.env.PULL_REQUEST_TITLE_PATTERN || undefined,
    pullRequestHeader: process.env.PULL_REQUEST_HEADER || undefined,
  };
}

function generateConfigFile(inputs) {
  const tempConfigFileName = '.release-please-config.tmp.json';
  const repoRoot = path.resolve(process.cwd(), '..', '..', '..');
  const config = JSON.parse(fs.readFileSync(path.join(repoRoot, inputs.configFile), 'utf8'));

  if (inputs.pullRequestTitlePattern) {
    config['pull-request-title-pattern'] = inputs.pullRequestTitlePattern;
  }
  if (inputs.pullRequestHeader) {
    config['pull-request-header'] = inputs.pullRequestHeader;
  }

  const outputPath = path.join(process.cwd(), tempConfigFileName);

  fs.writeFileSync(outputPath, JSON.stringify(config, null, 2));

  return outputPath;
}

function loadManifest(github, inputs, configFile) {
  console.log('Loading manifest from config file');

  return Manifest.fromManifest(
    github,
    inputs.targetBranch || github.repository.defaultBranch,
    configFile,
    inputs.manifestFile
  );
}

async function main() {
  console.log(`Running release-please version: ${VERSION}`);
  const inputs = parseInputs();
  const github = await getGitHubInstance(inputs);
  const configFilePath = generateConfigFile(inputs);

  if (!inputs.skipGitHubRelease) {
    const manifest = await loadManifest(github, inputs, configFilePath);
    console.log('Creating releases');
    outputReleases(await manifest.createReleases());
  }

  if (!inputs.skipGitHubPullRequest) {
    const manifest = await loadManifest(github, inputs, configFilePath);
    console.log('Creating pull requests');
    outputPRs(await manifest.createPullRequests());
  }
}

function getGitHubInstance(inputs) {
  const [owner, repo] = inputs.repoUrl.split('/');
  return GitHub.create({
    owner,
    repo,
    token: inputs.token,
    defaultBranch: inputs.targetBranch,
  });
}

function outputReleases(releases) {
  releases = releases.filter(release => release !== undefined);
  const pathsReleased = [];
  console.log(`releases_created=${releases.length > 0}`);
  if (releases.length) {
    for (const release of releases) {
      if (!release) {
        continue;
      }
      const path = release.path || '.';
      if (path) {
        pathsReleased.push(path);
      }
      console.log(`Created release: ${release.tagName}`);
      for (const [rawKey, value] of Object.entries(release)) {
        let key = rawKey;
        if (key === 'tagName') key = 'tag_name';
        if (key === 'uploadUrl') key = 'upload_url';
        if (key === 'notes') key = 'body';
        if (key === 'url') key = 'html_url';
        console.log(`  ${key}=${value}`);
      }
    }
  }
  console.log(`paths_released=${JSON.stringify(pathsReleased)}`);
}

function outputPRs(prs) {
  prs = prs.filter(pr => pr !== undefined);
  console.log(`prs_created=${prs.length > 0}`);
  if (prs.length) {
    for (const pr of prs) {
      console.log(`Created/updated PR #${pr.number}: ${pr.title}`);
    }
  }
}

main().catch(err => {
  console.error(`release-please failed: ${err.message}`);
  process.exit(1);
});
