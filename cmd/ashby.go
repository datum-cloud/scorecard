package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const ashbyAPIBase = "https://api.ashbyhq.com"

type ashbyApplication struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Status    string    `json:"status"`
	Candidate struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"candidate"`
	Job struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"job"`
}

type ashbyApplicationListResponse struct {
	Success           bool               `json:"success"`
	Results           []ashbyApplication `json:"results"`
	MoreDataAvailable bool               `json:"moreDataAvailable"`
	NextCursor        string             `json:"nextCursor"`
}

type ashbyJob struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	DepartmentID string `json:"departmentId"`
}

type ashbyJobListResponse struct {
	Success           bool       `json:"success"`
	Results           []ashbyJob `json:"results"`
	MoreDataAvailable bool       `json:"moreDataAvailable"`
	NextCursor        string     `json:"nextCursor"`
}

type ashbyDepartment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ashbyDepartmentListResponse struct {
	Success           bool              `json:"success"`
	Results           []ashbyDepartment `json:"results"`
	MoreDataAvailable bool              `json:"moreDataAvailable"`
	NextCursor        string            `json:"nextCursor"`
}

type ashbyJobInfo struct {
	Title      string
	Department string
}

type ashbyJobMetrics struct {
	Department string
	Title      string
	WeekCounts map[string]int
}

func init() {
	rootCmd.AddCommand(ashbyCmd)
	ashbyCmd.AddCommand(applicantsByWeekCmd)
	applicantsByWeekCmd.Flags().Bool("json", false, "Output in JSON format")
	applicantsByWeekCmd.Flags().Bool("histo", false, "Display histogram of last 6 months")
}

var ashbyCmd = &cobra.Command{
	Use:   "ashby",
	Short: "Pull metrics from Ashby HQ API",
	Long:  "Commands for pulling recruiting metrics from the Ashby HQ API",
}

var applicantsByWeekCmd = &cobra.Command{
	Use:   "applicants-by-week",
	Short: "Show applicants by week for each job",
	Long:  "Fetches all applications and groups them by job and week",
	Run:   runApplicantsByWeek,
}

func loadAshbyEnv(envVar string) string {
	v := os.Getenv(envVar)
	if v == "" {
		log.Fatalf("must set %v", envVar)
	}
	return v
}

func ashbyRequest(apiKey, endpoint string, body map[string]interface{}) ([]byte, error) {
	auth := base64.StdEncoding.EncodeToString([]byte(apiKey + ":"))

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", ashbyAPIBase+"/"+endpoint, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %d %s - %s", resp.StatusCode, resp.Status, string(respBody))
	}

	return respBody, nil
}

func fetchAllApplications(apiKey string) ([]ashbyApplication, error) {
	var applications []ashbyApplication
	var cursor string

	for {
		body := map[string]interface{}{"limit": 100}
		if cursor != "" {
			body["cursor"] = cursor
		}

		respBody, err := ashbyRequest(apiKey, "application.list", body)
		if err != nil {
			return nil, err
		}

		var response ashbyApplicationListResponse
		if err := json.Unmarshal(respBody, &response); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		if !response.Success {
			return nil, fmt.Errorf("API returned success=false")
		}

		applications = append(applications, response.Results...)

		if !response.MoreDataAvailable {
			break
		}
		cursor = response.NextCursor

		// Rate limiting
		time.Sleep(100 * time.Millisecond)
	}

	return applications, nil
}

func fetchAllDepartments(apiKey string) (map[string]string, error) {
	departments := make(map[string]string)
	var cursor string

	for {
		body := map[string]interface{}{"limit": 100}
		if cursor != "" {
			body["cursor"] = cursor
		}

		respBody, err := ashbyRequest(apiKey, "department.list", body)
		if err != nil {
			return nil, err
		}

		var response ashbyDepartmentListResponse
		if err := json.Unmarshal(respBody, &response); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		if !response.Success {
			return nil, fmt.Errorf("API returned success=false")
		}

		for _, dept := range response.Results {
			departments[dept.ID] = dept.Name
		}

		if !response.MoreDataAvailable {
			break
		}
		cursor = response.NextCursor

		time.Sleep(100 * time.Millisecond)
	}

	return departments, nil
}

