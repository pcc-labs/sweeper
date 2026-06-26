package worker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/papercomputeco/sweeper/pkg/linter"
)

// agentPreamble identifies this as an automated tool to comply with
// Anthropic's agentic system usage requirements.
const agentPreamble = "You are a sub-agent of Sweeper, an automated code maintenance tool. " +
	"A human developer initiated this run and will review all changes. " +
	"Your task is to make the maintenance change described below — this may be fixing lint issues, " +
	"writing or repairing tests, improving documentation, or refactoring code. " +
	"Preserve the existing behavior of the code you change, do not commit, and do not access external services.\n\n"

type Task struct {
	ID        int
	File      string
	Dir       string
	Issues    []linter.Issue
	Prompt    string
	RawOutput string
}

func BuildPrompt(task Task) string {
	var b strings.Builder
	b.WriteString(agentPreamble)
	b.WriteString("Fix the following lint issues in " + task.File + ":\n\n")
	for _, iss := range task.Issues {
		fmt.Fprintf(&b, "- Line %d: %s (%s)\n", iss.Line, iss.Message, iss.Linter)
	}
	b.WriteString("\nAddress each issue. Preserve existing behavior. Commit nothing.")
	return b.String()
}

func BuildRawPrompt(task Task) string {
	var b strings.Builder
	b.WriteString(agentPreamble)
	b.WriteString("The following lint output was produced. Analyze it, identify the issues, and fix them:\n\n")
	b.WriteString(task.RawOutput)
	b.WriteString("\n\nAddress each issue you can identify. Preserve existing behavior. Commit nothing.")
	return b.String()
}

// BuildRetryPrompt creates a retry prompt that includes the prior attempt output
// and instructs the agent to try a different approach.
func BuildRetryPrompt(task Task, priorOutput string) string {
	var b strings.Builder
	b.WriteString(agentPreamble)
	b.WriteString("Fix the following lint issues in " + task.File + ":\n\n")
	for _, iss := range task.Issues {
		fmt.Fprintf(&b, "- Line %d: %s (%s)\n", iss.Line, iss.Message, iss.Linter)
	}
	b.WriteString("\nA previous attempt did not fully resolve these issues. Here is what was tried:\n\n")
	b.WriteString(truncateOutput(priorOutput, 2000))
	b.WriteString("\n\nTry a different approach. Do not repeat what was already tried. Address each issue. Preserve existing behavior. Commit nothing.")
	return b.String()
}

// BuildExplorationPrompt creates an exploration prompt triggered after stagnation.
// It instructs the agent to consider refactoring surrounding code.
func BuildExplorationPrompt(task Task, priorOutput string) string {
	var b strings.Builder
	b.WriteString(agentPreamble)
	b.WriteString("Fix the following lint issues in " + task.File + ":\n\n")
	for _, iss := range task.Issues {
		fmt.Fprintf(&b, "- Line %d: %s (%s)\n", iss.Line, iss.Message, iss.Linter)
	}
	b.WriteString("\nPrevious approaches have not resolved these issues.")
	b.WriteString("\n\nPrior attempt output:\n\n")
	b.WriteString(truncateOutput(priorOutput, 2000))
	b.WriteString("\n\nConsider refactoring the surrounding code. The lint issues may stem from a deeper structural problem. ")
	b.WriteString("You may modify adjacent functions or extract code as needed, but do not change observable behavior. Commit nothing.")
	return b.String()
}

// BuildAPIPrompt creates a prompt for API-only providers (no built-in file tools).
// It includes the file content and asks for a unified diff response.
func BuildAPIPrompt(task Task) string {
	var b strings.Builder
	b.WriteString(agentPreamble)
	b.WriteString("Fix the following lint issues in " + task.File + ":\n\n")
	for _, iss := range task.Issues {
		fmt.Fprintf(&b, "- Line %d: %s (%s)\n", iss.Line, iss.Message, iss.Linter)
	}
	b.WriteString("\nHere is the current file content:\n\n")
	b.WriteString(readFileContent(task.Dir, task.File))
	b.WriteString(apiDiffInstructions)
	return b.String()
}

// BuildAPIRetryPrompt creates an API retry prompt with file content and prior output.
func BuildAPIRetryPrompt(task Task, priorOutput string) string {
	var b strings.Builder
	b.WriteString(agentPreamble)
	b.WriteString("Fix the following lint issues in " + task.File + ":\n\n")
	for _, iss := range task.Issues {
		fmt.Fprintf(&b, "- Line %d: %s (%s)\n", iss.Line, iss.Message, iss.Linter)
	}
	b.WriteString("\nA previous attempt did not fully resolve these issues. Here is what was tried:\n\n")
	b.WriteString(truncateOutput(priorOutput, 2000))
	b.WriteString("\n\nHere is the current file content:\n\n")
	b.WriteString(readFileContent(task.Dir, task.File))
	b.WriteString("\n\nTry a different approach. Do not repeat what was already tried.")
	b.WriteString(apiDiffInstructions)
	return b.String()
}

// BuildAPIExplorationPrompt creates an API exploration prompt with file content and prior output.
func BuildAPIExplorationPrompt(task Task, priorOutput string) string {
	var b strings.Builder
	b.WriteString(agentPreamble)
	b.WriteString("Fix the following lint issues in " + task.File + ":\n\n")
	for _, iss := range task.Issues {
		fmt.Fprintf(&b, "- Line %d: %s (%s)\n", iss.Line, iss.Message, iss.Linter)
	}
	b.WriteString("\nPrevious approaches have not resolved these issues.")
	b.WriteString("\n\nPrior attempt output:\n\n")
	b.WriteString(truncateOutput(priorOutput, 2000))
	b.WriteString("\n\nHere is the current file content:\n\n")
	b.WriteString(readFileContent(task.Dir, task.File))
	b.WriteString("\n\nConsider refactoring the surrounding code. The lint issues may stem from a deeper structural problem.")
	b.WriteString(apiDiffInstructions)
	return b.String()
}

const apiDiffInstructions = "\n\nRespond ONLY with a unified diff that resolves these issues. " +
	"Wrap the diff in ```diff and ``` markers. " +
	"Preserve existing behavior. Commit nothing."

// maxFileContentSize caps file content included in API prompts to avoid
// blowing up token limits or producing massive API bills.
const maxFileContentSize = 100 * 1024

// readFileContent reads the target file for inclusion in API prompts.
// Returns an empty placeholder on error so prompts degrade gracefully.
// Files larger than maxFileContentSize are truncated with a notice.
func readFileContent(dir, file string) string {
	data, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return fmt.Sprintf("(could not read %s: %v)\n", file, err)
	}
	truncated := ""
	if len(data) > maxFileContentSize {
		data = data[:maxFileContentSize]
		truncated = fmt.Sprintf("\n(truncated — file exceeds %d bytes)\n", maxFileContentSize)
	}
	return "```\n" + string(data) + "\n```\n" + truncated
}

func truncateOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
