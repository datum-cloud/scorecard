# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Datum Scorecard** is a Go CLI tool for collecting KPI metrics from various external APIs (GitHub, Ashby, Datum Cloud). It aggregates data into weekly reports with consistent tabular output formats.

**Purpose**: Automate the collection of key performance indicators for business metrics reporting.

## Build and Run

### Using Go
```bash
go build              # Build the binary
./scorecard           # Run the CLI
go build && ./scorecard <command>  # Build and run

# Run a specific command
./scorecard datum active-users
./scorecard github stars datum-cloud
```

### Using Nix
```bash
nix build             # Build via Nix flake
./result/bin/scorecard  # Run the built binary

# Or enter dev shell
nix develop
go build && ./scorecard
```

**Build Environment**: Go 1.23, uses Cobra for CLI framework.

## Required Environment Variables

- `GITHUB_TOKEN` - GitHub personal access token (for `github` and `incidents` commands)
- `ASHBY_API_KEY` - Ashby HQ API key (for `ashby` commands)
- No env vars needed for `datum` commands (uses `datumctl` authentication)

## External Dependencies

- **`datumctl`** - Datum Cloud CLI, must be installed and authenticated via `datumctl auth login`
  - Used by `datum` commands to query audit logs and user data
  - Can be in `~/bin/datumctl` or on PATH
  - Commands shell out to `datumctl activity query` and `datumctl get users`

## Architecture

### Command Structure

All commands are defined in `cmd/` directory using Cobra:

```
scorecard
├── ashby                   # Ashby HQ recruiting metrics (cmd/ashby.go)
│   ├── applicants-by-week  # Weekly applicant counts with histogram
│   └── applicant-map       # Candidate data with geo information
├── datum                   # Datum Cloud metrics (cmd/datum.go)
│   ├── active-users        # Count active users by week from audit logs
│   └── signups             # Count new user signups by week
├── github                  # GitHub metrics (cmd/github.go)
│   └── stars <org>         # Repository star counts for an org/user
└── incidents <org/repo>    # GitHub incident tracking (cmd/incidents.go)
```

**Entry Point**: `cmd/root.go` defines root command and calls `Execute()`

### Shared Utilities

- **`cmd/weeks.go`** - Week boundary calculations (Monday-Sunday UTC)
  - `getWeekStart(t)` - Get Monday of week containing time t
  - `getLast4Weeks()` - Return last 4 completed weeks
  - `getCurrentWeekStart()` - Get Monday of current in-progress week
  - Reports show only **completed weeks** (current week shown separately)

- **`cmd/table.go`** - `weeklyTable` struct for consistent tabular output
  - `newWeeklyTable(labelWidth, weekWidth, weeks)` - Create table
  - `printHeader(label, currentWeek)` - Print header with week ending dates
  - `printRow(label, weekValues, currentWeek)` - Print data row
  - `printSeparator(currentWeek)` - Print horizontal separator

### Key Patterns and Conventions

1. **Output Formatting**
   - Progress/status messages → `stderr` (via `fmt.Fprintln(os.Stderr, ...)`)
   - Data output → `stdout` (via `fmt.Println()`)
   - Most commands support `--json` flag for JSON output
   - Zero values in tables displayed as "-" instead of "0"

2. **Week Boundaries**
   - Monday 00:00:00 UTC to Sunday 23:59:59 UTC
   - Reports show completed weeks only; current week shown in separate column
   - All time parsing uses RFC3339 format

3. **Pagination**
   - All API fetching functions handle pagination internally
   - Use `--all-pages` with datumctl for complete data
   - `--limit` flag available for testing/debugging

4. **Error Handling**
   - Auth errors detected via string matching (oauth2, token, credentials)
   - Return user-friendly error messages with remediation steps
   - Example: "authentication error: please run 'datumctl auth login' and try again"

5. **JSON Structures**
   - Use anonymous structs for API responses when possible
   - Use named structs only when reused across functions
   - Example: `auditEvent`, `auditQueryResult`, `userInfo` in datum.go

## Important Implementation Details

### Datum Commands (`cmd/datum.go`)

**Active Users**: Counts unique users who performed create/update/patch operations
- Query: `datumctl activity query --platform-wide --filter "verb in ['create', 'update', 'patch'] && user.username.contains('system:') == false && user.uid != '' && objectRef.apiGroup in ['activity.miloapis.com'] == false"`
- Excludes system accounts and activity API operations
- Supports `--list` flag to print user details (UID, Name, Email, Last Activity)
- User list sorted by last activity time, oldest first

