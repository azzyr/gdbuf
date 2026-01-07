package tui

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/LJ-Software/gdbuf/internal/gdextension"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type progressMsg float64
type buildResultMsg error
type logLineMsg string

var (
	docStyle   = lipgloss.NewStyle().Margin(1, 2)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	logStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
)

type Model struct {
	builder      *gdextension.GDExtensionBuilder
	cppSourceDir string
	outputDir    string
	platforms    []string
	generateOnly bool

	currentPlatformIdx int
	overallProgress    progress.Model
	currentProgress    progress.Model
	spinner            spinner.Model

	width  int
	height int

	statusMessage string
	lastLogLine   string
	err           error
	done          bool

	logFile *os.File

	progressChan chan float64
	logChan      chan string
	resultChan   chan error
}

func Run(builder *gdextension.GDExtensionBuilder, cppSourceDir, outputDir string, platforms []string, generateOnly bool) error {
	logFile, err := os.Create("build.log")
	if err != nil {
		return fmt.Errorf("could not create build log file: %w", err)
	}
	defer logFile.Close()

	m := Model{
		builder:         builder,
		cppSourceDir:    cppSourceDir,
		outputDir:       outputDir,
		platforms:       platforms,
		generateOnly:    generateOnly,
		overallProgress: progress.New(progress.WithDefaultGradient()),
		currentProgress: progress.New(progress.WithDefaultGradient()),
		spinner:         spinner.New(),
		statusMessage:   "Initializing...",
		logFile:         logFile,
		progressChan:    make(chan float64, 100),
		logChan:         make(chan string, 100),
		resultChan:      make(chan error),
	}
	m.spinner.Spinner = spinner.Dot
	m.spinner.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	if _, err := tea.NewProgram(m).Run(); err != nil {
		return err
	}
	return m.err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.startNextBuild(),
		m.waitForProgress(),
		m.waitForLogLine(),
		m.waitForBuildResult(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		padding := 2
		maxWidth := m.width - padding*2
		m.overallProgress.Width = maxWidth
		m.currentProgress.Width = maxWidth

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case progressMsg:
		cmd := m.currentProgress.SetPercent(float64(msg))
		return m, tea.Batch(cmd, m.waitForProgress())

	case logLineMsg:
		m.lastLogLine = string(msg)
		return m, m.waitForLogLine()

	case buildResultMsg:
		if msg != nil {
			m.err = msg
			m.statusMessage = fmt.Sprintf("Build failed: %v", msg)
			return m, tea.Quit
		}

		m.currentPlatformIdx++

		// Update overall progress
		progressPct := float64(m.currentPlatformIdx) / float64(len(m.platforms))
		cmd := m.overallProgress.SetPercent(progressPct)

		if m.currentPlatformIdx >= len(m.platforms) {
			m.done = true
			m.statusMessage = "All builds completed successfully!"
			m.currentProgress.SetPercent(1.0)
			return m, tea.Sequence(cmd, tea.Quit)
		}

		m.currentProgress.SetPercent(0)
		m.lastLogLine = "" // Clear log line for next build
		return m, tea.Batch(cmd, m.startNextBuild(), m.waitForBuildResult())
	}

	return m, nil
}

func (m Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("\n%s\nError: %v\nSee build.log for details.\n", titleStyle.Render("Build Failed"), m.err)
	}

	if m.done {
		return fmt.Sprintf("\n%s\n", titleStyle.Render(m.statusMessage))
	}

	currentPlatform := ""
	if m.currentPlatformIdx < len(m.platforms) {
		currentPlatform = m.platforms[m.currentPlatformIdx]
	}

	overall := fmt.Sprintf("Overall Progress (%d/%d):\n%s",
		m.currentPlatformIdx, len(m.platforms),
		m.overallProgress.View())

	current := fmt.Sprintf("Building %s:\n%s",
		currentPlatform,
		m.currentProgress.View())

	spinnerView := fmt.Sprintf("%s %s", m.spinner.View(), m.statusMessage)

	// Truncate log line if too long
	displayLog := m.lastLogLine
	if len(displayLog) > m.width-4 && m.width > 4 {
		displayLog = displayLog[:m.width-4] + "..."
	}
	logView := logStyle.Render(displayLog)

	return docStyle.Render(
		fmt.Sprintf("%s\n\n%s\n\n%s\n\n%s\n%s",
			titleStyle.Render("GDBuf Builder"),
			overall,
			current,
			spinnerView,
			logView,
		),
	) + "\n"
}

func (m Model) startNextBuild() tea.Cmd {
	if m.currentPlatformIdx >= len(m.platforms) {
		return nil
	}

	platform := m.platforms[m.currentPlatformIdx]

	return func() tea.Msg {
		go func() {
			pw := &progressWriter{
				logFile: m.logFile,
				onProgress: func(p float64) {
					select {
					case m.progressChan <- p:
					default:
					}
				},
				onLog: func(line string) {
					select {
					case m.logChan <- line:
					default:
					}
				},
			}

			err := m.builder.Build(m.cppSourceDir, m.outputDir, platform, m.generateOnly, pw, pw)
			m.resultChan <- err
		}()
		return nil
	}
}

func (m Model) waitForProgress() tea.Cmd {
	return func() tea.Msg {
		return progressMsg(<-m.progressChan)
	}
}

func (m Model) waitForLogLine() tea.Cmd {
	return func() tea.Msg {
		return logLineMsg(<-m.logChan)
	}
}

func (m Model) waitForBuildResult() tea.Cmd {
	return func() tea.Msg {
		return buildResultMsg(<-m.resultChan)
	}
}

type progressWriter struct {
	logFile    *os.File
	onProgress func(float64)
	onLog      func(string)
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	if pw.logFile != nil {
		pw.logFile.Write(p)
	}

	output := string(p)

	// Extract the last non-empty line for logging display
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) > 0 {
		lastLine := lines[len(lines)-1]
		if pw.onLog != nil && lastLine != "" {
			pw.onLog(lastLine)
		}
	}

	// Match [ 10%] or [10%]
	re := regexp.MustCompile(`\[\s*(\d+)%\]`)
	matches := re.FindStringSubmatch(output)
	if len(matches) > 1 {
		pct, _ := strconv.Atoi(matches[1])
		if pw.onProgress != nil {
			pw.onProgress(float64(pct) / 100.0)
		}
	}
	return len(p), nil
}
