# Repository workflow

These instructions apply to every future code or documentation change.

## Protected branch

- `main` is protected. Never force-push or delete it.
- Do not push feature commits directly to `main`.
- Use a short-lived branch and Pull Request for every change.

## Required delivery sequence

1. Synchronize before starting:

   ```bash
   git switch main
   git pull --ff-only origin main
   git switch -c <type>/<short-description>
   ```

2. Implement and verify the change. Run the checks appropriate to the risk, including `make check` and `make test-race` for Go changes.
3. Confirm no secrets or environment-specific personal data were added.
4. Commit and push the branch:

   ```bash
   git add -A
   git diff --cached --check
   git commit -m "<imperative summary>"
   git push -u origin HEAD
   ```

5. Open a Pull Request targeting `main`.
6. Wait for required CI and CodeQL checks to pass.
7. Resolve review conversations, merge the Pull Request, and delete the remote branch.
8. Synchronize the local repository and remove the temporary branch:

   ```bash
   git switch main
   git pull --ff-only origin main
   git branch -d <type>/<short-description>
   ```

9. Confirm final synchronization before reporting completion:

   ```bash
   git status --short --branch
   test "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)"
   ```

## Non-negotiable checks

- Never commit `.env`, master keys, SSH private keys, database dumps, VM disks, session cookies, tokens, initial/reset passwords, or machine-specific deployment secrets.
- Preserve third-party license and notice files.
- Keep the final local `main` identical to `origin/main` after every completed change.
