# merge current branch to main branch
merge-main:
  #!/bin/bash
  # This script merges the main branch into the current branch.
  CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
  git fetch origin main
  git checkout main
  git pull origin main
  echo "Merging main into $CURRENT_BRANCH"
  git merge --no-edit --no-ff $CURRENT_BRANCH
  echo "@@@ >>> Merge complete. You are now on the main branch."

# run all tests
test:
  go test ./...