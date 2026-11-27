# OVPN GitHub workflow

This workflow applies to Core, Android, Desk, iOS, and future OVPN repositories. Use English in GitHub records and shared handoffs.

## Start with the issue

Describe the user or operator problem, the expected outcome, and how it will be accepted. Add relevant links and one small, sanitized example when it helps a reader understand the task. Use the security reporting channel for sensitive findings.

Assign one GitHub owner when the issue becomes active. Add a label for the type of work and a milestone for the intended acceptance or release. Milestones are per repository. Use the private OVPN Delivery project to see work across repositories. Its statuses are Backlog, In progress, In review, Validation, Paused, and Done. When handing off an issue, update the assignee and the project status, then leave a short note explaining the next action.

## Open and review the PR

- Link the issue with `Refs #55` or `Refs owner/repository#55` for ongoing work. Use `Closes` when the PR satisfies the entire issue.
- Explain the change, the decision behind it, and who is affected. Add a short example, screenshot, or diagram where it helps. Record the exact revision, tests, and outcomes. For a build, link to its run or artifact and give the version and SHA-256 when available.
- Assign the PR and request review in GitHub. Request `alex5416` for PRs from `agentram`, and `agentram` for PRs from `alex5416`. Keep CODEOWNERS aligned with repository ownership.
- Put actionable findings on the relevant lines of the diff. Submit a review with a verdict and any outstanding checks. Reply with the fix or decision before resolving a thread. Keep threads that still need evidence open.
- Keep VERSION, the README version, and the first numbered changelog entry aligned. Follow the target repository's release rules and record any deferred validation with its owner.

CI, local tests, emulator tests, and physical-device tests are different sources of evidence. Record what ran and where. If an artifact is pending, write that in the PR and add the link after it is available. Use only sanitized, publicly shareable evidence in public issues and PRs. Store private artifacts and internal details in an access-controlled private repository or project. Use synthetic configuration and inspect packages, examples, and screenshots before sharing.

## Hand off a stage

Use one active executor and one heavy run by default. Record the repository and issue, role, exact SHA or build, environment and result, completion criterion, and next owner and action. Check the executor's latest status before assigning the work elsewhere. When ownership changes, make that explicit.

| Role | Result to record |
| --- | --- |
| Developer | Implementation, commit, affected tests, and any migration or compatibility change |
| QA | Scenario, environment, expected and actual behavior, and a pass, fail, or blocked verdict |
| Reviewer | Diff findings, decisions, and outstanding evidence |
| DevOps | Exact build, workflow, artifact link, checksum, and verification outcome |
| Project Manager | Owner, milestone, current status, milestone evidence, and next handoff |

## Set up a new OVPN repository

1. Add the bug and feature forms, PR template, and a contributor link to this guide. Set CODEOWNERS to the repository's actual maintainers and confirm review-request access.
2. Add repository labels and milestones for the work. Link the repository to the private OVPN Delivery project and use the shared statuses.
3. Document the repository's checks, package and version rules, and artifact locations. Test that an issue can be assigned, a PR can request review and reference it, and readers can access the evidence intended for them.
