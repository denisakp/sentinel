---
name: Feature request
about: Propose a new capability or enhancement aligned with Sentinel roadmap
title: "feat: "
labels: 'enhancement'
assignees: denisakp

---

## Problem Statement

Describe the operator/user problem this feature solves.

## Proposed Solution

A clear and concise description of what you want to happen.

## Use Cases

- Primary use case:
- Secondary use case (optional):

## Benefits

Explain expected outcomes (reliability, recovery time, cost reduction, operator UX, etc.).

## Roadmap Alignment

Reference `docs/roadmap/ROADMAP.md` and indicate where this fits:

- [ ] v1.1.0 MySQL/MariaDB compression
- [ ] v1.2.0 Advanced restore (PITR/incremental)
- [ ] v1.3.0 Security/reliability hardening
- [ ] v2.0.0 Enterprise scale/performance
- [ ] Not on roadmap yet (explain why it should be added)

## Proposed UX / CLI / Config

Provide a concrete proposal where possible.

- CLI example:

```bash
sentinel ...
```

- YAML example:

```yaml
databases:
  my-job:
    type: mysql
    # proposed fields
```

## Acceptance Criteria

List verifiable outcomes.

1. 
2. 
3. 

## Potential Challenges

Identify any possible challenges or complications in implementing this feature, such as technical limitations, conflicts
with existing functionalities, or resource constraints

## Suggested Implementation (Optional)

If applicable, propose a way this feature could be implemented within the existing project structure. Include any
technical details and target packages.

## Impact on Existing Features

Discuss whether this feature might affect existing functionalities and how any conflicts or integrations might be
managed.

## Alternatives Considered

A clear and concise description of any alternative solutions or features you've considered.

## Additional Context

Add any other context or screenshots about the feature request here.