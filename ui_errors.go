package main

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	uiUnixPathPattern    = regexp.MustCompile(`(?:file://)?/[^:\r\n]*`)
	uiWindowsPathPattern = regexp.MustCompile(`[A-Za-z]:\\[^:\r\n]*`)
)

func uiErrorText(err error, fallbackText string) string {
	if err == nil {
		return fallbackText
	}
	text := strings.TrimSpace(err.Error())
	if text == "" {
		return fallbackText
	}
	text = uiUnixPathPattern.ReplaceAllStringFunc(text, uiPathLabel)
	text = uiWindowsPathPattern.ReplaceAllStringFunc(text, uiPathLabel)
	text = strings.TrimSpace(text)
	if text == "" {
		return fallbackText
	}
	return text
}

func uiErrorSummary(err error, fallbackText string, limit int) string {
	return summarizeLine(uiErrorText(err, fallbackText), limit)
}

func uiPathLabel(value string) string {
	value = strings.TrimPrefix(value, "file://")
	value = strings.TrimRight(value, `"'()[]{}.,;`)
	if base := filepath.Base(value); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return "local file"
}

func uiErrorFromText(text, fallbackText string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return fallbackText
	}
	return uiErrorText(errors.New(text), fallbackText)
}
