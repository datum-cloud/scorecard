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

type ashbyCandidate struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Location struct {
		City    string `json:"city"`
		Country string `json:"country"`
		Region  string `json:"region"`
	} `json:"location"`
	PrimaryLocation struct {
		Address struct {
			City        string `json:"city"`
			CountryCode string `json:"countryCode"`
			RegionCode  string `json:"regionCode"`
		} `json:"address"`
	} `json:"primaryLocation"`
}

type ashbyCandidateInfoResponse struct {
	Success bool           `json:"success"`
	Results ashbyCandidate `json:"results"`
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
	ashbyCmd.AddCommand(applicantMapCmd)
	applicantsByWeekCmd.Flags().Bool("json", false, "Output in JSON format")
	applicantsByWeekCmd.Flags().Bool("histo", false, "Display histogram of last 6 months")
	applicantMapCmd.Flags().Bool("debug", false, "Show debug output for candidate and application structures")
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

var applicantMapCmd = &cobra.Command{
	Use:   "applicant-map",
	Short: "Show a world map of applicant locations",
	Long:  "Fetches all applications and displays their locations on a terminal-based world map",
	Run:   runApplicantMap,
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

func fetchCandidateInfo(apiKey, candidateID string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"candidateId": candidateID,
	}

	respBody, err := ashbyRequest(apiKey, "candidate.info", body)
	if err != nil {
		return nil, err
	}

	var response map[string]interface{}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse candidate response: %w", err)
	}

	if success, ok := response["success"].(bool); !ok || !success {
		return nil, fmt.Errorf("API returned success=false for candidate %s", candidateID)
	}

	return response, nil
}

