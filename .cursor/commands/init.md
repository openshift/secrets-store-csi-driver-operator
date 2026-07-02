---
name: /oape:init
id: oape-init
category: OAPE
description: Clone a Git repository by URL into the current directory and checkout the specified base branch
argument-hint: <git-url> <base-branch>
---

## Name
oape:init

## Synopsis
```shell
/oape:init <git-url> <base-branch>
```

## Description
The `oape:init` command clones an OpenShift operator git repository into the current working directory using the provided URL. It uses `git clone`, checks out the specified base branch, then changes into the cloned directory so that subsequent `/oape:*` commands work in the cloned directory.

**You MUST follow ALL steps strictly. If any precheck fails, you MUST stop immediately and report the failure.**

## Implementation

### Phase 0: Prechecks

All prechecks must pass before proceeding. If ANY precheck fails, STOP immediately and report the failure.

#### Precheck 1 — Validate Arguments

Both a git URL and a base branch MUST be provided.

```bash
# Parse arguments: expect "<git-url> <base-branch>"
GIT_URL=$(echo "$ARGUMENTS" | awk '{print $1}')
BASE_BRANCH=$(echo "$ARGUMENTS" | awk '{print $2}')

if [ -z "$GIT_URL" ] || [ -z "$BASE_BRANCH" ]; then
  echo "PRECHECK FAILED: Both a git URL and a base branch are required."
  echo "Usage: /oape:init <git-url> <base-branch>"
  echo ""
  echo "Example:"
  echo "  /oape:init https://github.com/openshift/cert-manager-operator main"
  exit 1
fi

echo "Git URL: $GIT_URL"
echo "Base branch: $BASE_BRANCH"
```

#### Precheck 2 — Verify Required Tools

```bash
MISSING_TOOLS=""

# Check git
if ! command -v git &> /dev/null; then
  MISSING_TOOLS="$MISSING_TOOLS git"
fi

if [ -n "$MISSING_TOOLS" ]; then
  echo "PRECHECK FAILED: Missing required tools:$MISSING_TOOLS"
  echo "Please install the missing tools and try again."
  exit 1
fi

echo "Required tools are available."
```

**If ALL prechecks above passed, proceed to Phase 1.**
**If ANY precheck FAILED (exit 1), STOP. Do NOT proceed further. Report the failure to the user.**

---

### Phase 1: Derive Clone Directory

Extract the repository name from the git URL to use as the clone directory name.

```bash
# Extract repo name from URL (strip trailing .git and take the last path component)
CLONE_DIR=$(basename "$GIT_URL" .git)
CLONE_URL="$GIT_URL"

echo "Clone directory: $CLONE_DIR"
echo "Clone URL: $CLONE_URL"
```

---

### Phase 2: Clone Repository

Clone the repository into the current working directory using `git clone --filter=blob:none`. Handle the case where the target directory already exists.

```bash
if [ -d "$CLONE_DIR" ]; then
  echo "Directory '$CLONE_DIR' already exists."

  # Check if it is a git repo pointing to the same remote
  EXISTING_REMOTE=$(git -C "$CLONE_DIR" remote get-url origin 2>/dev/null || true)

  if [ -n "$EXISTING_REMOTE" ]; then
    # Normalize URLs for comparison (strip trailing slashes and .git suffix)
    NORM_EXISTING=$(echo "$EXISTING_REMOTE" | sed 's/\.git$//' | sed 's:/$::')
    NORM_CLONE=$(echo "$CLONE_URL" | sed 's/\.git$//' | sed 's:/$::')

    if [ "$NORM_EXISTING" = "$NORM_CLONE" ]; then
      echo "Existing directory is already a clone of the same repository."
      echo "Using existing directory as-is."
    else
      echo "FAILED: Directory '$CLONE_DIR' exists but points to a different remote."
      echo "  Expected: $CLONE_URL"
      echo "  Found:    $EXISTING_REMOTE"
      echo ""
      echo "Options:"
      echo "  1. Remove the directory manually: rm -rf $CLONE_DIR"
      echo "  2. Use a different working directory"
      exit 1
    fi
  else
    echo "FAILED: Directory '$CLONE_DIR' exists but is not a git repository."
    echo ""
    echo "Options:"
    echo "  1. Remove the directory manually: rm -rf $CLONE_DIR"
    echo "  2. Use a different working directory"
    exit 1
  fi
else
  echo "Cloning $CLONE_URL into $CLONE_DIR..."
  git clone --filter=blob:none "$CLONE_URL"

  if [ $? -ne 0 ]; then
    echo "FAILED: git clone failed."
    echo "Check your network connection and repository access."
    exit 1
  fi

  echo "Clone complete."
fi
```

---

### Phase 3: Change Directory, Checkout Base Branch, and Verify

Change into the cloned repository directory, checkout the specified base branch, and verify it is a valid Go-based operator repository.

```bash
cd "$CLONE_DIR" || { echo "FAILED: Cannot change to directory $CLONE_DIR"; exit 1; }

# Checkout the specified base branch
git checkout "$BASE_BRANCH" || { echo "FAILED: Cannot checkout branch '$BASE_BRANCH'"; exit 1; }
echo "Checked out branch: $BASE_BRANCH"

# Verify Go module
if [ -f "go.mod" ]; then
  GO_MODULE=$(head -1 go.mod | awk '{print $2}')
  echo "Go module: $GO_MODULE"
else
  echo "WARNING: No go.mod found. This may not be a Go-based operator repository."
  GO_MODULE="(not detected)"
fi

# Detect operator framework
FRAMEWORK="unknown"
if [ -f "go.mod" ]; then
  if grep -q "sigs.k8s.io/controller-runtime" go.mod 2>/dev/null; then
    FRAMEWORK="controller-runtime"
  elif grep -q "github.com/openshift/library-go" go.mod 2>/dev/null; then
    FRAMEWORK="library-go"
  fi
fi

echo "Framework: $FRAMEWORK"
echo "Current directory: $(pwd)"
```

---

### Phase 4: Output Summary

```text
=== Repository Init Summary ===

Repository:  <clone-dir>
Clone URL:   <clone-url>
Base Branch: <base-branch>
Local Path:  <absolute-path-to-cloned-dir>
Go Module:   <module-name>
Framework:   <controller-runtime | library-go | unknown>
```

---

## Critical Failure Conditions

The command MUST FAIL and STOP immediately if ANY of the following are true:

1. **Missing arguments**: Git URL or base branch was not provided
2. **Missing tools**: `git` is not installed
3. **Clone failed**: The git clone command fails (network, permissions, etc.)
4. **Branch checkout failed**: The specified base branch does not exist in the repository
5. **Directory conflict**: The target directory exists but is not a clone of the expected repository

When failing, provide a clear error message explaining:
- Which check failed
- What the expected state is
- How to fix the issue

## Behavioral Rules

1. **Efficient cloning**: Always use `git clone --filter=blob:none` for blobless clones.
2. **Non-destructive**: Never delete an existing directory automatically.
3. **Idempotent**: If the directory already exists and is a clone of the correct repository, use it as-is without re-cloning.

## Arguments

- `<git-url>`: The full Git clone URL of the repository
  - Required argument
  - Example: `https://github.com/openshift/cert-manager-operator`
- `<base-branch>`: The base branch to checkout after cloning
  - Required argument
  - Example: `main`, `master`, `release-4.18`

## Prerequisites

- **git** -- Git installed
- Access to the target Git repository
