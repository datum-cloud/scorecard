package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

func init() {
	rootCmd.AddCommand(datumCmd)
	datumCmd.AddCommand(activeUsersCmd)
	activeUsersCmd.Flags().Bool("json", false, "Output in JSON format")
	activeUsersCmd.Flags().Int("limit", 0, "Limit number of audit events to fetch (0 = all)")
}

type auditEvent struct {
	User struct {
		Username string `json:"username"`
		UID      string `json:"uid"`
	} `json:"user"`
	Verb                     string `json:"verb"`
	RequestReceivedTimestamp string `json:"requestReceivedTimestamp"`
}

type auditQueryResult struct {
	Items []auditEvent `json:"items"`
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

func runActiveUsers(cmd *cobra.Command, args []string) error {
	outputJSON, _ := cmd.Flags().GetBool("json")
	limit, _ := cmd.Flags().GetInt("limit")

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
	// Filter for write operations by real users (excluding system accounts)
	filter := "verb in ['create', 'update', 'patch'] && user.username.contains('system:') == false && user.uid != '' && objectRef.apiGroup in ['activity.miloapis.com'] == false"
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

	for _, event := range result.Items {
		username := event.User.Username
		if username == "" {
			continue
		}

		// Parse timestamp and get week
		t, err := time.Parse(time.RFC3339, event.RequestReceivedTimestamp)
		if err != nil {
			continue
		}
		weekStart := getWeekStart(t)

		// Only count if this week is in our range
		if users, ok := weekUsers[weekStart]; ok {
			users[username] = struct{}{}
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

	if outputJSON {
		type WeekData struct {
			WeekEnding  string `json:"week_ending"`
			ActiveUsers int    `json:"active_users"`
		}
		type jsonOutput struct {
			Weeks       []WeekData `json:"weeks"`
			CurrentWeek WeekData   `json:"current_week"`
			TotalUsers  int        `json:"total_unique_users"`
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
	}

	return nil
}
