---
name: ship-phase
description: Sanity-check for leaked secrets/PII, write the phase notes if missing, then commit and open a PR from the current phase branch into build. Use once a phase's implementation and tests are done and the user is ready to ship it.
disable-model-invocation: true
---

# Ship a phase: sanity check, commit, PR into build

Run this once a phase's implementation and tests are complete and the user says it's ready to ship. Follows the phase workflow in this repo's `CLAUDE.md` -- this skill exists so that workflow is actually followed every time, not just documented.

## 1. Confirm branch state

Run `git branch --show-current`.

- If it's `build` or `master`: the phase's work landed directly on the wrong branch. Create `phase-N-short-description` (check `git branch -a` / prior `notes/phase-*.md` files for the next N) from the current position and switch to it with `git checkout -b`. Uncommitted changes travel with you automatically -- do not stash, commit-then-cherry-pick, or otherwise route around this.
- If already on a `phase-N-*` branch, continue.

## 2. Confirm the phase is actually done

- Run `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .`. All must be clean. If anything fails, stop and fix it -- don't ship broken or unformatted code.
- Check whether `notes/phase-N-short-description.md` exists. If it doesn't, write it now using `notes/TEMPLATE.md`'s structure, drawing on the actual conversation: what was built and what was deliberately left out, the System Design concepts/trade-offs and why a given option was chosen, a curated (not raw-transcript) narrative of the design discussion, a summary of code changes, and testing results including weaknesses found and what's deferred. This file is a required artifact of every phase, not optional polish.

## 3. Sanity check for leaked credentials / personal info

- Review everything about to be committed: `git status`, `git diff` (staged and unstaged), and a full read of any new/untracked files (exported prompt transcripts, notes, etc. have no diff to grep, so open and read them directly).
- Grep the full changeset for common leak patterns: cloud/API credentials (`AKIA[0-9A-Z]{16}`, `sk-`, `ghp_`, `xox[baprs]-`), private key headers (`BEGIN .*PRIVATE KEY`), `.env`-style `KEY=value` secrets, generic high-entropy 32+ char tokens, AWS account IDs / ARNs, internal hostnames.
- Grep separately for email addresses and phone-number-like patterns, and check whether any hit is the user's own info (already known to the assistant) vs. a third party's.
- If anything is found: stop and flag it to the user with the specific file/line before staging or committing anything. Do not decide unilaterally to redact, exclude, or proceed -- this is the user's call (redact and commit, commit as-is, or exclude the file this round). If a hit is in a file that's already merged/pushed in an earlier commit, say so explicitly and do not attempt a history rewrite (`git filter-repo`, `rebase -i`, force-push) without the user explicitly asking for that separately -- it's a much bigger, riskier operation than this skill covers.

## 4. Stage and commit

- Stage specific files by name (never blanket `git add -A` or `git add .`); run `git status` after staging to confirm nothing unexpected got included.
- Write a commit message describing the phase's increment (what/why), matching this repo's existing commit style (check `git log`).
- Create a new commit -- never amend.

## 5. Push and open the PR

- Push the phase branch (`-u` if it has no upstream yet).
- Open a PR into `build` (not `master`): `gh pr create --base build`. The description should cover what/why/tests/known weaknesses, mirroring the notes file, and link `notes/phase-N-short-description.md`.
- Report the PR URL back to the user.

Step 3's sanity check is a hard gate, not a formality -- the user invoking this skill is authorization to commit and open the PR, but not authorization to skip checking what's actually in that commit first.
