package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbletea"
	"go-download-server/internal/core"
	"go-download-server/internal/logger"
)

// Model represents the TUI state
type Model struct {
	engine   core.Engine
	tasks    []*core.Task
	width    int
	height   int
	cursor   int
	progress progress.Model
	ticker   *time.Ticker
}

// tickCmd is a helper function for the ticker
func tickCmd(c <-chan time.Time) tea.Cmd {
	return func() tea.Msg {
		return <-c
	}
}

// tickMsg is a custom message for tick events
type tickMsg time.Time

// Init initializes the TUI model
func (m *Model) Init() tea.Cmd {
	m.ticker = time.NewTicker(500 * time.Millisecond)
	return tickCmd(m.ticker.C)
}

// Update handles TUI events
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.progress.Width = m.width - 10
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc":
			if m.ticker != nil {
				m.ticker.Stop()
			}
			return m, tea.Quit
		case "j", "down":
			m.cursor++
			if m.cursor >= len(m.tasks) {
				m.cursor = 0
			}
			return m, nil
		case "k", "up":
			m.cursor--
			if m.cursor < 0 {
				m.cursor = len(m.tasks) - 1
			}
			return m, nil
		case " ":
			// 暂停/继续任务
			if len(m.tasks) > 0 {
				task := m.tasks[m.cursor]
				if task.Status == core.TaskStatusDownloading {
					err := m.engine.PauseTask(task.ID)
					if err != nil {
						logger.Errorf("暂停任务失败 %s: %v", task.ID, err)
					}
				} else if task.Status == core.TaskStatusPaused {
					err := m.engine.ResumeTask(nil, task.ID)
					if err != nil {
						logger.Errorf("恢复任务失败 %s: %v", task.ID, err)
					}
				}
			}
			return m, nil
		}
	case time.Time:
		// 定期更新任务列表
		m.tasks = m.engine.ListTasks()
		return m, tickCmd(m.ticker.C)
	}
	return m, nil
}

// View renders the TUI interface
func (m *Model) View() string {
	if len(m.tasks) == 0 {
		return "没有任务\n按 q 退出\n"
	}

	// 渲染任务列表
	var s string
	for i, task := range m.tasks {
		cursor := " "
		if i == m.cursor {
			cursor = ">"
		}

		// 渲染进度条
		var progressStr string
		var percentage float64
		var downloaded float64
		var totalSize float64

		// 检查Progress是否为空
		if task.Progress != nil {
			percentage = task.Progress.Percentage
			downloaded = float64(task.Progress.Downloaded) / (1024 * 1024)
			totalSize = float64(task.Progress.TotalSize) / (1024 * 1024)

			if task.Progress.TotalSize > 0 {
				progressStr = m.progress.ViewAs(float64(task.Progress.Percentage) / 100)
			} else {
				progressStr = "进度未知"
			}
		} else {
			progressStr = "进度未知"
		}

		s += fmt.Sprintf("%s %s - %s\n", cursor, task.ID, task.Status)
		s += fmt.Sprintf("  %s %.1f%% (%.2f MB/%.2f MB)\n\n",
			progressStr,
			percentage,
			downloaded,
			totalSize)
	}

	s += "\n按 q 退出\n按 j/k 或 上下箭头 选择任务\n按 空格 暂停/继续任务\n"
	return s
}

// Run starts the TUI interface
func Run(engine core.Engine) error {
	progressModel := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(50),
	)

	model := &Model{
		engine:   engine,
		tasks:    engine.ListTasks(),
		progress: progressModel,
	}

	p := tea.NewProgram(model)
	_, err := p.Run()
	return err
}
