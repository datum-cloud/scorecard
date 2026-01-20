package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var datumCmd = &cobra.Command{
	Use:   "datum",
	Short: "Datum Cloud metrics and reporting",
	Long:  "Commands for pulling metrics and data from Datum Cloud.",
}

var activeUsersCmd = &cobra.Command{
	Use:   "active-users",
	Short: "Count users who have created or modified resources in the last week",
	Long: `Query Datum Cloud audit logs to count unique users who have created or modified
resources over the last week.

Requires datumctl to be installed and authenticated (run 'datumctl auth login').

Active users are those who performed create, update, or patch operations.
System accounts (prefixed with 'system:') are excluded from the count.`,
	RunE: runActiveUsers,
}

func init() {
	rootCmd.AddCommand(datumCmd)
	datumCmd.AddCommand(activeUsersCmd)
	activeUsersCmd.Flags().Bool("json", false, "Output in JSON format")
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

func runActiveUsers(cmd *cobra.Command, args []string) error {
	outputJSON, _ := cmd.Flags().GetBool("json")

	// Check if datumctl is available
	if _, err := exec.LookPath("datumctl"); err != nil {
		return fmt.Errorf("datumctl not found in PATH: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Querying Datum Cloud audit logs for the last week...")

	// Query audit logs for create/update/patch operations in the last week
	queryCmd := exec.Command("datumctl", "activity", "query",
		"--start-time", "now-7d",
		"--end-time", "now",
		"--filter", "verb in ['create', 'update', 'patch']",
		"--all-pages",
		"-o", "json",
	)

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

	// Count unique users, excluding system accounts
	userSet := make(map[string]struct{})
	for _, event := range result.Items {
		username := event.User.Username
		// Skip system accounts
		if strings.HasPrefix(username, "system:") {
			continue
		}
		if username != "" {
			userSet[username] = struct{}{}
		}
	}

	activeUserCount := len(userSet)

	if outputJSON {
		type jsonOutput struct {
			ActiveUsers int      `json:"active_users"`
			Period      string   `json:"period"`
			Users       []string `json:"users"`
		}

		users := make([]string, 0, len(userSet))
		for user := range userSet {
			users = append(users, user)
		}

		out := jsonOutput{
			ActiveUsers: activeUserCount,
			Period:      "last 7 days",
			Users:       users,
		}

		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("Active Users (Last 7 Days): %d\n", activeUserCount)
		if activeUserCount > 0 {
			fmt.Println("\nUsers:")
			for user := range userSet {
				fmt.Printf("  - %s\n", user)
			}
		}
	}

	return nil
}
