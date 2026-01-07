# selectag

selectag is a command-line tool to select a new version interactively.
It helps you to choose the next semantic version in monorepo.

## Demo

https://github.com/user-attachments/assets/7e524def-44da-423c-8d31-d884b8991a83

## Prerequisites

- git
- GitHub CLI (gh)

## Installation

You can install selectag using `go install`:

```bash
go install github.com/wreulicke/selectag@latest
```

## Usage

Run `selectag` in your terminal:

```bash
selectag
```

## Note

selectag assumes that your repository uses git tags to manage versions and follows next rules:

- Tags are prefixed with the package name followed by a slash (e.g., `package-a/v1.0.0`, `package-b/v2.1.3`).
- Tags are prefixed with `v` (e.g., `v1.0.0`, `v2.1.3`).
- Tags follow semantic versioning (MAJOR.MINOR.PATCH).
- Tags are created on the `main` branch.
