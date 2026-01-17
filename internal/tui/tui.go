package tui

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/onion-4-dinner/gdbuf/internal/gdextension"
)

type progressMsg float64
type buildResultMsg struct{ err error }
type logLineMsg string
type buildStartedMsg string

var (
	docStyle   = lipgloss.NewStyle().Margin(1, 2)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	logStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	progressRe = regexp.MustCompile(`\[\s*(?:(\d+)%|(\d+)\s*/\s*(\d+))\]`)
	phaseRe    = regexp.MustCompile(`cmake -B build/[^/]+/([^/ ]+)`)
	ansiRe     = regexp.MustCompile("[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[a-zA-Z\\d]*)*)?\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PRZcf-ntqry=><~]))")
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
		progressChan:    make(chan float64, 1000), // Increased buffer
		logChan:         make(chan string, 1000),
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

	case buildStartedMsg:
		m.statusMessage = fmt.Sprintf("Building %s...", string(msg))
		return m, m.performBuild(string(msg))

	case buildResultMsg:
		if msg.err != nil {
			m.err = msg.err
			m.statusMessage = fmt.Sprintf("Build failed: %v", msg.err)
			return m, tea.Quit
		}

		m.currentPlatformIdx++

		// Update overall progress
		progressPct := float64(m.currentPlatformIdx) / float64(len(m.platforms))
		progressCmd := m.overallProgress.SetPercent(progressPct)

		if m.currentPlatformIdx >= len(m.platforms) {
			m.done = true
			m.statusMessage = "All builds completed successfully!"
			m.currentProgress.SetPercent(1.0)
			return m, tea.Sequence(progressCmd, tea.Quit)
		}

		m.currentProgress.SetPercent(0)
		m.lastLogLine = "" // Clear log line for next build
		return m, tea.Batch(progressCmd, m.startNextBuild())

	default:
		// Pass messages to progress bars to handle animation frames
		var cmd tea.Cmd
		var cmds []tea.Cmd
		var progressModel tea.Model

		progressModel, cmd = m.overallProgress.Update(msg)
		m.overallProgress = progressModel.(progress.Model)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		progressModel, cmd = m.currentProgress.Update(msg)
		m.currentProgress = progressModel.(progress.Model)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		return m, tea.Batch(cmds...)
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
		return buildStartedMsg(platform)
	}
}

func (m Model) performBuild(platform string) tea.Cmd {
	return func() tea.Msg {
		// Use a WaitGroup to ensure we wait for the goroutine to finish
		var wg sync.WaitGroup
		wg.Add(1)

		var err error
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panic in builder: %v", r)
				}
			}()

			pw := &progressWriter{
				logFile:      m.logFile,
				currentPhase: 0,
				totalPhases:  2, // Typically Debug and Release
				onProgress: func(p float64) {
					// BLOCKING send. We need to ensure the UI gets these updates.
					// Since we run this in a goroutine, blocking here just slows down the build output processing
					// to match the UI render speed, which is acceptable and safe (no deadlock with main thread).
					m.progressChan <- p
				},
				onLog: func(line string) {
					m.logChan <- line
				},
			}

			defer pw.Close()

			err = m.builder.Build(m.cppSourceDir, m.outputDir, platform, m.generateOnly, pw, pw)
		}()

		wg.Wait()
		return buildResultMsg{err: err}
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

type progressWriter struct {
	logFile      *os.File
	onProgress   func(float64)
	onLog        func(string)
	buffer       []byte
	mu           sync.Mutex
	currentPhase int
	totalPhases  int
}

func (pw *progressWriter) Close() error {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if len(pw.buffer) > 0 {
		lineStr := string(pw.buffer)
		lineStr = strings.TrimSpace(lineStr)
		if lineStr != "" {
			if pw.onLog != nil {
				pw.onLog(lineStr)
			}
		}
		pw.buffer = nil
	}
	return nil
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if pw.logFile != nil {
		pw.logFile.Write(p)
	}

	pw.buffer = append(pw.buffer, p...)

	for {
		// Handle both \n and \r as delimiters
		idxN := bytes.IndexByte(pw.buffer, '\n')
		idxR := bytes.IndexByte(pw.buffer, '\r')

		var idx int
		var sepLen int

		if idxN != -1 && idxR != -1 {
			if idxN < idxR {
				idx = idxN
				sepLen = 1
			} else {
				idx = idxR
				sepLen = 1
			}
		} else if idxN != -1 {
			idx = idxN
			sepLen = 1
		} else if idxR != -1 {
			idx = idxR
			sepLen = 1
		} else {
			break
		}

		line := pw.buffer[:idx]
		pw.buffer = pw.buffer[idx+sepLen:]

		lineStr := string(line)
		lineStr = strings.TrimSpace(lineStr)

		if lineStr != "" {
			if pw.onLog != nil {
				pw.onLog(lineStr)
			}

			// Strip ANSI color codes for regex matching
			cleanLine := stripAnsi(lineStr)

			// Check for phase change
			phaseMatches := phaseRe.FindStringSubmatch(cleanLine)
			if len(phaseMatches) > 1 {
				target := phaseMatches[1]
				if strings.Contains(target, "debug") {
					pw.currentPhase = 0
				} else if strings.Contains(target, "release") {
					pw.currentPhase = 1
				}
			}

			// Match [ 10%] or [1/100]
			matches := progressRe.FindStringSubmatch(cleanLine)
			if len(matches) > 1 {
				var rawProgress float64
				var valid bool

				if matches[1] != "" {
					// Percentage match
					pct, _ := strconv.Atoi(matches[1])
					rawProgress = float64(pct) / 100.0
					valid = true
				} else if matches[2] != "" && matches[3] != "" {
					// Step match [current/total]
					current, _ := strconv.Atoi(matches[2])
					total, _ := strconv.Atoi(matches[3])
					if total > 0 {
						rawProgress = float64(current) / float64(total)
						valid = true
					}
				}

				if valid && pw.onProgress != nil {
					// Scale progress based on phase
					scaledProgress := (float64(pw.currentPhase) + rawProgress) / float64(pw.totalPhases)
					if scaledProgress > 1.0 {
						scaledProgress = 1.0
					}
					pw.onProgress(scaledProgress)
				}
			} else {
				// DEBUG: Log first few chars of unmatched lines to see if we are missing something obvious
				// But only if it looks like it might be relevant (has numbers or brackets)
				if strings.Contains(cleanLine, "[") || strings.Contains(cleanLine, "%") {
					if pw.onLog != nil {
						pw.onLog(fmt.Sprintf("DEBUG: Unmatched: %s", cleanLine))
					}
				}
			}
		}
	}

	return len(p), nil
}

func stripAnsi(str string) string {
	return ansiRe.ReplaceAllString(str, "")
}
