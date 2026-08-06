---
title: When release verification fails
description: What a failed cosign, SLSA, or checksum check means, how to tell tampering from a missing asset, and what to do next.
sidebar_position: 10
---

A signature, provenance, or checksum check on a downloaded Sentinel release did not pass, and you
need to decide whether that is a supply-chain signal or a download problem.

## Symptoms

The checksum comparison reports a mismatch:

```
sentinel-1.4.0-linux-amd64.tar.gz: FAILED
sha256sum: WARNING: 1 computed checksum did NOT match
```

Or it reports that no file was verified, which means the filename you downloaded does not appear in
`checksums.txt` at all rather than that it failed. With `--ignore-missing`, an archive that was never
downloaded produces exactly this.

`cosign verify-blob` exits non-zero instead of printing `Verified OK`, typically reporting either
that no signature matched or that the certificate identity or OIDC issuer did not match what you
asked for.

`slsa-verifier verify-artifact` exits non-zero instead of reporting a passing verification, typically
because the artifact hash is not among the attested subjects, or the source repository or tag does
not match.

A tool reports that a file is not valid JSON, or a downloaded file is far smaller or larger than
expected. This is usually the real symptom underneath all of the above: the file on disk is a GitHub
error page, not the asset. `curl -LO` follows redirects but does **not** fail on an HTTP error
status, so a request for an asset that does not exist writes the 404 page into the file and exits
zero.

:::note The happy path lives elsewhere
The full cosign and SLSA procedures, with the exact commands and identity regexp, are in
[Installation](../intro/installation.md). This page is for when one of them fails. If you have not
run the documented sequence end to end in a clean directory, do that first.
:::

## Before you start

Do not install or run the binary while this is unresolved.

Work in a new, empty directory. Files left over from an earlier attempt are the most common cause of
a verification that fails for no security reason.

Capture, before re-downloading anything:

```bash
ls -l                       # sizes of what you already have
sha256sum ./*               # what you actually hold
cosign version              # cosign v3 or later is required
```

Record the exact release tag and the exact URLs you used. Most of the analysis below turns on
whether the URL you fetched pointed at the release you think it did.

## Resolution

### 1. Rule out a partial or non-existent download

Start here. It resolves most of these incidents.

```bash
mkdir sentinel-verify && cd sentinel-verify
BASE=https://github.com/denisakp/sentinel/releases/download/v1.4.0

curl -fLO "$BASE/checksums.txt"
curl -fLO "$BASE/checksums.txt.sigstore.json"
```

The `-f` is the point: it makes curl fail with a non-zero exit on an HTTP error instead of saving the
error page under the asset's name. Then confirm each file is what it claims to be. `checksums.txt` is
plain text, one `<sha256>  <filename>` line per asset, and the bundle is JSON:

```bash
head -c 80 checksums.txt
jq empty checksums.txt.sigstore.json && echo "bundle parses"
```

If either check surprises you, the download was the problem and the verification result meant
nothing.

### 2. Confirm you are verifying the release you think you are

The `releases/latest/download` base URL always resolves to the newest release, whatever version
number you typed into the filename. Once `latest` moves past that version, the archive URL 404s while
`checksums.txt` downloads fine from the new release, so the checksum step reports that nothing was
verified and the provenance step fails on a tag mismatch. Neither is tampering.

Pin the tag, as in the `BASE` above, and make sure `slsa-verifier --source-tag` names the same tag
the archive came from.

### 3. Work out which check failed and what it could prove

| Check | What it proves | What a failure most often means |
|---|---|---|
| `sha256sum -c checksums.txt` | Your archive matches the published list | Partial or wrong download, or the archive was altered |
| `cosign verify-blob` on `checksums.txt` | The list came from Sentinel's release workflow on a tag | Wrong identity regexp or issuer, wrong bundle, or an altered `checksums.txt` |
| `slsa-verifier verify-artifact` | The archive was built by the declared workflow from the declared source | Wrong `--source-uri` or `--source-tag`, or an archive not covered by the attestation |

Three structural facts change how you read those results.

**The signature covers `checksums.txt` and nothing else.** The GoReleaser `signs:` block is scoped to
`artifacts: checksum`. One signature covers every archive only *transitively*, through the checksum
list. A `Verified OK` from cosign therefore says nothing about your `.tar.gz` until you also run
`sha256sum -c checksums.txt --ignore-missing`. If someone concludes "cosign passed, so the download
is fine", that is the gap.

