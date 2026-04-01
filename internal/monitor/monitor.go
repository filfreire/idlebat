package monitor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/filfreire/idlebat/internal/display"
)

func MonitorStep(stateDir, stepID string, stepStart time.Time) {
	doneFile := filepath.Join(stateDir, "status", stepID+".done")
	streamLog := filepath.Join(stateDir, "logs", stepID+".stream.jsonl")

	for {
		if _, err := os.Stat(doneFile); err == nil {
			return
		}

		time.Sleep(10 * time.Second)
		elapsed := time.Since(stepStart)

		info, err := os.Stat(streamLog)
		if err != nil {
			display.LogWaiting(fmt.Sprintf("[%s elapsed] Waiting for output...", display.FormatDuration(elapsed)))
			continue
		}

		events := countFileLines(streamLog)
		size := display.HumanSize(info.Size())
		lastActivity := getLastActivity(streamLog)

		if lastActivity == "" {
			lastActivity = "working..."
		}

		display.LogWaiting(fmt.Sprintf("[%s | %d events | %s] %s",
			display.FormatDuration(elapsed), events, size, lastActivity))
	}
}

func countFileLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		count++
	}
	return count
}

func getLastActivity(streamLogPath string) string {
	data, err := os.ReadFile(streamLogPath)
	if err != nil {
		return ""
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	start := len(lines) - 10
	if start < 0 {
		start = 0
	}

	last := ""
	for _, line := range lines[start:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}

		msgType, _ := obj["type"].(string)

		switch msgType {
		case "assistant":
			msg, ok := obj["message"].(map[string]interface{})
			if !ok {
				continue
			}
			content, ok := msg["content"].([]interface{})
			if !ok {
				continue
			}
			for _, block := range content {
				b, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				bt, _ := b["type"].(string)
				if bt == "tool_use" {
					name, _ := b["name"].(string)
					last = fmt.Sprintf("[tool: %s]", name)
				} else if bt == "text" {
					text, _ := b["text"].(string)
					text = strings.TrimSpace(text)
					if len(text) > 80 {
						text = text[:80]
					}
					if text != "" {
						last = text
					}
				}
			}
		case "result":
			last = "[completed]"
		case "user":
			msg, ok := obj["message"].(map[string]interface{})
			if !ok {
				continue
			}
			content, ok := msg["content"].([]interface{})
			if !ok {
				continue
			}
			for _, block := range content {
				b, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				if b["type"] == "tool_result" {
					last = "[tool result received]"
				}
			}
		}
	}

	return last
}
