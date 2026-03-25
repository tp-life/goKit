package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"goKit/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/subosito/gotenv"
)

func main() {
	_ = gotenv.Load(".env")

	defaultBaseURL := strings.TrimSpace(os.Getenv("FUNDING_TUI_BASE_URL"))
	if defaultBaseURL == "" {
		defaultBaseURL = "http://127.0.0.1:8080"
	}
	defaultToken := strings.TrimSpace(os.Getenv("EXECUTION_API_TOKEN"))

	baseURL := flag.String("base-url", defaultBaseURL, "Funding monitor API base URL")
	token := flag.String("token", defaultToken, "Execution API bearer token")
	refresh := flag.Duration("refresh", 8*time.Second, "Refresh interval")
	opportunityLimit := flag.Int("opportunity-limit", 5000, "How many opportunities to fetch per refresh")
	flag.Parse()

	client := tui.NewClient(*baseURL, *token, *opportunityLimit)
	model := tui.NewModel(client, *refresh)

	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithInputTTY(),
	)
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
}
