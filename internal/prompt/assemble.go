// Package prompt assembles the system instruction from built-in prompt assets
// plus user-owned prompt files under the Pudding home.
package prompt

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultUserPromptName = "pudding.md"

//go:embed assets/core_system.md
var coreSystemPrompt string

//go:embed assets/mode_chat.md
var chatModePrompt string

//go:embed assets/mode_research.md
var researchModePrompt string

//go:embed assets/mode_workspace.md
var workspaceModePrompt string

type Segment struct {
	ID      string
	Layer   string
	Content string
}

type Input struct {
	UserInstruction string
	Mode            string
}

type Output struct {
	SystemInstruction string
	Segments          []Segment
}

type Loader struct {
	home string
}

func NewLoader(home string) *Loader {
	return &Loader{home: home}
}

func (l *Loader) Prompt(_ context.Context, mode string) (Output, error) {
	user, err := LoadUserInstruction(l.home)
	if err != nil {
		return Output{}, err
	}
	return Assemble(Input{UserInstruction: user, Mode: mode}), nil
}

func Assemble(input Input) Output {
	segments := make([]Segment, 0, 3)
	if core := strings.TrimSpace(coreSystemPrompt); core != "" {
		segments = append(segments, Segment{ID: "core_system", Layer: "core", Content: core})
	}
	if mode := strings.TrimSpace(modePrompt(input.Mode)); mode != "" {
		segments = append(segments, Segment{ID: "mode_" + normalizeMode(input.Mode), Layer: "mode", Content: mode})
	}
	if user := strings.TrimSpace(input.UserInstruction); user != "" {
		segments = append(segments, Segment{
			ID:    "user_system",
			Layer: "user",
			Content: "The following user preferences come from `<home>/pudding.md`. " +
				"They apply only when they do not conflict with higher-priority system rules or tool rules:\n\n" +
				user,
		})
	}

	parts := make([]string, 0, len(segments))
	for _, seg := range segments {
		if content := strings.TrimSpace(seg.Content); content != "" {
			parts = append(parts, content)
		}
	}
	return Output{SystemInstruction: strings.Join(parts, "\n\n"), Segments: segments}
}

func modePrompt(mode string) string {
	switch normalizeMode(mode) {
	case "workspace":
		return workspaceModePrompt
	case "research":
		return researchModePrompt
	case "chat":
		fallthrough
	default:
		return chatModePrompt
	}
}

func normalizeMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "code", "operate", "local", "workspace":
		return "workspace"
	case "work", "research":
		return "research"
	case "chat":
		fallthrough
	default:
		return "chat"
	}
}

func UserInstructionPath(home string) string {
	return filepath.Join(home, defaultUserPromptName)
}

func LoadUserInstruction(home string) (string, error) {
	path := UserInstructionPath(home)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("prompt: read %s: %w", path, err)
	}
	return strings.TrimSpace(string(b)), nil
}
