package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var incidentsCmd = &cobra.Command{
	Use:   "incidents [org]/[repo]",
	Short: "Display incident counts by week for a GitHub repository",
	Long: `Query GitHub issues for a repository and count incidents by week.

Looks for issues with the following labels:
  - :incident/issue
  - :incident/report

Displays counts for the last 4 weeks.

Requires GITHUB_TOKEN environment variable to be set for API authentication.`,
	Args: cobra.ExactArgs(1),
	RunE: runIncidents,
}

func init() {
	rootCmd.AddCommand(incidentsCmd)
	incidentsCmd.Flags().Bool("json", false, "Output in JSON format")
}

type githubIssue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	Labels    []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

type weeklyIncidentCounts struct {
	WeekStart      string
	IncidentIssues int
	IncidentReports int
}

func runIncidents(cmd *cobra.Command, args []string) error {
	repo := args[0]

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN environment variable not set")
	}

	// Calculate last 4 week boundaries
	weeks := getLast4Weeks()

	fmt.Fprintf(os.Stderr, "Fetching incidents for %s...\n", repo)

	// Fetch issues with incident labels
	incidentIssues, err := fetchIncidentIssues(token, repo, ":incident/issue")
	if err != nil {
		return fmt.Errorf("failed to fetch incident issues: %w", err)
	}

	incidentReports, err := fetchIncidentIssues(token, repo, ":incident/report")
	if err != nil {
		return fmt.Errorf("failed to fetch incident reports: %w", err)
	}

	// Count by week
	counts := make([]weeklyIncidentCounts, len(weeks))
	for i, week := range weeks {
		counts[i].WeekStart = week
	}

	for _, issue := range incidentIssues {
		weekStart := getWeekStart(issue.CreatedAt)
		for i, week := range weeks {
			if weekStart == week {
				counts[i].IncidentIssues++
				break
			}
		}
	}

	for _, issue := range incidentReports {
		weekStart := getWeekStart(issue.CreatedAt)
		for i, week := range weeks {
			if weekStart == week {
				counts[i].IncidentReports++
				break
			}
		}
	}

	// Check for JSON output
	outputJSON, _ := cmd.Flags().GetBool("json")
	if outputJSON {
		printIncidentsJSON(repo, weeks, counts)
		return nil
	}

	// Print results using shared table functions
	fmt.Printf("Incident Counts for %s (Last 4 Weeks)\n\n", repo)

	table := newWeeklyTable(20, 10, weeks)
	table.printHeader("Label")
	table.printSeparator()

	// Extract counts into slices
	issuesCounts := make([]int, len(counts))
	reportsCounts := make([]int, len(counts))
	totalCounts := make([]int, len(counts))
	for i, c := range counts {
		issuesCounts[i] = c.IncidentIssues
		reportsCounts[i] = c.IncidentReports
		totalCounts[i] = c.IncidentIssues + c.IncidentReports
	}

	// Print rows
	table.printRowWithSlice(":incident/issue", issuesCounts)
	table.printRowWithSlice(":incident/report", reportsCounts)

	// Print totals
	table.printSeparator()
	table.printRowWithSlice("Total", totalCounts)

	return nil
}


func fetchIncidentIssues(token, repo, label string) ([]githubIssue, error) {
	var allIssues []githubIssue
	page := 1

	client := &http.Client{Timeout: 30 * time.Second}

	// Get issues from the last 4 weeks
	since := time.Now().AddDate(0, 0, -28).Format(time.RFC3339)

	for {
		url := fmt.Sprintf("https://api.github.com/repos/%s/issues?labels=%s&state=all&since=%s&per_page=100&page=%d",
			repo, url.QueryEscape(label), since, page)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == 404 {
			resp.Body.Close()
			return nil, fmt.Errorf("repository not found: %s", repo)
		}

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
		}

		var issues []githubIssue
		if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		if len(issues) == 0 {
			break
		}

		allIssues = append(allIssues, issues...)
		page++
	}

	return allIssues, nil
}

func printIncidentsJSON(repo string, weeks []string, counts []weeklyIncidentCounts) {
	type WeekData struct {
		WeekEnding     string `json:"week_ending"`
		IncidentIssue  int    `json:"incident_issue"`
		IncidentReport int    `json:"incident_report"`
		Total          int    `json:"total"`
	}
	type Output struct {
		Repository string     `json:"repository"`
		Weeks      []WeekData `json:"weeks"`
		Totals     struct {
			IncidentIssue  int `json:"incident_issue"`
			IncidentReport int `json:"incident_report"`
			Total          int `json:"total"`
		} `json:"totals"`
	}

	var output Output
	output.Repository = repo

	for i, week := range weeks {
		weekData := WeekData{
			WeekEnding:     weekStartToEnd(week),
			IncidentIssue:  counts[i].IncidentIssues,
			IncidentReport: counts[i].IncidentReports,
			Total:          counts[i].IncidentIssues + counts[i].IncidentReports,
		}
		output.Weeks = append(output.Weeks, weekData)
		output.Totals.IncidentIssue += counts[i].IncidentIssues
		output.Totals.IncidentReport += counts[i].IncidentReports
	}
	output.Totals.Total = output.Totals.IncidentIssue + output.Totals.IncidentReport

	b, _ := json.MarshalIndent(output, "", "  ")
	fmt.Println(string(b))
}
