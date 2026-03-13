package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var datumCmd = &cobra.Command{
	Use:   "datum",
	Short: "Datum Cloud metrics and reporting",
	Long:  "Commands for pulling metrics and data from Datum Cloud.",
}

var activeUsersCmd = &cobra.Command{
	Use:   "active-users",
	Short: "Count active users by week over the last 4 weeks",
	Long: `Query Datum Cloud audit logs to count unique users who have created or modified
resources, broken down by week over the last 4 completed weeks.

Requires datumctl to be installed and authenticated (run 'datumctl auth login').

Active users are those who performed create, update, or patch operations.
System accounts are excluded from the count.`,
	RunE: runActiveUsers,
}

var signupsCmd = &cobra.Command{
	Use:   "signups",
	Short: "Count new user signups by week over the last 4 weeks",
	Long: `Query Datum Cloud audit logs to count new user accounts created,
broken down by week over the last 4 completed weeks.

Requires datumctl to be installed and authenticated (run 'datumctl auth login').

Counts user signups tracked as PATCH operations on users by zitadel-actions-server.`,
	RunE: runSignups,
}

func init() {
	rootCmd.AddCommand(datumCmd)
	datumCmd.AddCommand(activeUsersCmd)
	datumCmd.AddCommand(signupsCmd)
	activeUsersCmd.Flags().Bool("json", false, "Output in JSON format")
	activeUsersCmd.Flags().Int("limit", 0, "Limit number of audit events to fetch (0 = all)")
	activeUsersCmd.Flags().Bool("list", false, "Print list of active users with details after the table")
	signupsCmd.Flags().Bool("json", false, "Output in JSON format")
	signupsCmd.Flags().Int("limit", 0, "Limit number of audit events to fetch (0 = all)")
	signupsCmd.Flags().Bool("debug", false, "Show debug information about audit events")
	signupsCmd.Flags().String("week", "", "Debug a specific week (e.g., '2026-01-18' for the week containing that date)")
}

type auditEvent struct {
	User struct {
		Username string `json:"username"`
		UID      string `json:"uid"`
	} `json:"user"`
	ObjectRef struct {
		Resource string `json:"resource"`
		APIGroup string `json:"apiGroup"`
		Name     string `json:"name"`
	} `json:"objectRef"`
	Verb                     string `json:"verb"`
	RequestReceivedTimestamp string `json:"requestReceivedTimestamp"`
}

type auditQueryResult struct {
	Items []auditEvent `json:"items"`
}

type userInfo struct {
	UID          string    `json:"uid"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	LastActivity time.Time `json:"last_activity,omitempty"`
}

func findDatumctl() (string, error) {
	// Prefer ~/bin/datumctl if it exists
	home, err := os.UserHomeDir()
	if err == nil {
		customPath := filepath.Join(home, "bin", "datumctl")
		if _, err := os.Stat(customPath); err == nil {
			return customPath, nil
		}
	}

	// Fall back to PATH
	path, err := exec.LookPath("datumctl")
	if err != nil {
		return "", fmt.Errorf("datumctl not found in ~/bin or PATH")
	}
	return path, nil
}

// fetchAllUsers fetches all user details from Datum Cloud via datumctl.
// Returns a map of UID to userInfo, or nil if the fetch fails.
func fetchAllUsers(datumctl string) map[string]userInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, datumctl, "get", "users", "-o", "json", "--platform-wide")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var result struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				Email string `json:"email"`
			} `json:"spec"`
			Status struct {
				DisplayName string `json:"displayName"`
				Email       string `json:"email"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil
	}

	users := make(map[string]userInfo)
	for _, item := range result.Items {
		uid := item.Metadata.Name
		name := item.Status.DisplayName
		email := item.Status.Email
		if email == "" {
			email = item.Spec.Email
		}
		users[uid] = userInfo{UID: uid, Name: name, Email: email}
	}
	return users
}