**Signups**: Counts new user signups by tracking PATCH operations
- **Critical**: User signups are tracked as `verb == 'patch'` on `objectRef.resource == 'users'` by `user.username == 'zitadel-actions-server'`
- NOT tracked as CREATE operations (this was discovered via debugging)
- Query: `datumctl activity query --platform-wide --filter "verb == 'patch' && objectRef.resource == 'users' && user.username == 'zitadel-actions-server'"`
- Supports `--debug` flag for troubleshooting filters
- Supports `--week YYYY-MM-DD` flag for debugging specific weeks

### GitHub Commands (`cmd/github.go`)

- Requires `GITHUB_TOKEN` environment variable
- Uses GitHub REST API v3
- Handles pagination via Link headers
- Stars command supports `-s` flag for alphabetical sorting (default is by star count)

### Ashby Commands (`cmd/ashby.go`)

- Requires `ASHBY_API_KEY` environment variable
- `applicants-by-week`: Shows weekly applicant counts
  - `--histo` flag displays 6-month histogram
- `applicant-map`: Shows candidate data with geo information
  - `--debug` flag shows candidate and application structures

### Incidents Command (`cmd/incidents.go`)

- Tracks GitHub issues as incidents for a specific repository
- Uses GitHub API with pagination
- Groups incidents by week of creation

## Adding New Commands

1. Create or edit file in `cmd/` directory
2. Define Cobra command with `&cobra.Command{}`
3. Add to parent command in `init()` function
4. Implement `RunE` function
5. Follow patterns:
   - Use `cmd.Flags().Get*()` to retrieve flag values
   - Output progress to stderr, data to stdout
   - Support `--json` flag for structured output
   - Use `weeklyTable` for tabular weekly data
   - Handle auth errors with clear messages

**Example Structure**:
```go
var myCmd = &cobra.Command{
    Use:   "mycommand",
    Short: "Brief description",
    Long:  "Longer description with requirements",
    RunE:  runMyCommand,
}

func init() {
    rootCmd.AddCommand(myCmd)
    myCmd.Flags().Bool("json", false, "Output in JSON format")
}

func runMyCommand(cmd *cobra.Command, args []string) error {
    outputJSON, _ := cmd.Flags().GetBool("json")
    // Implementation
}
```

## Debugging Approaches

### When datum commands return unexpected results:

1. **Add `--debug` flag support** (like in signups command)
   - Show raw event counts and resource types
   - Print sample events to understand data structure
   - Count events by resource and verb

2. **Use `--week YYYY-MM-DD` flag** to focus on specific week
   - Reduces data volume for investigation
   - Shows all events in that time range
   - Helps identify correct filters

3. **Check audit event filters**
   - Verb (create, update, patch, get, delete)
   - Resource type (users, platforminvitations, etc.)
   - User (real users vs system accounts like zitadel-actions-server)
   - API group (exclude activity.miloapis.com operations)

4. **Verify week boundaries**
   - Monday 00:00:00 UTC to Sunday 23:59:59 UTC
   - Use `getWeekStart()` to check date calculations
   - Ensure timestamps parsed as RFC3339

### When building fails:

- Check `go.mod` is up to date: `go mod tidy`
- For Nix builds, update `vendorHash` in `flake.nix` if dependencies change
- Ensure Go 1.23+ is available

## Git Workflow

Based on commit history:
- Feature commits: `feat: description`
- Bug fixes: `fix: description` or `fix(scope): description`
- Documentation: `chore(docs):` or `fix(docs):`
- Use conventional commits style
- PRs merged to main branch

## Testing

- No automated tests currently (no `*_test.go` files)
- Manual testing via building and running commands
- Use `--limit` flag to test with smaller data sets
- Use `--debug` flags for troubleshooting

## Common Issues and Solutions

1. **"datumctl not found"**: Ensure datumctl is in ~/bin or PATH, and authenticated
2. **Auth errors with datum**: Run `datumctl auth login`
3. **Zero counts in datum signups**: Check if using correct filter (PATCH by zitadel-actions-server, not CREATE)
4. **Missing user details in --list**: fetchAllUsers may timeout; falls back to audit event data
5. **Week boundaries seem off**: All times are UTC; verify timezone conversions

## Future Considerations

When adding features:
- Maintain consistency with `--json` output format
- Use `weeklyTable` for weekly reports
- Add `--limit` flags for large data sets
- Consider adding `--debug` flags for complex queries
- Document any non-obvious API behavior (like the signups PATCH pattern)