**The identity regexp is pinned to the workflow file path.** It matches
`.../.github/workflows/release.yml@refs/tags/.*` on the `token.actions.githubusercontent.com` issuer.
A regexp copied from an older document, or a workflow file that has since been renamed, makes cosign
reject a perfectly legitimate signature.

**The two checks are genuinely independent.** Provenance is generated by a separate CI job with its
own OIDC identity, not by the job that built the binaries, so a compromise of the build job alone
cannot forge it. The attested subject list is taken verbatim from the same `checksums.txt` that was
signed, so the attested, checksummed, and signed sets are identical by construction rather than by
convention. That is why a disagreement between the two checks is worth taking seriously.

### 4. Separate a missing asset from a present but invalid one

This is the distinction the whole page turns on. Verification is fail-**open** for an absent asset
and fail-**closed** for a present one.

| Situation | Reading |
|---|---|
| No `checksums.txt.sigstore.json` on the release | Expected before v1.4.0. Use the checksum-only path. |
| No `multiple.intoto.jsonl` on the release | Expected before v1.4.0. Skip the provenance check. |
| No archives or `checksums.txt` at all | Expected before v1.3.0, when prebuilt binaries were introduced. |
| A local GoReleaser snapshot with no bundle | Expected. `goreleaser --snapshot` skips signing entirely. |
| The asset is present and the check fails | Hard failure. Treat it as an incident. |

Cosign signing and SLSA provenance both shipped in v1.4.0. A missing asset on an older release is
history, not a signal.

The reverse is also informative. The release workflow signs with no `continue-on-error` and then
verifies its own bundle before finishing, so a v1.4.0-or-later release that published at all is one
whose `checksums.txt` was signed and self-checked. A missing bundle on a recent release is itself
anomalous and worth reporting.

### 5. If the asset is present and verification still fails

:::danger Do not install the binary
A present but invalid signature, attestation, or checksum is the exact condition these checks exist
to catch. Do not extract, run, or install the artifact, and do not work around the failure with
`--insecure`-style flags or by skipping a step.
:::

Re-download once from a different network and, if you can, a different host, into a fresh empty
directory using the pinned-tag `BASE` and `curl -fLO`. A local proxy, a corporate TLS-inspecting
middlebox, or a caching CDN edge is a plausible and non-malicious cause of a corrupted asset, and a
second path distinguishes that from tampering.

If it fails again from a clean path, stop. Keep the downloaded files and your captured hashes for
analysis, do not delete them, and report it at
[the Sentinel issue tracker](https://github.com/denisakp/sentinel/issues) with the release tag, the
URLs, and the exact tool output.

In the meantime, build from source or use `go install`, both covered in
[Installation](../intro/installation.md). Note that a `go install` build reports
`dev / unknown / unknown` from `sentinel version`, because it is compiled without the release build
flags.

## Verify recovery

Run the whole sequence again, in order, in a clean directory against a pinned tag. All four
observations should hold together:

```bash
cosign verify-blob ... checksums.txt        # prints: Verified OK
sha256sum -c checksums.txt --ignore-missing # prints: sentinel-<version>-<os>-<arch>.tar.gz: OK
slsa-verifier verify-artifact ...           # exits zero
sentinel version                            # a real version, commit, and build date
```

The last line is the one people skip. If `sentinel version` reports `dev / unknown / unknown` after
you believed you installed a release binary, you are running something other than the archive you
just verified.

## Prevent recurrence

Use `curl -fLO` everywhere, in documentation, scripts, and Dockerfiles. Silently saving an error page
under the asset's name is the single most common way this goes wrong.

Pin the release tag rather than fetching through `latest/download` when the filename contains a
version number. The two disagree the moment a new release lands.

Automate the sequence as one unit so the signature check and the checksum comparison cannot be
separated. Verifying the signature without then comparing your archive against the list is a check
that feels complete and proves nothing about what you are about to run.

Pin `cosign` v3 or later and `slsa-verifier` in whatever image performs the verification, and record
the verified SHA-256 of each installed binary in your change record, so a future mismatch has
something to be compared against.

## Related

- [Installation](../intro/installation.md): the cosign and SLSA procedures in full, plus the
  checksum-only path.
- [Upgrade the Sentinel binary](../guides/upgrade-sentinel-binary.md): where release verification
  fits into an upgrade.
- [Troubleshooting](./troubleshooting.md): symptom index across all operations pages.
- [Documentation home](../index.md): everything else.

{/* sources: .github/workflows/release.yml, .goreleaser.yaml, release-notes.md, internal/cli/version.go, docs/runbooks/verify-release-artifacts.md */}