func fetchAllJobs(apiKey string, departments map[string]string) (map[string]ashbyJobInfo, error) {
	jobs := make(map[string]ashbyJobInfo)
	var cursor string

	for {
		body := map[string]interface{}{"limit": 100}
		if cursor != "" {
			body["cursor"] = cursor
		}

		respBody, err := ashbyRequest(apiKey, "job.list", body)
		if err != nil {
			return nil, err
		}

		var response ashbyJobListResponse
		if err := json.Unmarshal(respBody, &response); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		if !response.Success {
			return nil, fmt.Errorf("API returned success=false")
		}

		for _, job := range response.Results {
			deptName := departments[job.DepartmentID]
			if deptName == "" {
				deptName = "No Department"
			}
			jobs[job.ID] = ashbyJobInfo{Title: job.Title, Department: deptName}
		}

		if !response.MoreDataAvailable {
			break
		}
		cursor = response.NextCursor

		time.Sleep(100 * time.Millisecond)
	}

	return jobs, nil
}

func runApplicantsByWeek(cmd *cobra.Command, args []string) {
	apiKey := loadAshbyEnv("ASHBY_API_KEY")
	outputJSON, _ := cmd.Flags().GetBool("json")
	outputHisto, _ := cmd.Flags().GetBool("histo")

	fmt.Fprintln(os.Stderr, "Fetching departments...")
	departments, err := fetchAllDepartments(apiKey)
	if err != nil {
		log.Fatalf("failed to fetch departments: %v", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d departments\n", len(departments))

	fmt.Fprintln(os.Stderr, "Fetching jobs...")
	jobs, err := fetchAllJobs(apiKey, departments)
	if err != nil {
		log.Fatalf("failed to fetch jobs: %v", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d jobs\n", len(jobs))

	fmt.Fprintln(os.Stderr, "Fetching applications...")
	applications, err := fetchAllApplications(apiKey)
	if err != nil {
		log.Fatalf("failed to fetch applications: %v", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d applications\n\n", len(applications))

	// Group by job and week
	// map[jobID]ashbyJobMetrics
	metrics := make(map[string]*ashbyJobMetrics)

	for _, app := range applications {
		jobID := app.Job.ID
		jobInfo, ok := jobs[jobID]
		if !ok {
			jobInfo = ashbyJobInfo{Title: app.Job.Title, Department: "No Department"}
			if jobInfo.Title == "" {
				jobInfo.Title = "Unknown Job"
			}
		}

		weekStart := getWeekStart(app.CreatedAt)

		if _, ok := metrics[jobID]; !ok {
			metrics[jobID] = &ashbyJobMetrics{
				Department: jobInfo.Department,
				Title:      jobInfo.Title,
				WeekCounts: make(map[string]int),
			}
		}
		metrics[jobID].WeekCounts[weekStart]++
	}

	if outputHisto {
		printHistogram(metrics)
	} else if outputJSON {
		printJSONGrouped(metrics)
	} else {
		printTableGrouped(metrics, len(applications))
	}
}

func printJSONGrouped(metrics map[string]*ashbyJobMetrics) {
	type WeekData struct {
		WeekEnding string `json:"week_ending"`
		Count      int    `json:"count"`
	}
	type JobData struct {
		Department  string   `json:"department"`
		Job         string   `json:"job"`
		Weeks       []WeekData `json:"weeks"`
		CurrentWeek WeekData `json:"current_week"`
		Total       int      `json:"total"`
	}

	allWeeks := getLast4Weeks()
	currentWeek := getCurrentWeekStart()
	var output []JobData

	for _, m := range metrics {
		var weeks []WeekData
		total := 0
		// Include all weeks, even those with zero count
		for _, week := range allWeeks {
			count := m.WeekCounts[week]
			weeks = append(weeks, WeekData{WeekEnding: weekStartToEnd(week), Count: count})
			total += count
		}
		output = append(output, JobData{
			Department: m.Department,
			Job: m.Title,
			Weeks: weeks,
			CurrentWeek: WeekData{WeekEnding: weekStartToEnd(currentWeek), Count: m.WeekCounts[currentWeek]},
			Total: total,
		})
	}

	sort.Slice(output, func(i, j int) bool {
		if output[i].Department != output[j].Department {
			return output[i].Department < output[j].Department
		}
		return output[i].Job < output[j].Job
	})

	b, _ := json.MarshalIndent(output, "", "  ")
	fmt.Println(string(b))
}

func printHistogram(metrics map[string]*ashbyJobMetrics) {
	weeks := getLast26Weeks()

	// Aggregate counts per week across all jobs
	weekTotals := make(map[string]int)
	for _, m := range metrics {
		for week, count := range m.WeekCounts {
			weekTotals[week] += count
		}
	}

	// Get counts for last 26 weeks in order
	var counts []int
	maxCount := 0
	for _, week := range weeks {
		count := weekTotals[week]
		counts = append(counts, count)
		if count > maxCount {
			maxCount = count
		}
	}

	if maxCount == 0 {
		fmt.Println("No applications in the last 6 months")
		return
	}

	// Print title
	fmt.Println("Applicants per Week (Last 6 Months)")
	fmt.Println()

	// Draw histogram (vertical bars going down)
	barChar := "█"
	maxBarHeight := 15
	labelWidth := 12

	// Print bars row by row from top to bottom
	for row := maxBarHeight; row >= 1; row-- {
		threshold := float64(row) / float64(maxBarHeight) * float64(maxCount)
		fmt.Printf("%*s", labelWidth, "")
		for _, count := range counts {
			if float64(count) >= threshold {
				fmt.Print(barChar)
			} else {
				fmt.Print(" ")
			}
		}
		fmt.Println()
	}

	// Print x-axis
	fmt.Printf("%*s", labelWidth, "")
	fmt.Println(strings.Repeat("-", 26))

	// Print month labels
	fmt.Printf("%*s", labelWidth, "")
	lastMonth := ""
	for _, week := range weeks {
		t, _ := time.Parse("2006-01-02", week)
		month := t.Format("Jan")
		if month != lastMonth {
			fmt.Print(month[:1])
			lastMonth = month
		} else {
			fmt.Print(" ")
		}
	}
	fmt.Println()

	// Print legend with scale
	fmt.Println()
	fmt.Printf("Scale: Each row = %.1f applicants\n", float64(maxCount)/float64(maxBarHeight))
	fmt.Printf("Max: %d applicants/week\n", maxCount)

	// Print weekly totals summary
	fmt.Println()
	fmt.Println("Weekly Breakdown:")
	fmt.Println()

	total := 0
	for i, week := range weeks {
		count := counts[i]
		total += count
		if count > 0 {
			bar := strings.Repeat("▪", int(float64(count)/float64(maxCount)*30)+1)
			fmt.Printf("  %s  %3d %s\n", formatWeekEnd(week), count, bar)
		} else {
			fmt.Printf("  %s  %3d\n", formatWeekEnd(week), count)
		}
	}
	fmt.Println()
	fmt.Printf("  Total: %d applicants over 26 weeks\n", total)
	fmt.Printf("  Average: %.1f applicants/week\n", float64(total)/26.0)
}

func printTableGrouped(metrics map[string]*ashbyJobMetrics, totalApps int) {
	weeks := getLast4Weeks()
	currentWeek := getCurrentWeekStart()

	// Group jobs by department
	deptJobs := make(map[string][]*ashbyJobMetrics)
	for _, m := range metrics {
		deptJobs[m.Department] = append(deptJobs[m.Department], m)
	}

	// Sort departments
	var depts []string
	for dept := range deptJobs {
		depts = append(depts, dept)
	}
	sort.Strings(depts)

	// Sort jobs within each department
	for _, jobs := range deptJobs {
		sort.Slice(jobs, func(i, j int) bool {
			return jobs[i].Title < jobs[j].Title
		})
	}

	// Create table
	table := newWeeklyTable(35, 10, weeks)
	table.printHeader("Job", currentWeek)
	table.printSeparator(currentWeek)

	// Print each department and its jobs
	weekTotals := make(map[string]int)

	for _, dept := range depts {
		jobs := deptJobs[dept]

		// Print department header
		fmt.Printf("\n%s\n", dept)

		deptWeekTotals := make(map[string]int)
		for _, job := range jobs {
			// Truncate job title if too long
			displayTitle := "  " + job.Title
			if len(displayTitle) > table.labelColWidth-2 {
				displayTitle = displayTitle[:table.labelColWidth-5] + "..."
			}

			// Print job row and accumulate totals
			table.printRow(displayTitle, job.WeekCounts, currentWeek)

			// Update totals
			for _, week := range weeks {
				count := job.WeekCounts[week]
				weekTotals[week] += count
				deptWeekTotals[week] += count
			}
			// Add current week to totals
			deptWeekTotals[currentWeek] += job.WeekCounts[currentWeek]
			weekTotals[currentWeek] += job.WeekCounts[currentWeek]
		}

		// Print department subtotal
		table.printRow("  Subtotal", deptWeekTotals, currentWeek)
	}

	// Print totals
	table.printSeparator(currentWeek)
	table.printTotalsRow("Total", weekTotals, currentWeek)
}
