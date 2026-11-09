// Command docscheck verifies that the operator documentation matches the
// repository. It checks three things:
//
//   - every relative Markdown link resolves to a file or directory;
//   - every repository path named in the docs exists;
//   - every `make <target>` and `virtlab <command>` named in code samples is
//     defined by the Makefile or by the CLI command switch.
//
// Run it from the repository root. It exits non-zero and prints one line per
// problem when a reference does not resolve.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	docsDir  = "docs"
	rulesDir = ".agents"
)

// repoPathPrefixes are the repository-relative directories that the docs may
// name. Build outputs (bin, dist) and runtime files (state, configs) are not
// checked because they do not exist in a fresh checkout.
var repoPathPrefixes = []string{
	".agents/",
	"cmd/",
	"docs/",
	"examples/",
	"hack/",
	"internal/",
	"scripts/",
	"sdks/",
	".github/",
}

// repoFiles are the repository-root files that the docs may name.
var repoFiles = []string{
	".gitignore",
	".goreleaser.yaml",
	"AGENTS.md",
	"CLAUDE.md",
	"LICENSE",
	"Makefile",
	"README.md",
	"SECURITY.md",
	"go.mod",
	"go.sum",
}

var (
	linkRe     = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
	refLinkRe  = regexp.MustCompile(`(?m)^\s*\[[^\]]+\]:\s*(\S+)`)
	inlineRe   = regexp.MustCompile("`([^`]+)`")
	makeRe     = regexp.MustCompile(`\bmake\s+([A-Za-z0-9_.-]+)`)
	virtlabRe  = regexp.MustCompile(`(?:^|[\s;&|(])(?:\./)?virtlab\s+([A-Za-z0-9_./:-]+)`)
	pathRe     = regexp.MustCompile(`(?:^|[^A-Za-z0-9._/-])((?:` + strings.Join(quoteAll(repoPathPrefixes), "|") + `)[A-Za-z0-9._/-]+)`)
	fileRe     = regexp.MustCompile(`(?:^|[^A-Za-z0-9._/-])(` + strings.Join(quoteAll(repoFiles), "|") + `)(?:[^A-Za-z0-9._/-]|$)`)
	caseRe     = regexp.MustCompile(`(?m)^\s*case\s+(.+):\s*$`)
	quotedRe   = regexp.MustCompile(`"([^"]*)"`)
	fenceStart = regexp.MustCompile("^\\s*(```|~~~)\\s*([A-Za-z0-9_+-]*)")
)

// nonShellFences hold fences whose content is not a command sample. Commands
// are still checked in unlabeled fences and in shell fences.
var nonShellFences = map[string]bool{
	"diff":    true,
	"html":    true,
	"ini":     true,
	"json":    true,
	"mermaid": true,
	"patch":   true,
	"text":    true,
	"toml":    true,
	"xml":     true,
	"yaml":    true,
	"yml":     true,
}

type finding struct {
	file string
	line int
	msg  string
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		fatalf("cannot determine working directory: %v", err)
	}

	docs, err := docFiles(root)
	if err != nil {
		fatalf("%v", err)
	}
	if len(docs) == 0 {
		fatalf("no Markdown files found under %q", docsDir)
	}

	makeTargets, err := makefileTargets(filepath.Join(root, "Makefile"))
	if err != nil {
		fatalf("%v", err)
	}
	commands, err := virtlabCommands(filepath.Join(root, "cmd", "virtlab", "main.go"))
	if err != nil {
		fatalf("%v", err)
	}

	var findings []finding
	for _, doc := range docs {
		rel, err := filepath.Rel(root, doc)
		if err != nil {
			rel = doc
		}
		items, err := checkFile(root, rel, doc, makeTargets, commands)
		if err != nil {
			fatalf("%v", err)
		}
		findings = append(findings, items...)
	}

	ruleFindings, err := checkRules(root)
	if err != nil {
		fatalf("%v", err)
	}
	findings = append(findings, ruleFindings...)

	if len(findings) == 0 {
		fmt.Printf("docs check: %d Markdown files OK\n", len(docs))
		return
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].file != findings[j].file {
			return findings[i].file < findings[j].file
		}
		return findings[i].line < findings[j].line
	})
	for _, f := range findings {
		fmt.Printf("%s:%d: %s\n", f.file, f.line, f.msg)
	}
	fmt.Printf("\ndocs check: %d problem(s) found\n", len(findings))
	os.Exit(1)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "docs check: "+format+"\n", args...)
	os.Exit(2)
}

