package cmd

import (
	"fmt"
	"strings"
)

// weeklyTable represents a table with weeks as columns and rows of data.
type weeklyTable struct {
	labelColWidth int
	weekColWidth  int
	weeks         []string
}

// newWeeklyTable creates a new weekly table with the specified column widths and weeks.
func newWeeklyTable(labelColWidth, weekColWidth int, weeks []string) *weeklyTable {
	return &weeklyTable{
		labelColWidth: labelColWidth,
		weekColWidth:  weekColWidth,
		weeks:         weeks,
	}
}

// printHeader prints the table header with week ending dates.
func (t *weeklyTable) printHeader(labelTitle string) {
	fmt.Printf("%-*s", t.labelColWidth, labelTitle)
	for _, week := range t.weeks {
		fmt.Printf("%*s", t.weekColWidth, formatWeekEnd(week))
	}
	fmt.Printf("%*s\n", t.weekColWidth, "Total")
}

// printSeparator prints a horizontal separator line.
func (t *weeklyTable) printSeparator() {
	totalWidth := t.labelColWidth + t.weekColWidth*(len(t.weeks)+1)
	fmt.Println(strings.Repeat("-", totalWidth))
}

// printRow prints a data row with label, weekly values, and total.
// weekValues is a map from week (Monday date string) to count.
// Zero values are displayed as "-".
func (t *weeklyTable) printRow(label string, weekValues map[string]int) int {
	fmt.Printf("%-*s", t.labelColWidth, label)
	total := 0
	for _, week := range t.weeks {
		count := weekValues[week]
		if count == 0 {
			fmt.Printf("%*s", t.weekColWidth, "-")
		} else {
			fmt.Printf("%*d", t.weekColWidth, count)
		}
		total += count
	}
	fmt.Printf("%*d\n", t.weekColWidth, total)
	return total
}

// printRowWithSlice prints a data row using a slice of counts (one per week).
// This is useful when data is already ordered by week.
func (t *weeklyTable) printRowWithSlice(label string, counts []int) int {
	fmt.Printf("%-*s", t.labelColWidth, label)
	total := 0
	for _, count := range counts {
		if count == 0 {
			fmt.Printf("%*s", t.weekColWidth, "-")
		} else {
			fmt.Printf("%*d", t.weekColWidth, count)
		}
		total += count
	}
	fmt.Printf("%*d\n", t.weekColWidth, total)
	return total
}

// printTotalsRow prints a totals row with week totals and grand total.
// weekTotals is a map from week to total count for that week.
func (t *weeklyTable) printTotalsRow(label string, weekTotals map[string]int) {
	fmt.Printf("%-*s", t.labelColWidth, label)
	grandTotal := 0
	for _, week := range t.weeks {
		total := weekTotals[week]
		if total == 0 {
			fmt.Printf("%*s", t.weekColWidth, "-")
		} else {
			fmt.Printf("%*d", t.weekColWidth, total)
		}
		grandTotal += total
	}
	fmt.Printf("%*d\n", t.weekColWidth, grandTotal)
}
