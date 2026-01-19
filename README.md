# Datum Scorecard

Assortment of automation that feeds our KPI collection.

## Usage

```
$ export ASHBY_API_KEY=abcdef123...
$ export GITHUB_TOKEN=ghp_....
$ go build      # or nix build
$ ./scorecard
...
Usage:
  scorecard [command]

Available Commands:
  ashby       Pull metrics from Ashby HQ API
  completion  Generate the autocompletion script for the specified shell
  github      GitHub metrics and reporting
  help        Help about any command
  incidents   Display incident counts by week for a GitHub repository
...
```