func fetchAllCandidates(apiKey string) (map[string]interface{}, error) {
	candidates := make(map[string]interface{})
	var cursor string

	for {
		body := map[string]interface{}{"limit": 100}
		if cursor != "" {
			body["cursor"] = cursor
		}

		respBody, err := ashbyRequest(apiKey, "candidate.list", body)
		if err != nil {
			return nil, err
		}

		var response map[string]interface{}
		if err := json.Unmarshal(respBody, &response); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		if success, ok := response["success"].(bool); !ok || !success {
			return nil, fmt.Errorf("API returned success=false")
		}

		if results, ok := response["results"].([]interface{}); ok {
			for _, result := range results {
				if candidate, ok := result.(map[string]interface{}); ok {
					if id, ok := candidate["id"].(string); ok {
						candidates[id] = candidate
					}
				}
			}
		}

		moreData, _ := response["moreDataAvailable"].(bool)
		if !moreData {
			break
		}

		if nextCursor, ok := response["nextCursor"].(string); ok {
			cursor = nextCursor
		} else {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	return candidates, nil
}

type locationCoord struct {
	lat  int
	lon  int
	name string
}

func timezoneToLocation(tz string) string {
	tzLower := strings.ToLower(tz)

	// Map common timezone patterns to locations
	timezoneMap := map[string]string{
		"america/new_york":     "USA",
		"america/chicago":      "USA",
		"america/denver":       "USA",
		"america/los_angeles":  "USA",
		"america/phoenix":      "USA",
		"america/toronto":      "Canada",
		"america/vancouver":    "Canada",
		"america/mexico_city":  "Mexico",
		"america/sao_paulo":    "Brazil",
		"america/buenos_aires": "Argentina",
		"europe/london":        "UK",
		"europe/paris":         "France",
		"europe/berlin":        "Germany",
		"europe/madrid":        "Spain",
		"europe/rome":          "Italy",
		"europe/amsterdam":     "Netherlands",
		"asia/tokyo":           "Japan",
		"asia/shanghai":        "China",
		"asia/hong_kong":       "Hong Kong",
		"asia/singapore":       "Singapore",
		"asia/dubai":           "UAE",
		"asia/kolkata":         "India",
		"asia/seoul":           "South Korea",
		"australia/sydney":     "Australia",
		"australia/melbourne":  "Australia",
		"pacific/auckland":     "New Zealand",
	}

	if location, ok := timezoneMap[tzLower]; ok {
		return location
	}

	// Try prefix matching
	for tzPattern, location := range timezoneMap {
		if strings.HasPrefix(tzLower, tzPattern) {
			return location
		}
	}

	return ""
}

func phoneNumberToCountry(phone string) string {
	// Remove common formatting characters
	phone = strings.ReplaceAll(phone, " ", "")
	phone = strings.ReplaceAll(phone, "-", "")
	phone = strings.ReplaceAll(phone, "(", "")
	phone = strings.ReplaceAll(phone, ")", "")
	phone = strings.ReplaceAll(phone, ".", "")

	if !strings.HasPrefix(phone, "+") {
		return ""
	}

	// Map country codes to countries
	countryCodeMap := map[string]string{
		"+1":   "USA",
		"+44":  "UK",
		"+33":  "France",
		"+49":  "Germany",
		"+34":  "Spain",
		"+39":  "Italy",
		"+31":  "Netherlands",
		"+46":  "Sweden",
		"+47":  "Norway",
		"+48":  "Poland",
		"+351": "Portugal",
		"+41":  "Switzerland",
		"+91":  "India",
		"+86":  "China",
		"+81":  "Japan",
		"+65":  "Singapore",
		"+82":  "South Korea",
		"+972": "Israel",
		"+971": "UAE",
		"+27":  "South Africa",
		"+61":  "Australia",
		"+64":  "New Zealand",
		"+55":  "Brazil",
		"+52":  "Mexico",
		"+54":  "Argentina",
		"+56":  "Chile",
		"+57":  "Colombia",
	}

	// Try to match country codes (longest first)
	for i := 4; i >= 2; i-- {
		if len(phone) >= i {
			code := phone[:i]
			if country, ok := countryCodeMap[code]; ok {
				return country
			}
		}
	}

	return ""
}

func getCoordinates(location string) *locationCoord {
	location = strings.ToLower(strings.TrimSpace(location))

	// Country and city mappings (simplified for terminal map)
	// Terminal map is approx 140 cols wide, 60 rows tall
	// Latitude: 90 (top) to -90 (bottom), maps to rows 0-59
	// Longitude: -180 (left) to 180 (right), maps to cols 0-139
	locationMap := map[string]locationCoord{
		// North America
		"usa":           {37, -95, "USA"},
		"united states": {37, -95, "USA"},
		"us":            {37, -95, "USA"},
		"canada":        {56, -106, "Canada"},
		"mexico":        {23, -102, "Mexico"},
		"new york":      {40, -74, "New York"},
		"san francisco": {37, -122, "San Francisco"},
		"los angeles":   {34, -118, "Los Angeles"},
		"chicago":       {41, -87, "Chicago"},
		"toronto":       {43, -79, "Toronto"},
		"vancouver":     {49, -123, "Vancouver"},

		// South America
		"brazil":        {-14, -51, "Brazil"},
		"argentina":     {-38, -63, "Argentina"},
		"colombia":      {4, -72, "Colombia"},
		"chile":         {-35, -71, "Chile"},

		// Europe
		"uk":            {54, -2, "UK"},
		"united kingdom":{54, -2, "UK"},
		"london":        {51, 0, "London"},
		"france":        {46, 2, "France"},
		"paris":         {48, 2, "Paris"},
		"germany":       {51, 10, "Germany"},
		"berlin":        {52, 13, "Berlin"},
		"spain":         {40, -3, "Spain"},
		"italy":         {41, 12, "Italy"},
		"netherlands":   {52, 5, "Netherlands"},
		"amsterdam":     {52, 4, "Amsterdam"},
		"sweden":        {60, 18, "Sweden"},
		"norway":        {60, 8, "Norway"},
		"poland":        {51, 19, "Poland"},
		"portugal":      {39, -8, "Portugal"},
		"switzerland":   {46, 8, "Switzerland"},

		// Asia
		"india":         {20, 77, "India"},
		"china":         {35, 105, "China"},
		"japan":         {36, 138, "Japan"},
		"tokyo":         {35, 139, "Tokyo"},
		"singapore":     {1, 103, "Singapore"},
		"korea":         {37, 127, "South Korea"},
		"south korea":   {37, 127, "South Korea"},
		"israel":        {31, 34, "Israel"},
		"tel aviv":      {32, 34, "Tel Aviv"},
		"thailand":      {15, 100, "Thailand"},
		"vietnam":       {14, 108, "Vietnam"},
		"philippines":   {12, 122, "Philippines"},

		// Middle East
		"uae":           {23, 53, "UAE"},
		"dubai":         {25, 55, "Dubai"},

		// Africa
		"south africa":  {-30, 22, "South Africa"},
		"nigeria":       {9, 8, "Nigeria"},
		"egypt":         {26, 30, "Egypt"},

		// Oceania
		"australia":     {-25, 133, "Australia"},
		"sydney":        {-33, 151, "Sydney"},
		"melbourne":     {-37, 144, "Melbourne"},
		"new zealand":   {-40, 174, "New Zealand"},
	}

	if coord, ok := locationMap[location]; ok {
		return &coord
	}

	// Try partial matches
	for key, coord := range locationMap {
		if strings.Contains(location, key) || strings.Contains(key, location) {
			return &coord
		}
	}

	return nil
}

func latLonToTerminal(lat, lon int, width, height int) (int, int) {
	// Convert lat/lon to terminal coordinates
	// Latitude: 90 to -90 -> row 0 to height-1
	// Longitude: -180 to 180 -> col 0 to width-1
	row := int((90.0 - float64(lat)) / 180.0 * float64(height))
	col := int((float64(lon) + 180.0) / 360.0 * float64(width))

	// Clamp to valid ranges
	if row < 0 {
		row = 0
	}
	if row >= height {
		row = height - 1
	}
	if col < 0 {
		col = 0
	}
	if col >= width {
		col = width - 1
	}

	return row, col
}

func renderWorldMap(locations map[string]int) {
	// Create a simple ASCII world map
	width := 80
	height := 10

	// Initialize map grid
	grid := make([][]rune, height)
	for i := range grid {
		grid[i] = make([]rune, width)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}

	// Draw simplified continents using basic shapes
	// Coordinates are scaled for 80x10 grid
	drawContinents := func() {
		// North America
		drawRegion(grid, 2, 6, 9, 26, '.')

		// South America
		drawRegion(grid, 6, 9, 17, 29, '.')

		// Europe
		drawRegion(grid, 2, 5, 31, 43, '.')

		// Africa
		drawRegion(grid, 4, 8, 34, 46, '.')

		// Asia
		drawRegion(grid, 1, 7, 43, 71, '.')

		// Australia
		drawRegion(grid, 7, 9, 63, 74, '.')
	}

	drawContinents()

	// Plot applicant locations
	for locationName, count := range locations {
		coord := getCoordinates(locationName)
		if coord != nil {
			row, col := latLonToTerminal(coord.lat, coord.lon, width, height)
			if row >= 0 && row < height && col >= 0 && col < width {
				// Use different markers based on count
				var marker rune
				switch {
				case count >= 100:
					marker = '█'
				case count >= 50:
					marker = '▓'
				case count >= 10:
					marker = '▒'
				case count >= 5:
					marker = '●'
				default:
					marker = '•'
				}
				grid[row][col] = marker
			}
		}
	}

	// Print the map
	fmt.Println("\n" + strings.Repeat("=", width))
	fmt.Println("APPLICANT LOCATION MAP")
	fmt.Println(strings.Repeat("=", width))
	fmt.Println()

	for _, row := range grid {
		fmt.Println(string(row))
	}

	fmt.Println()
	fmt.Println(strings.Repeat("=", width))
	fmt.Println("\nLegend:")
	fmt.Println("  •   1-4 applicants")
	fmt.Println("  ●   5-9 applicants")
	fmt.Println("  ▒   10-49 applicants")
	fmt.Println("  ▓   50-99 applicants")
	fmt.Println("  █   100+ applicants")
	fmt.Println()
}

func drawRegion(grid [][]rune, rowStart, rowEnd, colStart, colEnd int, char rune) {
	height := len(grid)
	width := len(grid[0])

	for r := rowStart; r < rowEnd && r < height; r++ {
		for c := colStart; c < colEnd && c < width; c++ {
			if r >= 0 && c >= 0 && grid[r][c] == ' ' {
				grid[r][c] = char
			}
		}
	}
}

func runApplicantMap(cmd *cobra.Command, args []string) {
	apiKey := loadAshbyEnv("ASHBY_API_KEY")
	showDebug, _ := cmd.Flags().GetBool("debug")

	fmt.Fprintln(os.Stderr, "Fetching applications...")
	applications, err := fetchAllApplications(apiKey)
	if err != nil {
		log.Fatalf("failed to fetch applications: %v", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d applications\n", len(applications))

	// Debug: print first application's full structure
	if showDebug && len(applications) > 0 {
		fmt.Fprintf(os.Stderr, "\n=== First application structure ===\n")
		appJSON, _ := json.MarshalIndent(applications[0], "", "  ")
		fmt.Fprintf(os.Stderr, "%s\n\n", string(appJSON))
	}

	// Fetch all candidates using candidate.list
	fmt.Fprintln(os.Stderr, "Fetching candidate data...")
	candidates, err := fetchAllCandidates(apiKey)
	if err != nil {
		log.Fatalf("failed to fetch candidates: %v", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d candidates\n", len(candidates))

	if showDebug && len(candidates) > 0 {
		// Print first candidate's full structure
		for _, candidateData := range candidates {
			fmt.Fprintf(os.Stderr, "\n=== First candidate structure ===\n")
			debugJSON, _ := json.MarshalIndent(candidateData, "", "  ")
			fmt.Fprintf(os.Stderr, "%s\n\n", string(debugJSON))
			break
		}
	}

	// Extract unique candidate IDs from applications
	candidateIDSet := make(map[string]bool)
	for _, app := range applications {
		candidateIDSet[app.Candidate.ID] = true
	}

	// Collect locations from candidates
	locationCounts := make(map[string]int)
	totalWithLocation := 0
	customFieldsSample := make(map[string]int)
	timezonesFound := 0
	phoneNumbersFound := 0

	for candidateID := range candidateIDSet {
		candidateData, ok := candidates[candidateID]
		if !ok {
			continue
		}

		// Try to extract location from various possible fields
		var location string

		if candidateMap, ok := candidateData.(map[string]interface{}); ok {
			// Check customFields for location data
			if customFields, ok := candidateMap["customFields"].([]interface{}); ok && len(customFields) > 0 {
				for _, field := range customFields {
					if fieldMap, ok := field.(map[string]interface{}); ok {
						fieldTitle := strings.ToLower(fmt.Sprintf("%v", fieldMap["title"]))
						fieldValue := fmt.Sprintf("%v", fieldMap["value"])

						// Track what custom fields exist
						customFieldsSample[fieldTitle]++

						// Look for location-related custom fields
						if strings.Contains(fieldTitle, "location") ||
						   strings.Contains(fieldTitle, "city") ||
						   strings.Contains(fieldTitle, "country") ||
						   strings.Contains(fieldTitle, "address") {
							if fieldValue != "" && fieldValue != "<nil>" {
								location = fieldValue
								break
							}
						}
					}
				}
			}

			// Check timezone as a backup
			if location == "" {
				if tz, ok := candidateMap["timezone"].(string); ok && tz != "" {
					timezonesFound++
					// Map common timezones to locations
					location = timezoneToLocation(tz)
				}
			}

			// Check phone numbers for country codes
			if location == "" {
				if phoneNumbers, ok := candidateMap["phoneNumbers"].([]interface{}); ok && len(phoneNumbers) > 0 {
					phoneNumbersFound++
					for _, phone := range phoneNumbers {
						if phoneMap, ok := phone.(map[string]interface{}); ok {
							if phoneValue, ok := phoneMap["value"].(string); ok {
								// Try to extract country from phone number format
								if country := phoneNumberToCountry(phoneValue); country != "" {
									location = country
									break
								}
							}
						}
					}
				}
			}
		}

		if location != "" {
			locationCounts[location]++
			totalWithLocation++
		}
	}

	if showDebug {
		fmt.Fprintf(os.Stderr, "\n=== Debug Info ===\n")
		fmt.Fprintf(os.Stderr, "Custom fields found:\n")
		for field, count := range customFieldsSample {
			fmt.Fprintf(os.Stderr, "  - %s: %d candidates\n", field, count)
		}
		fmt.Fprintf(os.Stderr, "Candidates with timezone: %d\n", timezonesFound)
		fmt.Fprintf(os.Stderr, "Candidates with phone: %d\n\n", phoneNumbersFound)
	}

	fmt.Fprintf(os.Stderr, "Found location data for %d/%d candidates\n\n", totalWithLocation, len(candidateIDSet))

	if totalWithLocation == 0 {
		fmt.Println("No location data found for applicants")
		return
	}

	// Display the map
	renderWorldMap(locationCounts)

	// Print location summary
	fmt.Println("Location Breakdown:")
	fmt.Println(strings.Repeat("-", 50))

	// Sort locations by count
	type locationCount struct {
		location string
		count    int
	}
	var sortedLocations []locationCount
	for loc, count := range locationCounts {
		sortedLocations = append(sortedLocations, locationCount{loc, count})
	}
	sort.Slice(sortedLocations, func(i, j int) bool {
		return sortedLocations[i].count > sortedLocations[j].count
	})

	for _, lc := range sortedLocations {
		percentage := float64(lc.count) / float64(totalWithLocation) * 100
		bar := strings.Repeat("▪", int(percentage/2))
		fmt.Printf("  %-25s %4d (%5.1f%%) %s\n", lc.location, lc.count, percentage, bar)
	}

	fmt.Printf("\n  Total: %d applicants with location data\n", totalWithLocation)
}
