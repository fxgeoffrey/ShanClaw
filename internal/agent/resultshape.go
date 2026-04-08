package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

type ResultProfile string

const (
	ResultProfileDefault ResultProfile = "default"
	ResultProfileTree    ResultProfile = "tree"
)

type ShapedResult struct {
	Text      string
	Signature string
}

var treeRefPattern = regexp.MustCompile(`\bref=e\d+\b`)
var treeWhitespacePattern = regexp.MustCompile(`[ \t]+`)

func resultProfileForTool(toolName string) ResultProfile {
	switch toolName {
	case "browser_snapshot":
		return ResultProfileTree
	default:
		return ResultProfileDefault
	}
}

func shapeContextResult(toolName, content string, previous *ShapedResult) ShapedResult {
	switch resultProfileForTool(toolName) {
	case ResultProfileTree:
		return shapeTreeResult(content, previous)
	default:
		return ShapedResult{Text: content}
	}
}

func shapeTreeResult(content string, previous *ShapedResult) ShapedResult {
	normalizedLines := normalizeTreeLines(content)
	if len(normalizedLines) == 0 {
		return ShapedResult{Text: content}
	}

	signature := shortStableHash(strings.Join(normalizedLines, "\n"))
	if len([]rune(content)) < 2000 && len(normalizedLines) < 30 {
		return ShapedResult{Text: content, Signature: signature}
	}

	refCount := 0
	for _, line := range normalizedLines {
		if strings.Contains(line, "ref=*") {
			refCount++
		}
	}

	if previous != nil && previous.Signature == signature {
		return ShapedResult{
			Text:      fmt.Sprintf("[tree snapshot unchanged since last read; signature %s; %d lines; %d refs]", signature, len(normalizedLines), refCount),
			Signature: signature,
		}
	}

	header := fmt.Sprintf("[tree snapshot summary; signature %s; %d lines; %d refs]", signature, len(normalizedLines), refCount)
	if previous != nil && previous.Signature != "" {
		header = fmt.Sprintf("[tree snapshot changed since last read; signature %s; previous %s; %d lines; %d refs]", signature, previous.Signature, len(normalizedLines), refCount)
	}

	excerpt := buildTreeExcerpt(normalizedLines, 18, 1600)
	if excerpt == "" {
		return ShapedResult{Text: header, Signature: signature}
	}
	return ShapedResult{
		Text:      header + "\n" + excerpt,
		Signature: signature,
	}
}

func normalizeTreeLines(content string) []string {
	lines := strings.Split(content, "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		line = treeRefPattern.ReplaceAllString(line, "ref=*")
		line = treeWhitespacePattern.ReplaceAllString(line, " ")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		normalized = append(normalized, line)
	}
	return normalized
}

func buildTreeExcerpt(lines []string, maxLines, maxChars int) string {
	if len(lines) == 0 || maxLines <= 0 || maxChars <= 0 {
		return ""
	}

	var excerpt []string
	chars := 0
	for _, line := range lines {
		if len(excerpt) >= maxLines {
			break
		}
		if len([]rune(line)) > 140 {
			runes := []rune(line)
			line = string(runes[:140]) + "..."
		}
		nextChars := chars + len([]rune(line))
		if len(excerpt) > 0 {
			nextChars++
		}
		if nextChars > maxChars {
			break
		}
		excerpt = append(excerpt, line)
		chars = nextChars
	}
	return strings.Join(excerpt, "\n")
}

func shortStableHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:12]
}