func runActiveUsers(cmd *cobra.Command, args []string) error {
	outputJSON, _ := cmd.Flags().GetBool("json")
	limit, _ := cmd.Flags().GetInt("limit")
	listUsers, _ := cmd.Flags().GetBool("list")

	datumctl, err := findDatumctl()
	if err != nil {
		return err
	}

	weeks := getLast4Weeks()
	if len(weeks) == 0 {
		return fmt.Errorf("failed to calculate weeks")
	}
	currentWeek := getCurrentWeekStart()

	fmt.Fprintln(os.Stderr, "Querying Datum Cloud audit logs for the last 4 weeks...")

	// Query audit logs for the last ~30 days (covers 4 weeks + current week)
	// Filter for write operations by real users (excluding system accounts and auto-provisioned personal resources)
	filter := "verb in ['create', 'update', 'patch'] && user.username.contains('system:') == false && objectRef.name.startsWith('personal-project-') == false && objectRef.name.startsWith('personal-org-') == false"
	queryArgs := []string{"activity", "query",
		"--platform-wide",
		"--start-time", "now-30d",
		"--end-time", "now",
		"--filter", filter,
		"-o", "json",
	}
	if limit > 0 {
		queryArgs = append(queryArgs, "--limit", fmt.Sprintf("%d", limit))
	} else {
		queryArgs = append(queryArgs, "--all-pages")
	}
	queryCmd := exec.Command(datumctl, queryArgs...)

	output, err := queryCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			// Detect auth-related failures
			if strings.Contains(stderr, "oauth2") ||
				strings.Contains(stderr, "token") ||
				strings.Contains(stderr, "nil context") ||
				strings.Contains(stderr, "credentials") {
				return fmt.Errorf("authentication error: please run 'datumctl auth login' and try again")
			}
			return fmt.Errorf("datumctl query failed: %s", stderr)
		}
		return fmt.Errorf("failed to run datumctl: %w", err)
	}

	var result auditQueryResult
	if err := json.Unmarshal(output, &result); err != nil {
		return fmt.Errorf("failed to parse audit log response: %w", err)
	}

	// Group users by week (including current week)
	weekUsers := make(map[string]map[string]struct{})
	for _, week := range weeks {
		weekUsers[week] = make(map[string]struct{})
	}
	weekUsers[currentWeek] = make(map[string]struct{})

	type userActivity struct {
		Username     string
		LastActivity time.Time
	}
	userActivities := make(map[string]userActivity) // uid -> activity info

	for _, event := range result.Items {
		uid := event.User.UID
		if uid == "" {
			continue
		}

		// Parse timestamp and get week
		t, err := time.Parse(time.RFC3339, event.RequestReceivedTimestamp)
		if err != nil {
			continue
		}

		// Track last activity time for this user (keyed by UID to deduplicate)
		existing := userActivities[uid]
		if existing.LastActivity.IsZero() || t.After(existing.LastActivity) {
			userActivities[uid] = userActivity{
				Username:     event.User.Username,
				LastActivity: t,
			}
		}

		weekStart := getWeekStart(t)

		// Only count if this week is in our range
		if users, ok := weekUsers[weekStart]; ok {
			users[uid] = struct{}{}
		}
	}

	// Count unique users per week
	weekCounts := make(map[string]int)
	allUsers := make(map[string]struct{})
	for week, users := range weekUsers {
		weekCounts[week] = len(users)
		for user := range users {
			allUsers[user] = struct{}{}
		}
	}

	// Build user list if requested
	var activeUserList []userInfo
	if listUsers {
		fmt.Fprintln(os.Stderr, "Looking up user details...")
		allUserDetails := fetchAllUsers(datumctl)

		for uid := range allUsers {
			activity := userActivities[uid]
			lastActivity := activity.LastActivity

			var info userInfo
			if allUserDetails != nil {
				if details, ok := allUserDetails[uid]; ok {
					info = details
					info.LastActivity = lastActivity
					activeUserList = append(activeUserList, info)
					continue
				}
			}
			// Fall back to audit event data
			activeUserList = append(activeUserList, userInfo{
				UID:          uid,
				Email:        activity.Username,
				LastActivity: lastActivity,
			})
		}

		// Sort by last activity time, oldest first
		sort.Slice(activeUserList, func(i, j int) bool {
			return activeUserList[i].LastActivity.Before(activeUserList[j].LastActivity)
		})
	}

	if outputJSON {
		type WeekData struct {
			WeekEnding  string `json:"week_ending"`
			ActiveUsers int    `json:"active_users"`
		}
		type jsonOutput struct {
			Weeks       []WeekData `json:"weeks"`
			CurrentWeek WeekData   `json:"current_week"`
			TotalUsers  int        `json:"total_unique_users"`
			Users       []userInfo `json:"users,omitempty"`
		}

		var weeksData []WeekData
		for _, week := range weeks {
			weeksData = append(weeksData, WeekData{
				WeekEnding:  weekStartToEnd(week),
				ActiveUsers: weekCounts[week],
			})
		}

		out := jsonOutput{
			Weeks: weeksData,
			CurrentWeek: WeekData{
				WeekEnding:  weekStartToEnd(currentWeek),
				ActiveUsers: weekCounts[currentWeek],
			},
			TotalUsers: len(allUsers),
			Users:      activeUserList,
		}

		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	} else {
		table := newWeeklyTable(20, 10, weeks)
		table.printHeader("Metric", currentWeek)
		table.printSeparator(currentWeek)
		table.printRow("Active Users", weekCounts, currentWeek)
		table.printSeparator(currentWeek)
		fmt.Printf("\nTotal Unique Users: %d\n", len(allUsers))

		if listUsers {
			fmt.Println("\nActive Users (sorted by last activity, oldest first):")
			fmt.Printf("%-24s %-30s %-35s %s\n", "User ID", "Name", "Email", "Last Activity")
			fmt.Println(strings.Repeat("-", 120))
			for _, u := range activeUserList {
				name := u.Name
				if name == "" {
					name = "-"
				}
				lastActivity := ""
				if !u.LastActivity.IsZero() {
					lastActivity = u.LastActivity.Format(time.RFC3339)
				}
				fmt.Printf("%-24s %-30s %-35s %s\n", u.UID, name, u.Email, lastActivity)
			}
		}
	}

	return nil
}

