package main

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	uiUnixPathPattern    = regexp.MustCompile(`(^|[\s"'(\[=,])(file://(?:/[A-Za-z]:)?/[^\s:"')\],]+|/[^\s:"')\],]+)`)
	uiWindowsPathPattern = regexp.MustCompile(`(^|[\s"'(\[=,])([A-Za-z]:\\[^\s:"')\],]+)`)
)

func uiErrorText(err error, fallbackText string) string {
	if err == nil {
		return fallbackText
	}
	text := strings.TrimSpace(err.Error())
	if text == "" {
		return fallbackText
	}
	text = uiUnixPathPattern.ReplaceAllStringFunc(text, uiRedactPathMatch)
	text = uiWindowsPathPattern.ReplaceAllStringFunc(text, uiRedactPathMatch)
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
	base := filepath.Base(value)
	if index := strings.LastIndexAny(value, `/\`); index >= 0 {
		base = value[index+1:]
	}
	if base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return "local file"
}

func uiRedactPathMatch(value string) string {
	if value == "" {
		return value
	}
	if strings.HasPrefix(value, "file://") {
		return uiPathLabel(value)
	}
	if value[0] == '/' || (len(value) > 1 && value[1] == ':') {
		return uiPathLabel(value)
	}
	return value[:1] + uiPathLabel(value[1:])
}

func uiErrorFromText(text, fallbackText string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return fallbackText
	}
	return uiErrorText(errors.New(text), fallbackText)
}