// quoteAll wraps each value in a regular expression non-capturing group and
// escapes it for literal matching.
func quoteAll(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = regexp.QuoteMeta(v)
	}
	return out
}

func docFiles(root string) ([]string, error) {
	var files []string
	for _, dir := range []string{docsDir, rulesDir} {
		dirRoot := filepath.Join(root, dir)
		if _, err := os.Stat(dirRoot); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", dir, err)
		}
		err := filepath.WalkDir(dirRoot, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ".md") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", dir, err)
		}
	}

	for _, name := range []string{"README.md", "AGENTS.md", "CLAUDE.md", "SECURITY.md"} {
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); err == nil {
			files = append(files, path)
		}
	}

	sort.Strings(files)
	return files, nil
}

// checkRules verifies that every rule under .agents/rules is listed in the
// rule index and has the frontmatter that makes it discoverable.
func checkRules(root string) ([]finding, error) {
	dir := filepath.Join(root, rulesDir, "rules")
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat %s: %w", dir, err)
	}

	indexRel := filepath.Join(rulesDir, "rules", "README.md")
	index, err := os.ReadFile(filepath.Join(root, indexRel))
	if err != nil {
		return []finding{{file: indexRel, line: 1, msg: "missing rule index"}}, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	var findings []finding
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(name), ".md") || name == "README.md" {
			continue
		}
		rel := filepath.Join(rulesDir, "rules", name)
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", rel, err)
		}
		if !strings.Contains(string(index), name) {
			findings = append(findings, finding{file: rel, line: 1, msg: "rule is not listed in " + indexRel})
		}
		for _, field := range []string{"name", "description"} {
			if !hasFrontmatterField(data, field) {
				findings = append(findings, finding{file: rel, line: 1, msg: "rule is missing frontmatter field: " + field})
			}
		}
	}
	return findings, nil
}

// hasFrontmatterField reports whether the leading YAML frontmatter block
// contains the given top-level field.
func hasFrontmatterField(data []byte, field string) bool {
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return false
	}
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			return false
		}
		if strings.HasPrefix(trimmed, field+":") {
			return true
		}
	}
	return false
}

func makefileTargets(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	targets := make(map[string]bool)
	targetRe := regexp.MustCompile(`^([A-Za-z0-9_.-]+):`)
	for _, line := range strings.Split(string(data), "\n") {
		if m := targetRe.FindStringSubmatch(line); m != nil {
			targets[m[1]] = true
		}
	}
	return targets, nil
}

// virtlabCommands returns the subcommand literals of the command switch in
// cmd/virtlab/main.go. The switch is the source of truth for the CLI surface.
func virtlabCommands(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	commands := make(map[string]bool)
	for _, m := range caseRe.FindAllStringSubmatch(string(data), -1) {
		for _, q := range quotedRe.FindAllStringSubmatch(m[1], -1) {
			if q[1] != "" {
				commands[q[1]] = true
			}
		}
	}
	if len(commands) == 0 {
		return nil, fmt.Errorf("no subcommands found in %s", path)
	}
	return commands, nil
}

