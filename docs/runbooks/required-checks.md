# Making the test suite enforceable: required checks on the integration branches

**Status**: specification awaiting a maintainer decision. Nothing here has been applied.
**Owner**: repository maintainer, since applying it needs admin rights on the repository.
**Origin**: spec 061, FR-007a and FR-007b. Root cause issue #175.

## Why this exists

Spec 061 added three gates to the test suite: the integration tests are now compiled everywhere and
run where the environment allows, an unrecorded skip fails the suite, and a configuration key that
nothing reads fails the census.

**All three are currently advisory.** Neither integration branch carries branch protection, so no
check is required anywhere, and nothing prevents a merge that ignores a red result. Verified on
2026-09-09:

```console
$ gh api repos/denisakp/sentinel/branches/develop/protection
{"message":"Branch not protected", ... "status":"404"}
```

A gate nobody is obliged to satisfy is a suggestion. Seventy-three defects reached production against
a suite that was reporting success; adding better gates without making them binding repeats the shape
of that failure with more machinery.

## What the split is, and why it needs enforcing

`scripts/e2e.sh` cannot run the integration tests on every machine. They invoke `pg_dump`,
`mysqldump`, `mariadb-dump` and `mongodump`, and a developer host frequently has none of them. The
suite therefore compiles those tests locally, runs them only when the binaries are present, and says
plainly when it declines.

The environment that always runs them is the `Integration Tests` workflow, which installs all four
client binaries explicitly before running. That workflow is consequently the only place the
integration suite is guaranteed to execute, which is exactly why it has to be a required check rather
than an informational one.

## The settings to apply

Both branches, `develop` and `1.x`.

### Required status checks

Use these context names exactly as they appear in the API. They are the job names, not the workflow
names, and the two differ:

| Context | Workflow | Why required |
|---|---|---|
| `integration` | Integration Tests | The only environment guaranteed to run the integration suite |
| `lint` | Lint | Enforces the ADR 0001 layering rules through depguard |

`GitGuardian Security Checks` also reports on pull requests. It is an external app rather than a
workflow job; require it only if the maintainer wants secret scanning to be blocking, which is a
separate decision from this one.

`Documentation site` runs on `1.x` and on pull requests. Requiring it on `1.x` is reasonable, since a
broken link fails that build, but it is outside the scope of this runbook.

Enable **strict** mode (branches must be up to date before merging) only if you accept the extra
rebase traffic. It is not needed for the guarantee this runbook is after.

### Applying it

Through the web interface, Settings then Branches then Add branch protection rule, or:

```bash
gh api -X PUT repos/denisakp/sentinel/branches/develop/protection \
  --input - <<'JSON'
{
  "required_status_checks": { "strict": false, "contexts": ["integration", "lint"] },
  "enforce_admins": false,
  "required_pull_request_reviews": null,
  "restrictions": null
}
JSON
```

Repeat for `1.x`.

`required_pull_request_reviews` is left null deliberately. This repository is maintained by one
person, and requiring a review from someone else would block every merge. `enforce_admins` is left
false so the maintainer retains an escape hatch; setting it true is a stricter posture and a separate
call.

## What this changes for the maintainer

Today a branch can be pushed to directly and a pull request merged whatever the checks say. After
applying this, a merge waits for `integration` and `lint` to pass. That is the intended cost.

The most likely irritation is a pull request blocked by an integration failure unrelated to its
change. If that happens repeatedly, the answer is to fix the flaky test, not to remove the
requirement. Removing it returns the repository to the state that produced this defect batch.

## Maintainer decision

> **Not yet decided.**
>
> Spec 061 delivered the specification above and deliberately did not apply it. Implementation must
> not change repository settings on its own: this is a product and workflow call with a real cost,
> and the person who pays that cost should make it.
>
> Record the outcome here either way, including a decision not to apply it and the reasoning. A
> declined decision that is written down is a position; one that is merely never taken is a gap
> nobody can see.

| Date | Decision | By | Notes |
|---|---|---|---|
| | | | |
