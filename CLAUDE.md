# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Run

```bash
go build              # Build the binary
./scorecard           # Run the CLI
go build && ./scorecard <command>  # Build and run
```

Alternative: `nix build` if using Nix.

## Required Environment Variables

- `GITHUB_TOKEN` - GitHub personal access token (for `github` and `incidents` commands)
- `ASHBY_API_KEY` - Ashby HQ API key (for `ashby` commands)

## Architecture

This is a Go CLI tool built with Cobra for collecting KPI metrics from external APIs.

### Command Structure

- `cmd/root.go` - Root command definition and `Execute()` entry point
- `cmd/github.go` - GitHub stars subcommand (`github stars <org>`)
- `cmd/incidents.go` - GitHub incidents tracking (`incidents <org/repo>`)
- `cmd/ashby.go` - Ashby HQ recruiting metrics (`ashby applicants-by-week`)

### Shared Utilities

- `cmd/weeks.go` - Week boundary calculations (Monday-Sunday UTC). Reports show only completed weeks.
- `cmd/table.go` - `weeklyTable` struct for consistent tabular output across commands.

### Patterns

- All API fetching functions handle pagination internally
- Commands support `--json` flag for JSON output where applicable
- Progress/status messages go to stderr; data output goes to stdout
- Week boundaries are Monday 00:00:00 UTC to Sunday 23:59:59 UTC
