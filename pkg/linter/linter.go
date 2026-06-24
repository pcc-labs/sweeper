package linter

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Issue struct {
	File    string
	Line    int
	Col     int
	Message string
	Linter  string
}

type ParseResult struct {
	Issues    []Issue
	RawOutput string
	Parsed    bool
}

var (
	// golangci-lint format: file:line:col: message (linter)
	// Linter name supports @-scoped rules like @typescript-eslint/no-unused-vars
	golangciPattern = regexp.MustCompile(`^(.+?):(\d+):(\d+):\s+(.+)\s+\(([@\w][\w./@-]*)\)$`)
	// ESLint stylish issue line: "  line:col  error|warning  message  rule-name"
	eslintStylishIssue = regexp.MustCompile(`^\s+(\d+):(\d+)\s+(error|warning)\s+(.+?)\s{2,}(\S+)\s*$`)
	// generic file:line:col: message
	genericPattern = regexp.MustCompile(`^(.+?):(\d+):(\d+):\s+(.+)$`)
	// minimal file:line: message
	minimalPattern = regexp.MustCompile(`^(.+?):(\d+):\s+(.+)$`)
)

func ParseOutput(raw string) ParseResult {
	result := ParseResult{RawOutput: raw}

	// Try ESLint stylish (multi-line block) format first
	if issues := parseESLintStylish(raw); len(issues) > 0 {
		result.Issues = issues
		result.Parsed = true
		return result
	}

	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if iss, ok := parseGolangci(line); ok {
			result.Issues = append(result.Issues, iss)
			continue
		}
		if iss, ok := parseGeneric(line); ok {
			result.Issues = append(result.Issues, iss)
			continue
		}
		if iss, ok := parseMinimal(line); ok {
			result.Issues = append(result.Issues, iss)
			continue
		}
	}
	result.Parsed = len(result.Issues) > 0
	return result
}

// parseESLintStylish parses ESLint's default "stylish" multi-line format:
//
//	/path/to/file.js
//	   2:10  error  'foo' is not defined  no-undef
//	   5:1   warning  Unexpected console statement  no-console
//
//	✖ 2 problems (1 error, 1 warning)
func parseESLintStylish(raw string) []Issue {
	var issues []Issue
	var currentFile string

	for _, line := range strings.Split(raw, "\n") {
		// Skip empty lines and summary lines (e.g. "✖ 2 problems...")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(line, "problem") && (strings.Contains(line, "✖") || strings.Contains(line, "error") && strings.Contains(line, "warning")) {
			continue
		}

		// Issue line: indented with "line:col  severity  message  rule"
		if m := eslintStylishIssue.FindStringSubmatch(line); m != nil {
			if currentFile == "" {
				continue
			}
			lineNum, _ := strconv.Atoi(m[1])
			col, _ := strconv.Atoi(m[2])
			issues = append(issues, Issue{
				File:    currentFile,
				Line:    lineNum,
				Col:     col,
				Message: strings.TrimSpace(m[4]),
				Linter:  m[5],
			})
			continue
		}

		// File header: non-indented line that looks like a path
		trimmed := strings.TrimSpace(line)
		if line == trimmed && len(trimmed) > 0 && !strings.HasPrefix(trimmed, "✖") {
			currentFile = trimmed
		}
	}

	return issues
}

func parseGolangci(line string) (Issue, bool) {
	m := golangciPattern.FindStringSubmatch(line)
	if m == nil {
		return Issue{}, false
	}
	lineNum, _ := strconv.Atoi(m[2])
	col, _ := strconv.Atoi(m[3])
	return Issue{
		File:    m[1],
		Line:    lineNum,
		Col:     col,
		Message: m[4],
		Linter:  m[5],
	}, true
}

func parseGeneric(line string) (Issue, bool) {
	m := genericPattern.FindStringSubmatch(line)
	if m == nil {
		return Issue{}, false
	}
	lineNum, _ := strconv.Atoi(m[2])
	col, _ := strconv.Atoi(m[3])
	return Issue{
		File:    m[1],
		Line:    lineNum,
		Col:     col,
		Message: m[4],
		Linter:  "custom",
	}, true
}

func parseMinimal(line string) (Issue, bool) {
	m := minimalPattern.FindStringSubmatch(line)
	if m == nil {
		return Issue{}, false
	}
	msg := m[3]
	// Skip go compiler package headers (e.g. ": # testproject") and
	// malformed messages that start with ": " (mangled column parse).
	if strings.HasPrefix(msg, ": ") || strings.HasPrefix(msg, "# ") {
		return Issue{}, false
	}
	lineNum, _ := strconv.Atoi(m[2])
	return Issue{
		File:    m[1],
		Line:    lineNum,
		Message: msg,
		Linter:  "custom",
	}, true
}

func RunCommand(ctx context.Context, dir string, cmd []string) (ParseResult, error) {
	c := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return ParseResult{}, fmt.Errorf("running %s: %w", cmd[0], err)
		}
	}
	result := ParseOutput(string(out))
	normalizeIssuePaths(result.Issues, dir)
	return result, nil
}

// normalizeIssuePaths resolves parsed file paths relative to dir.
// Linters sometimes emit absolute or CWD-relative paths for compile errors
// (e.g. "../../../tmp/project/main.go") even when run from dir. This
// normalizes them so they're consistent with the target directory.
func normalizeIssuePaths(issues []Issue, dir string) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	for i := range issues {
		f := issues[i].File
		// Resolve the file path against the command's working dir.
		absFile := f
		if !filepath.IsAbs(f) {
			absFile = filepath.Join(absDir, f)
		}
		absFile = filepath.Clean(absFile)
		// Make it relative to dir.
		if rel, err := filepath.Rel(absDir, absFile); err == nil {
			issues[i].File = rel
		}
	}
}

func Run(ctx context.Context, dir string) (ParseResult, error) {
	return RunCommand(ctx, dir, []string{"golangci-lint", "run", "./..."})
}
