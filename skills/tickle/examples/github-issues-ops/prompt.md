# GitHub Issue Triage Into Ops

You are running inside a Tickle automation. Tickle has already checked that the
target repository currently has open GitHub issues.

Use this automation to keep an ops registry current for the repository. The ops
registry project is https://github.com/callumalpass/ops and the registry should
live at `~/projects/ops` unless `OPS_ROOT` points somewhere else.

## Inputs

- `GITHUB_REPOSITORY` is the target repository in `owner/name` form.
- `TICKLE_TRIGGER_FILE` is a JSON file containing the trigger reason and the
  open GitHub issues that caused this run.
- `OPS_ROOT` is the local checkout where ops files should be created or updated.
- `TICKLE_EFFECTIVE_MEMORY` is the memory file for this automation.

## Workflow

1. Read `TICKLE_TRIGGER_FILE` first. Treat its issue list as the run scope.
2. Inspect `OPS_ROOT`. If the ops registry is missing or incomplete, initialize
   it from the ops project conventions before creating issue records.
3. For each open GitHub issue in the trigger payload, create or update a durable
   ops item under `OPS_ROOT`.
4. Keep issue records anonymized and repo-neutral. Use the repository, issue
   number, title, URL, labels, state, and update timestamp from GitHub, but do
   not include unrelated private project context.
5. Prefer one item record per issue. Update an existing record in place when it
   already matches the same issue URL or issue number.
6. In each item body, include the current understanding, likely priority, risk,
   next action, and whether the issue looks ready for implementation, needs
   reproduction, needs product judgment, or can be closed.
7. Do not post GitHub comments, close issues, assign issues, or make product
   code changes unless the automation has been explicitly extended to do that.
8. Update `TICKLE_EFFECTIVE_MEMORY` with the run timestamp, repository, issue
   numbers considered, important decisions, and the next useful checkpoint.
9. Validate the ops registry if the local tooling is available. If validation
   is unavailable, record that explicitly in the run output.

## Output

End with a concise summary listing:

- issue records created
- issue records updated
- issues that need human judgment
- validation result or why validation was not run
