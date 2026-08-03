# Contributing to Sentinel

Welcome to Sentinel! We appreciate your interest in contributing to our open-source project. Your contributions help
make our project better and more inclusive. Before you get started, please take a moment to review our contribution
guidelines.

## Types of Contributions We Welcome

- **Bug Fixes**: Help us identify and fix issues in the project.
- **New Features**: Contribute new features and enhancements to improve the project.
- **Documentation**: Improve project documentation, README files, or provide examples.
- **Bug Reports**: Report any issues, errors, or bugs you encounter.
- **Feedback**: Share your feedback, ideas, or suggestions for improvement.
- **Testing**: Help us test the project, identify edge cases, and ensure its reliability.
- **Code Reviews**: Review pull requests, provide feedback, and help maintain code quality.

## Getting Started

Prerequisite: Go 1.24+.

1. Fork the repository to your GitHub account.
2. Clone your forked repository to your local machine.
3. Create a new branch for your contribution:

   ``` shell
     git checkout -b feature/your-feature-name
   ```
4. Build and test locally:

   ``` shell
     go build -o sentinel
     go test ./...
   ```

   For end-to-end coverage against real database engines (requires Docker):

   ``` shell
     make e2e
   ```
5. Make your changes, additions, or fixes, following the [Architecture](#architecture) rules below.
6. Commit your changes with descriptive commit messages.
7. Push your changes to your forked repository:

   ``` shell
     git push origin feature/your-feature-name
   ```
8. Open a pull request (PR) from your forked repository to this main repository.

## Architecture

Sentinel follows a hexagonal (ports-and-adapters) architecture: domain logic
depends only on `internal/ports/` interfaces, never on concrete adapters, and
adapters within the same axis (e.g. the storage backends, or the per-engine
dump/restore adapters) never import each other. This keeps each database
engine and storage backend swappable and independently testable. Before
adding or moving code across package boundaries, read
[`docs/architecture/vision.md`](docs/architecture/vision.md) and
[ADR 0001](docs/adr/0001-adopt-hexagonal-architecture.md).

Feature-sized contributions (new commands, new adapters, new architectural
surface) go through the project's Spec Kit workflow (spec → clarify → plan →
tasks → analyze → implement) rather than landing as a single ad hoc PR —
maintainers can help scope this with you before you start writing code.

## Pull Request Guidelines

When creating a pull request, please follow these guidelines:

- Provide a clear and concise description of your changes.
- Reference any relevant issues or related pull requests.
- Ensure your code is well-documented and includes inline comments where necessary.
- Test your changes thoroughly and provide evidence of successful testing.
- Be responsive to any feedback or comments on your pull request.

## Code of Conduct

We enforce a Code of Conduct to ensure a welcoming and inclusive environment for all contributors. Please review our
[Code of Conduct](CODE_OF_CONDUCT.md) to understand our community standards.

## Our Commitment

Our project maintainers are committed to responding promptly to issues, pull requests, and inquiries from contributors.
We appreciate your time and effort in making this project better.

Thank you for contributing to Sentinel!

## Attribution

We would like to acknowledge and thank all our contributors for their valuable contributions to this project. Your
dedication and support are greatly appreciated.

---
*This CONTRIBUTING.md file is based on best practices and guidelines for open-source contribution. Please note that
contribution rules may be subject to change over time to better suit the needs of the project.*

[Back to Top](#contributing-to-sentinel)

```