---
name: review-phase-pr
description: Work through review feedback on an open phase PR -- GitHub Copilot's automated review plus the user's own comments -- explain concepts, make code changes, answer questions, then update README.md with the phase's summary. Use once a phase's PR (opened via ship-phase) has comments waiting to be addressed.
disable-model-invocation: true
---

# Review and close out a phase PR

Run this after `ship-phase` has opened a PR and it has review comments to address, from GitHub Copilot's automated review, the user, or both.

## 1. Gather the feedback

- Identify the open PR for the current branch: `gh pr view --json url,number,headRefName`.
- Pull the full picture: `gh pr view <N> --comments` for top-level thread comments, and `gh api repos/{owner}/{repo}/pulls/<N>/comments` for inline code-review comments (this is usually where Copilot's line-level suggestions land, not the top-level thread).
- Read the full PR diff (`gh pr diff <N>`) alongside the comments so each one is understood in the context of the actual change, not in isolation.

## 2. Work through each comment

For every Copilot or user comment:

- If it's a question about code or a concept (not a requested change): answer it directly and thoroughly. This is the same kind of teaching opportunity as the rest of this project (per `CLAUDE.md`) -- first-principles, no analogies, explain the actual mechanism, not just the high-level idea.
- If it's a valid requested change: make the code change, and explain *why* it's correct and what concept it touches while making it -- don't silently patch and move on.
- If a suggestion is not applicable or would be wrong to apply: say so explicitly (as a PR reply where appropriate) and explain why, rather than applying it just because it was suggested. Automated review tools produce false positives; treat Copilot's suggestions as input to evaluate, not instructions to execute.
- After any code change, re-run `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .`.

## 3. Commit and push follow-ups

- Stage and commit the review-driven changes as a new commit on the phase branch (never amend a commit that's already part of an open PR someone may have looked at).
- Push -- this updates the existing PR automatically. Report back what changed and which comments each change addresses.

## 4. Update README.md

Once the user confirms the PR's feedback is addressed and it's ready to merge (or has merged):

- Add a phase entry to `README.md` summarizing what was built, the key System Design concept(s) learned, and what testing covered/found -- a few sentences, written for someone skimming the repo (e.g. an interviewer), not a copy of the full notes file. Link to `notes/phase-N-short-description.md` for the complete write-up.
- Leave README.md's existing introductory content untouched. The phase entries should read as a running, growing log across phases, in the order the phases happened.

Do not merge the PR as part of this skill -- merging is a separate, explicit decision the user makes on GitHub (or asks for by name).
