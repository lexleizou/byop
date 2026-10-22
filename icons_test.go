package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strconv"
	"strings"
	"testing"
)

func TestUIiconReferencesExist(t *testing.T) {
	entries, err := parser.ParseDir(token.NewFileSet(), ".", func(info fs.FileInfo) bool {
		name := info.Name()
		return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	iconArgs := map[string][]int{
		"icon":                {0},
		"iconOr":              {0, 1},
		"iconButton":          {3},
		"themeButton":         {3},
		"panelToggleButton":   {3},
		"sessionRowAction":    {3},
		"followUpStyleButton": {3},
		"primaryAction":       {4},
		"choiceButton":        {3},
	}
	for _, parsed := range entries {
		for _, file := range parsed.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				ident, ok := call.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				for _, index := range iconArgs[ident.Name] {
					if index >= len(call.Args) {
						continue
					}
					literal, ok := call.Args[index].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					name, err := strconv.Unquote(literal.Value)
					if err != nil {
						t.Fatalf("unquote icon reference %s: %v", literal.Value, err)
					}
					if iconSet[strings.ToLower(name)] == nil {
						t.Errorf("unknown UI icon %q", name)
					}
				}
				return true
			})
		}
	}

	for _, name := range []string{
		"languages", "monitor", "sun", "moon", "contrast", "activity", "stop",
		"list", "corner-up-left", "chat", "document", "panel-left", "panel-right",
		"pin", "pin-off", "archive", "archive-restore", "undo", "grip-vertical",
		"plus", "x", "package", "chevron-right", "plugins", "automations", "settings", "byop",
	} {
		if iconSet[name] == nil {
			t.Errorf("required P9c icon %q is missing", name)
		}
	}
}
