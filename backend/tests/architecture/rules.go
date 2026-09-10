// Package architecture verifies source boundaries, not runtime authorization or SQL ownership.
package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

const module = "github.com/orz-i/mender/backend/"

var contexts = map[string]bool{
	"identity": true, "catalog": true, "connections": true, "distribution": true,
	"execution": true, "commerce": true, "supply": true, "governance": true,
}

// Pure imports are an explicit allowlist. Adding one requires a boundary review.
var pureImports = map[string]bool{
	"bytes": true, "cmp": true, "context": true, "encoding/hex": true,
	"errors": true, "fmt": true, "math": true, "math/big": true,
	"slices": true, "sort": true, "strconv": true, "strings": true,
	"time": true, "unicode": true, "unicode/utf8": true,
}

type Violation struct {
	File   string
	Line   int
	Rule   string
	Detail string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s:%d [%s] %s", v.File, v.Line, v.Rule, v.Detail)
}

type boundary struct{ context, layer, direction string }

func locate(name string) boundary {
	parts := strings.Split(strings.TrimPrefix(name, module), "/")
	if len(parts) >= 4 && parts[0] == "internal" && parts[1] == "processes" {
		b := boundary{context: "process:" + parts[2], layer: parts[3]}
		if len(parts) >= 5 && b.layer == "adapters" {
			b.direction = parts[4]
		}
		return b
	}
	if len(parts) >= 4 && parts[0] == "internal" && parts[1] == "contexts" {
		b := boundary{context: parts[2], layer: parts[3]}
		if len(parts) >= 5 && b.layer == "adapters" {
			b.direction = parts[4]
		}
		return b
	}
	if strings.HasPrefix(name, "internal/sharedkernel/") || strings.HasPrefix(name, module+"internal/sharedkernel") {
		return boundary{layer: "sharedkernel"}
	}
	return boundary{}
}

func pure(b boundary) bool {
	return b.layer == "domain" || b.layer == "application" || b.layer == "public" || b.layer == "sharedkernel"
}

func allowedLocal(from, to boundary) bool {
	if to.layer == "sharedkernel" {
		return true
	}
	if from.context == "" || to.context == "" {
		return false
	}
	if from.context != to.context {
		return from.layer == "adapters" && from.direction == "outbound" && to.layer == "public"
	}
	switch from.layer {
	case "domain":
		return to.layer == "domain"
	case "application":
		return to.layer == "domain" || to.layer == "application"
	case "public":
		return to.layer == "public"
	case "adapters":
		if from.direction == "inbound" {
			return to.layer == "application" || to.layer == "public" || (to.layer == "adapters" && to.direction == "inbound")
		}
		return to.layer == "domain" || to.layer == "application" || to.layer == "public" || (to.layer == "adapters" && to.direction == "outbound")
	}
	return false
}

// CheckSource also checks inactive build-tag files and generated files. It never executes them.
func CheckSource(filename string, source []byte) []Violation {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, source, parser.AllErrors)
	if err != nil {
		return []Violation{{filename, 1, "GO_PARSE", err.Error()}}
	}
	from := locate(filename)
	var violations []Violation
	add := func(pos token.Pos, rule, detail string) {
		violations = append(violations, Violation{filename, fset.Position(pos).Line, rule, detail})
	}
	if from.context != "" && !contexts[from.context] && from.context != "process:admission" {
		add(file.Pos(), "GO_CONTEXT", "unknown context: "+from.context)
	}
	if from.context != "" && from.layer != "domain" && from.layer != "application" && from.layer != "public" && (from.layer != "adapters" || (from.direction != "inbound" && from.direction != "outbound")) {
		add(file.Pos(), "GO_LAYER", "production code must have an explicit context layer")
	}
	aliases := map[string]string{}
	for _, imp := range file.Imports {
		name, _ := strconv.Unquote(imp.Path.Value)
		alias := path.Base(name)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		aliases[alias] = name
		if pure(from) && alias == "." {
			add(imp.Pos(), "GO_DOT_IMPORT", "dot imports obscure pure-layer dependencies")
		}
		if strings.HasPrefix(name, module) {
			to := locate(name)
			if from.context != "" || from.layer == "sharedkernel" {
				if !allowedLocal(from, to) {
					add(imp.Pos(), "GO_BOUNDARY", "forbidden local dependency: "+name)
				}
			} else if strings.HasPrefix(filename, "internal/platform/") && to.context != "" {
				add(imp.Pos(), "GO_PLATFORM", "platform must not depend on business contexts")
			}
		} else if pure(from) && !pureImports[name] {
			if strings.HasSuffix(filename, "_test.go") && (name == "testing" || name == "reflect") {
				continue
			}
			add(imp.Pos(), "GO_PURE_IMPORT", "non-allowlisted pure-layer dependency: "+name)
		}
	}
	if pure(from) {
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			dep := aliases[id.Name]
			if dep == "time" {
				switch sel.Sel.Name {
				case "Now", "Sleep", "After", "AfterFunc", "NewTimer", "NewTicker", "Tick", "Since", "Until", "Local":
					add(sel.Pos(), "GO_AMBIENT_TIME", "pass time or a clock port instead of reading ambient time")
				}
			}
			if dep == "fmt" && (strings.HasPrefix(sel.Sel.Name, "Print") || strings.HasPrefix(sel.Sel.Name, "Scan")) {
				add(sel.Pos(), "GO_AMBIENT_IO", "console I/O belongs in an adapter")
			}
			return true
		})
	}
	return violations
}

// CheckTree expects a filesystem rooted at backend/. Symlinks are not followed.
func CheckTree(root fs.FS) ([]Violation, error) {
	var violations []Violation
	count := 0
	err := fs.WalkDir(root, "internal", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("unexpected symlink in architecture scan: %s", name)
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			return nil
		}
		count++
		data, err := fs.ReadFile(root, name)
		if err != nil {
			return err
		}
		violations = append(violations, CheckSource(name, data)...)
		return nil
	})
	if err == nil && count == 0 {
		err = fmt.Errorf("architecture scan found no Go source files")
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].File == violations[j].File {
			return violations[i].Line < violations[j].Line
		}
		return violations[i].File < violations[j].File
	})
	return violations, err
}