func checkFile(root, rel, path string, makeTargets, commands map[string]bool) ([]finding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	seen := make(map[string]bool)
	var findings []finding
	report := func(line int, msg string) {
		key := fmt.Sprintf("%d|%s", line, msg)
		if seen[key] {
			return
		}
		seen[key] = true
		findings = append(findings, finding{file: rel, line: line, msg: msg})
	}

	inFence := false
	checkCommands := true
	var fence string
	for i, line := range strings.Split(string(data), "\n") {
		lineNo := i + 1
		if m := fenceStart.FindStringSubmatch(line); m != nil {
			if !inFence {
				inFence, fence = true, m[1]
				checkCommands = !nonShellFences[strings.ToLower(m[2])]
				continue
			}
			if m[1] == fence {
				inFence, fence, checkCommands = false, "", true
				continue
			}
		}

		if inFence {
			checkCode(root, line, func(msg string) { report(lineNo, msg) }, makeTargets, commands, checkCommands)
			continue
		}

		// Outside fences, remove inline code before parsing Markdown links so a
		// code sample that contains brackets is not treated as a link.
		withoutCode := inlineRe.ReplaceAllString(line, "")
		for _, m := range linkRe.FindAllStringSubmatch(withoutCode, -1) {
			checkLink(root, rel, m[1], func(msg string) { report(lineNo, msg) })
		}
		for _, m := range refLinkRe.FindAllStringSubmatch(line, -1) {
			checkLink(root, rel, m[1], func(msg string) { report(lineNo, msg) })
		}

		// Repository paths may appear in prose, tables, or inline code.
		checkPaths(root, line, func(msg string) { report(lineNo, msg) })

		// Commands and targets are only meaningful inside code samples; plain
		// prose uses the same words in ordinary sentences.
		for _, m := range inlineRe.FindAllStringSubmatch(line, -1) {
			checkCode(root, m[1], func(msg string) { report(lineNo, msg) }, makeTargets, commands, true)
		}
	}

	return findings, nil
}

func checkLink(root, rel, target string, report func(string)) {
	target = strings.TrimSpace(target)
	if target == "" {
		return
	}
	// A Markdown link may carry an optional title after the destination.
	if i := strings.IndexAny(target, " \t"); i >= 0 {
		target = target[:i]
	}
	target = strings.Trim(target, "<>")
	if target == "" ||
		strings.HasPrefix(target, "#") ||
		strings.HasPrefix(target, "/") ||
		strings.Contains(target, "://") ||
		strings.HasPrefix(target, "mailto:") ||
		strings.HasPrefix(target, "tel:") {
		return
	}
	if i := strings.IndexAny(target, "#?"); i >= 0 {
		target = target[:i]
	}
	if target == "" {
		return
	}

	resolved := filepath.Join(filepath.Dir(filepath.Join(root, rel)), target)
	if _, err := os.Stat(resolved); err != nil {
		report(fmt.Sprintf("broken link: %s", target))
	}
}

func checkCode(root, text string, report func(string), makeTargets, commands map[string]bool, checkCommands bool) {
	checkPaths(root, text, report)

	if !checkCommands {
		return
	}

	for _, m := range makeRe.FindAllStringSubmatch(text, -1) {
		target := m[1]
		if strings.HasPrefix(target, "-") {
			continue
		}
		if !makeTargets[target] {
			report(fmt.Sprintf("unknown Make target %q", target))
		}
	}

	for _, m := range virtlabRe.FindAllStringSubmatch(text, -1) {
		token := strings.TrimRight(m[1], ".,;:)")
		if token == "" || strings.HasPrefix(token, "-") || strings.HasPrefix(token, "/") ||
			strings.HasPrefix(token, ".") || strings.Contains(token, "=") {
			continue
		}
		for _, command := range strings.Split(token, "/") {
			if command == "" || strings.Contains(command, ":") {
				continue
			}
			if !commands[command] {
				report(fmt.Sprintf("unknown virtlab command %q", command))
			}
		}
	}
}

func checkPaths(root, text string, report func(string)) {
	for _, m := range pathRe.FindAllStringSubmatch(text, -1) {
		candidate := strings.TrimRight(m[1], ".,;:)`'\"")
		if candidate == "" || hasPlaceholder(candidate) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, candidate)); err != nil {
			report(fmt.Sprintf("path does not exist: %s", candidate))
		}
	}

	for _, m := range fileRe.FindAllStringSubmatch(text, -1) {
		candidate := m[1]
		if _, err := os.Stat(filepath.Join(root, candidate)); err != nil {
			report(fmt.Sprintf("path does not exist: %s", candidate))
		}
	}
}

func hasPlaceholder(s string) bool {
	return strings.ContainsAny(s, "<>{}*$")
}
