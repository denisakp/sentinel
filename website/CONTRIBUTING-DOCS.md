# Writing for the Sentinel documentation site

This is the authoring convention. Every page on the site follows it, so that ~88 pages written by
different people over five increments read as one document rather than eighty-eight.

Read this before writing your first page. If you are about to deviate from it, say why in the pull
request.

---

## Running the site

```bash
cd website
npm ci
npm start          # dev server, http://localhost:3000
```

Before opening a pull request:

```bash
npm run build      # the real gate — see below
npm run serve      # preview the production build, in one terminal
npm run a11y       # accessibility audit, in another
```

`npm run a11y` audits **every page in the sitemap**, so new pages are covered automatically — there
is no URL list to maintain. It needs `npm run serve` already running, because it audits the built
site rather than the source.

:::note
The audit rewrites the sitemap's production origin to `localhost` so it checks your local build and
not the deployed site. That rewrite is pinned to the GitHub Pages origin in the `a11y` script; it has
to be updated at the same time as `DEPLOY_TARGET` when the custom domain goes live.
:::

### `npm run build` is the test suite

The production build is not a formality. It enforces:

- **Broken internal links fail the build** (`onBrokenLinks: 'throw'`).
- **Broken heading anchors fail the build** (`onBrokenAnchors: 'throw'`). On a site this
  cross-linked, a link to a heading that has been renamed is the most likely form of rot.
- **Configuration errors fail the build**, because the config is TypeScript.

Two things exist only in the production build and cannot be checked with `npm start`:

- **Redirects** — stub pages are emitted at build time only.
- **The search index** — built from compiled output.

---

## The six sections

Each answers one reader question. Put your page where its question belongs.

| Section | The reader is asking |
|---|---|
| `intro/` | What is this, and how do I get started? |
| `concepts/` | What is X and why does Sentinel have it? |
| `guides/` | How do I accomplish X? |
| `tutorials/` | Teach me by doing, for my database engine. |
| `operations/` | Something is broken. What do I do? |
| `reference/` | What exactly does this flag/key do? |

A page belongs to exactly one section. If it seems to belong to two, it is probably two pages.

---

## Front matter

```yaml
---
title: Restoring a backup
description: How Sentinel restores an artifact, plans a chain, and what can go wrong.
sidebar_position: 2
---
```

| Field | Required | Rule |
|---|---|---|
| `title` | Yes | Sentence case. Unique across the site. Becomes the `<h1>` — **do not repeat it as a heading in the body**. |
| `description` | Yes | One sentence, ≤ 160 characters. Describes the page, not the product. |
| `sidebar_position` | Yes | Integer ordering within the directory. |
| `sidebar_label` | No | Only when `title` is too long for the sidebar. |

### Never set these

- **`slug`** — the file path *is* the URL, permanently. A slug override decouples the two and makes
  URL permanence impossible to audit by reading the directory tree.
- **`id`** — same reasoning.
- **`draft` / `unlisted`** — a page is either finished and present, or absent. There is no
  half-published state; navigation must never point at content that is not there.

---

## Every page ends with its sources

```markdown
<!-- sources: internal/domain/restore/executor.go, docs/runbooks/restore-from-backup.md -->
```

This is not bookkeeping. It is what makes the site auditable: a reviewer can go from any claim on
the page to the code that backs it.

- At least one path, repository-relative, and it must resolve.
- If the page documents CLI flags or YAML keys, include the file that defines them —
  `internal/cli/*.go` or `internal/config/types.go`.

---

## Accuracy: the rule that matters most

**Every command, flag, subcommand, and configuration key you write must exist in the codebase.**
No invented flags, no renamed commands, no aspirational YAML keys.

Pages are authored in parallel, often by people reading about a subsystem for the first time. That
is exactly the condition under which plausible-sounding flags get invented. Check before you write:

```bash
grep -rn "your-flag-name" internal/cli/
grep -n "your_yaml_key" internal/config/types.go
sentinel <command> --help
```

If you cannot find it, it does not exist. Do not document it.

---

## Universal rules

1. **No `<h1>` in the body.** The front-matter `title` supplies it.
2. **Heading levels never skip.** `##` → `###` → `####`. This is an accessibility requirement, not a
   style preference — screen reader users navigate by heading structure.
3. **Link text is descriptive.** Never "click here", "this page", or a bare URL. Someone browsing by
   link list must understand each destination out of context.
4. **Images carry alt text** describing what the image *conveys*, not what it depicts. Decorative
   images take `alt=""`.
