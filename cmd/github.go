package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var githubCmd = &cobra.Command{
	Use:   "github",
	Short: "GitHub metrics and reporting",
	Long:  "Commands for pulling metrics and data from GitHub.",
}

var starsCmd = &cobra.Command{
	Use:   "stars [org-or-user]",
	Short: "Display star counts for repositories in a GitHub organization or user",
	Long: `Fetch and display star counts for all repositories in a GitHub organization or user.

Requires GITHUB_TOKEN environment variable to be set for API authentication.

By default, repositories are sorted by star count (ascending). Use -s to sort alphabetically.`,
	Args: cobra.ExactArgs(1),
	RunE: runStars,
}

func init() {
	rootCmd.AddCommand(githubCmd)
	githubCmd.AddCommand(starsCmd)
	starsCmd.Flags().BoolP("sort", "s", false, "Sort alphabetically by repository name")
}

type githubRepo struct {
	Name            string `json:"name"`
	StargazersCount int    `json:"stargazers_count"`
}

func runStars(cmd *cobra.Command, args []string) error {
	target := args[0]
	sortAlpha, _ := cmd.Flags().GetBool("sort")

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN environment variable not set")
	}

	fmt.Fprintf(os.Stderr, "Fetching repositories for %s...\n", target)

	// Try org endpoint first, then user
	repos, err := fetchGitHubRepos(token, "orgs", target)
	if err != nil {
		repos, err = fetchGitHubRepos(token, "users", target)
		if err != nil {
			return fmt.Errorf("could not find organization or user '%s': %w", target, err)
		}
	}

	if len(repos) == 0 {
		return fmt.Errorf("no repositories found for '%s'", target)
	}

	// Sort repositories
	if sortAlpha {
		sort.Slice(repos, func(i, j int) bool {
			return strings.ToLower(repos[i].Name) < strings.ToLower(repos[j].Name)
		})
	} else {
		// Sort by star count ascending (most popular at the end)
		sort.Slice(repos, func(i, j int) bool {
			return repos[i].StargazersCount < repos[j].StargazersCount
		})
	}

	// Print header
	fmt.Printf("%-50s %10s\n", "Repository", "Stars")
	fmt.Println(strings.Repeat("=", 62))

	// Print repos and calculate total
	total := 0
	for _, repo := range repos {
		fmt.Printf("%-50s %10d\n", repo.Name, repo.StargazersCount)
		total += repo.StargazersCount
	}

	// Print footer
	fmt.Println(strings.Repeat("=", 62))
	timestamp := time.Now().UTC().Format("2006-01-02 15:04 UTC")
	fmt.Printf("%-50s %10d\n", fmt.Sprintf("Total [ %s ]", timestamp), total)

	return nil
}

func fetchGitHubRepos(token, entityType, target string) ([]githubRepo, error) {
	var allRepos []githubRepo
	page := 1

	client := &http.Client{Timeout: 30 * time.Second}

	for {
		url := fmt.Sprintf("https://api.github.com/%s/%s/repos?per_page=100&page=%d", entityType, target, page)

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
			return nil, fmt.Errorf("not found")
		}

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
		}

		var repos []githubRepo
		if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		if len(repos) == 0 {
			break
		}

		allRepos = append(allRepos, repos...)
		page++
	}

	return allRepos, nil
}
