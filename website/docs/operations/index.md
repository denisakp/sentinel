---
title: Operations
description: "Incident procedures for when something has already gone wrong, and the maintenance tasks that keep it from going wrong again."
sidebar_position: 1
---

These pages are written for someone whose backups are already broken. Each one leads with symptoms,
so you can tell quickly whether you are in the right place, then with the most likely resolution
before any explanation.

If nothing is on fire, the [guides](../guides/index.md) cover routine work, and the
[concepts](../concepts/index.md) explain the machinery these procedures act on.

## Something is broken

| Page | Symptoms |
|---|---|
| [Failed backup triage](./failed-backup-triage.md) | A backup job reported failure and you need to work out why. |
| [Stale lock recovery](./stale-lock-recovery.md) | A job will not start, or reports that another run holds its lock. |
| [Scheduler crash recovery](./scheduler-crash-recovery.md) | The scheduler died, and you need to know what ran, what did not, and what state it left. |
| [Repository state repair](./state-repair.md) | History rows, manifests and artifacts disagree with each other. |
| [Chain corruption recovery](./chain-corruption-recovery.md) | An incremental chain will not validate, or a baseline is missing. |
| [Troubleshooting](./troubleshooting.md) | Symptoms that do not fit the pages above. |

## Something is at risk

| Page | Situation |
|---|---|
| [Key loss incident](./key-loss-incident.md) | An encryption key may have been lost or compromised. |
| [Recover a legacy envelope](./recover-legacy-envelope.md) | An artifact predates envelope v2 and is refused by default. |
| [Verify release artifacts](./verify-release-artifacts.md) | A signature or provenance check on a Sentinel release failed, and you need to decide what that means. |

## Read this before you rely on any of these

Porting these procedures from the repository's runbooks meant checking each step against the code.
That check found that several recovery mechanisms these procedures describe **do not exist in the
current release**. The pages say so where you would meet the problem, but the two worth knowing before
an incident are:

**A passing `sentinel backup verify` does not mean an encrypted backup is restorable.** Verification
hashes the stored bytes and never decrypts, so an artifact encrypted under a key you no longer hold
passes ([#164](https://github.com/denisakp/sentinel/issues/164)). The only real test is an actual
restore. The repository's key-loss runbook says otherwise, which is why
[the page here](./key-loss-incident.md) contradicts it.

**Stale locks are not reaped when the scheduler starts.** Two runbooks describe an automatic sweep at
startup. The function exists and has no caller
([#142](https://github.com/denisakp/sentinel/issues/142)), so recovery is manual today.

Where a procedure depends on something broken, the page names the open issue rather than describing a
step that cannot be followed. That is deliberate: a recovery procedure that fails halfway is worse
than one that tells you the truth up front.

## The runbooks these came from

The [runbooks in the repository](https://github.com/denisakp/sentinel/tree/develop/docs/runbooks)
remain as source material. They are no longer maintained in parallel, and fifteen of them were found
to describe behaviour the code does not have. Where the two disagree, these pages are the ones checked
against the code.

<!-- sources: docs/runbooks/, internal/adapters/lock/, internal/scheduler/, internal/cli/repair.go, internal/cli/backup_verify.go -->