5. **Never convey meaning by colour alone.** Pair it with a word, label, or icon.
6. **Every command is real and copy-pasteable.** No pseudo-syntax.
7. **No real credentials, ever.** Placeholders must be obviously fake, and must demonstrate
   Sentinel's supported patterns — environment variables, secrets files — never a password on a
   command line. Readers copy examples into production.
8. **Destructive steps carry a warning** and say how to verify or recover first:

   ```markdown
   :::danger Destructive
   This overwrites the target database. Confirm you have a verified backup with
   `sentinel backup verify <id>` before continuing.
   :::
   ```

9. **State version applicability** where a capability is not in every supported release:

   ```markdown
   :::info Added in v1.4.0
   At-rest encryption of secrets files requires Sentinel v1.4.0 or later.
   :::
   ```

---

## Page templates

### Concept pages — `concepts/`

Explain; do not walk through a task.

```markdown
[One paragraph answering "what is this". A reader who stops here still gains something.]

## Why it exists
[The problem this solves. What breaks without it.]

## How it works
[The mental model. Inputs, outputs, order of operations. Prose over bullet dumps.]

## Per-engine behaviour
[REQUIRED where behaviour differs across PostgreSQL / MySQL / MariaDB / MongoDB.
 Use a table. State engines with no support explicitly — never omit them silently.]

## Configuration
[The YAML keys that control it, with a real excerpt. Link to the configuration reference.]

## Example
[REQUIRED. A real command or config, and its observable result.]

## Failure modes
[What goes wrong, how it surfaces, where to go next.]

## Related
[REQUIRED. Links to the relevant guide, tutorial, and reference pages.]
```

### Guide pages — `guides/`

Task-oriented. Assumes the concept is understood.

```markdown
[One sentence: what this achieves.]

## When to use this
[REQUIRED. The situation that brings a reader here — and when NOT to use it.]

## Before you start
[REQUIRED. Preconditions: access, configuration, running services, prior state.]

## Steps
[Numbered. Each step: the command, then what the reader should observe.]

## Verify
[How to confirm it worked. A guide without verification is a guess.]

## If it goes wrong
[The two or three likeliest failures, or a link to the operations section.]

## Related
```

### Tutorial pages — `tutorials/`

Sequential, one engine, assumes nothing.

```markdown
[What the reader will have built by the end, and roughly how long it takes.]

## What you need
[REQUIRED. Include a reproducible throwaway instance — reference
 infra/docker/docker-compose.yml. Never assume a pre-existing environment.]

## Step N — [action]
[The command, then "You should see:" with real expected output.
 The reader must be able to confirm success before continuing.]

## What just happened
[Brief. Connects the mechanics back to the concept page.]

## Next
[The next page in this engine's track.]
```

State engine limitations where the reader meets them:

```markdown
:::note Not available on MongoDB
MongoDB recovery granularity is bounded by the oplog window rather than an arbitrary timestamp.
:::
```

### Operations pages — `operations/`

Written for someone under pressure. Most likely resolution first; no theory before action.

```markdown
[One sentence: the symptom this page addresses.]

## Symptoms
[How the reader knows they are in the right place — exact error strings where possible.]

## Before you start
[REQUIRED. Preconditions, and what to capture before changing anything.]

## Resolution
[Numbered steps. Destructive steps carry the danger admonition.]

## Verify recovery

## Prevent recurrence

## Related
```

### Reference pages — `reference/`

Exhaustive and scannable. No narrative.

```markdown
[One sentence on the command's purpose.]

## Synopsis

## Subcommands
[Table: name, purpose, link.]

## Flags
[Table: flag, type, default, description. Every flag that exists — no curation.]

## Examples

## Related
```

For the configuration reference: one table per YAML block, columns **Key / Type / Required /
Default / Description**, plus engine restrictions where they apply.

### Section index pages

Short. Orients and routes; does not duplicate the pages it links to.

---

## Moving a published page

Do not just rename the file:

1. Move it.
2. Add a row to `redirects.md`.
3. Add the matching entry to `redirects` in `docusaurus.config.ts`.
4. `npm run build` and confirm the old path still resolves.

---

## Two directories named `docs`

- **`website/docs/`** — the site's content. Pages go here.
- **`docs/runbooks/`** at the repository root — the preserved operator runbook corpus. **Not** part
  of the site build.

A site page derived from a runbook cites it in its sources comment. The runbook stays where it is.