func runSignups(cmd *cobra.Command, args []string) error {
	outputJSON, _ := cmd.Flags().GetBool("json")
	limit, _ := cmd.Flags().GetInt("limit")
	debug, _ := cmd.Flags().GetBool("debug")
	weekFlag, _ := cmd.Flags().GetString("week")

	datumctl, err := findDatumctl()
	if err != nil {
		return err
	}

	var startTime, endTime string
	var weeks []string
	var currentWeek string

	// If specific week requested, query just that week
	if weekFlag != "" {
		t, err := time.Parse("2006-01-02", weekFlag)
		if err != nil {
			return fmt.Errorf("invalid week format, use YYYY-MM-DD: %w", err)
		}
		weekStart := getWeekStart(t)
		weekStartTime, _ := time.Parse("2006-01-02", weekStart)
		weekEndTime := weekStartTime.Add(7 * 24 * time.Hour)
		startTime = weekStartTime.Format(time.RFC3339)
		endTime = weekEndTime.Format(time.RFC3339)
		fmt.Fprintf(os.Stderr, "Querying week of %s (%s to %s)...\n", weekFlag, startTime, endTime)
	} else {
		weeks = getLast4Weeks()
		if len(weeks) == 0 {
			return fmt.Errorf("failed to calculate weeks")
		}
		currentWeek = getCurrentWeekStart()
		startTime = "now-30d"
		endTime = "now"
		fmt.Fprintln(os.Stderr, "Querying Datum Cloud audit logs for user signups (PATCH on users by zitadel-actions-server)...")
	}

	// Query audit logs for user creation events
	// User signups are tracked as PATCH operations on users by zitadel-actions-server
	filter := "verb == 'patch' && objectRef.resource == 'users' && user.username == 'zitadel-actions-server'"
	if weekFlag != "" {
		// When debugging a specific week, show ALL events (including system accounts and all verbs)
		filter = "objectRef.resource in ['users', 'platforminvitations', 'platformaccessapprovals', 'organizationmemberships', 'userpreferences']"
	} else if debug {
		// In debug mode, use a broader filter to see all create events
		filter = "verb == 'create' && user.username.contains('system:') == false"
	}
	queryArgs := []string{"activity", "query",
		"--platform-wide",
		"--start-time", startTime,
		"--end-time", endTime,
		"--filter", filter,
		"-o", "json",
	}
	if limit > 0 {
		queryArgs = append(queryArgs, "--limit", fmt.Sprintf("%d", limit))
	} else if debug || weekFlag != "" {
		// In debug mode or week mode, default to a reasonable limit to avoid hanging
		queryArgs = append(queryArgs, "--limit", "500")
	} else {
		queryArgs = append(queryArgs, "--all-pages")
	}
	queryCmd := exec.Command(datumctl, queryArgs...)

	output, err := queryCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			// Detect auth-related failures
			if strings.Contains(stderr, "oauth2") ||
				strings.Contains(stderr, "token") ||
				strings.Contains(stderr, "nil context") ||
				strings.Contains(stderr, "credentials") {
				return fmt.Errorf("authentication error: please run 'datumctl auth login' and try again")
			}
			return fmt.Errorf("datumctl query failed: %s", stderr)
		}
		return fmt.Errorf("failed to run datumctl: %w", err)
	}

	var result auditQueryResult
	if err := json.Unmarshal(output, &result); err != nil {
		return fmt.Errorf("failed to parse audit log response: %w", err)
	}

	if debug || weekFlag != "" {
		fmt.Fprintf(os.Stderr, "\nDEBUG: Found %d total events\n", len(result.Items))
		resourceCounts := make(map[string]int)
		apiGroupCounts := make(map[string]int)
		for _, event := range result.Items {
			resourceCounts[event.ObjectRef.Resource]++
			apiGroupCounts[event.ObjectRef.APIGroup]++
		}
		fmt.Fprintln(os.Stderr, "\nDEBUG: Resources being created:")
		for resource, count := range resourceCounts {
			fmt.Fprintf(os.Stderr, "  %s: %d\n", resource, count)
		}
		fmt.Fprintln(os.Stderr, "\nDEBUG: API Groups:")
		for apiGroup, count := range apiGroupCounts {
			fmt.Fprintf(os.Stderr, "  %s: %d\n", apiGroup, count)
		}

		// Show all user-related events when debugging a specific week
		if weekFlag != "" {
			fmt.Fprintln(os.Stderr, "\nDEBUG: All user-related events in this week (ALL verbs, including system accounts):")
			for i, event := range result.Items {
				fmt.Fprintf(os.Stderr, "  Event %d: timestamp=%s verb=%s resource=%s apiGroup=%s name=%s user=%s uid=%s\n",
					i+1, event.RequestReceivedTimestamp, event.Verb, event.ObjectRef.Resource,
					event.ObjectRef.APIGroup, event.ObjectRef.Name, event.User.Username, event.User.UID)
			}
		} else {
			fmt.Fprintln(os.Stderr, "\nDEBUG: Sample events (first 5):")
			for i, event := range result.Items {
				if i >= 5 {
					break
				}
				fmt.Fprintf(os.Stderr, "  Event %d: verb=%s resource=%s apiGroup=%s name=%s user=%s\n",
					i+1, event.Verb, event.ObjectRef.Resource, event.ObjectRef.APIGroup,
					event.ObjectRef.Name, event.User.Username)
			}
		}
		fmt.Fprintln(os.Stderr, "")
	}

	// If querying a specific week, just return the debug info
	if weekFlag != "" {
		resourceVerbCounts := make(map[string]map[string]int)
		signupCount := 0
		for _, event := range result.Items {
			if resourceVerbCounts[event.ObjectRef.Resource] == nil {
				resourceVerbCounts[event.ObjectRef.Resource] = make(map[string]int)
			}
			resourceVerbCounts[event.ObjectRef.Resource][event.Verb]++

			// Count signups (PATCH on users by zitadel-actions-server)
			if event.ObjectRef.Resource == "users" && event.Verb == "patch" && event.User.Username == "zitadel-actions-server" {
				signupCount++
			}
		}
		fmt.Fprintf(os.Stderr, "\nSummary for week (by resource and verb):\n")
		for resource, verbs := range resourceVerbCounts {
			for verb, count := range verbs {
				fmt.Fprintf(os.Stderr, "  %s (%s): %d\n", resource, verb, count)
			}
		}
		fmt.Fprintf(os.Stderr, "\nSignup events (users PATCH by zitadel-actions-server): %d\n", signupCount)
		return nil
	}

	// Count signups by week (including current week)
	weekSignups := make(map[string]int)
	for _, week := range weeks {
		weekSignups[week] = 0
	}
	weekSignups[currentWeek] = 0

	totalSignups := 0
	matchedEvents := 0
	for _, event := range result.Items {
		// In non-debug mode, we already filtered. In debug mode, filter here
		if debug && (event.ObjectRef.Resource != "users" || event.Verb != "patch" || event.User.Username != "zitadel-actions-server") {
			continue
		}
		matchedEvents++

		// Parse timestamp and get week
		t, err := time.Parse(time.RFC3339, event.RequestReceivedTimestamp)
		if err != nil {
			if debug {
				fmt.Fprintf(os.Stderr, "DEBUG: Failed to parse timestamp: %s\n", event.RequestReceivedTimestamp)
			}
			continue
		}
		weekStart := getWeekStart(t)

		// Only count if this week is in our range
		if _, ok := weekSignups[weekStart]; ok {
			weekSignups[weekStart]++
			totalSignups++
		}
	}

	if debug {
		fmt.Fprintf(os.Stderr, "DEBUG: Matched %d PATCH events on users by zitadel-actions-server\n", matchedEvents)
		fmt.Fprintf(os.Stderr, "DEBUG: %d events fell within the week range\n\n", totalSignups)
	}

	if outputJSON {
		type WeekData struct {
			WeekEnding string `json:"week_ending"`
			Signups    int    `json:"signups"`
		}
		type jsonOutput struct {
			Weeks       []WeekData `json:"weeks"`
			CurrentWeek WeekData   `json:"current_week"`
			TotalSignups int       `json:"total_signups"`
		}

		var weeksData []WeekData
		for _, week := range weeks {
			weeksData = append(weeksData, WeekData{
				WeekEnding: weekStartToEnd(week),
				Signups:    weekSignups[week],
			})
		}

		out := jsonOutput{
			Weeks: weeksData,
			CurrentWeek: WeekData{
				WeekEnding: weekStartToEnd(currentWeek),
				Signups:    weekSignups[currentWeek],
			},
			TotalSignups: totalSignups,
		}

		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	} else {
		table := newWeeklyTable(20, 10, weeks)
		table.printHeader("Metric", currentWeek)
		table.printSeparator(currentWeek)
		table.printRow("New Signups", weekSignups, currentWeek)
		table.printSeparator(currentWeek)
		fmt.Printf("\nTotal Signups: %d\n", totalSignups)
	}

	return nil
}
