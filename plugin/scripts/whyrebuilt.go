// Command whyrebuilt explains why a Go build or test run missed the build
// cache, by comparing the cache keys of two runs.
//
// The go command looks up every compile, vet and link step in GOCACHE by a
// key (the action ID) hashed from that step's inputs: the Go version, the
// package directory, compiler flags, cgo settings, source file contents and
// the outputs of its dependencies. With GODEBUG=gocachehash=1 it prints every
// input it hashes. whyrebuilt records that output for a run, and compares two
// records to name the inputs that changed, separating the root causes from
// the steps that changed only because a dependency did.
//
// It uses only the standard library, so `go run` needs no setup.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const version = "0.1.0"

// Exit codes.
const (
	exitOK        = 0
	exitDiffer    = 1 // --fail and the runs' keys differ
	exitCantCheck = 2 // input can't be trusted, or usage error
)

// ---------------------------------------------------------------------------
// Record file format
//
//	# whyrebuilt record v1
//	# command: ["go","build","./..."]
//	# dir: "/abs/dir"
//	# start: 2026-11-06T10:00:00.123456789Z
//	# gocache: "/home/u/.cache/go-build" entries 1234
//	# begin
//	...every stderr line of the command, verbatim...
//	# end exit 0 lines 4567 entries 1300
//
// The end line must be the last line. A file without it was cut off, and a
// line count that doesn't match means lines were lost or added.

const (
	headerLine = "# whyrebuilt record v1"
	beginLine  = "# begin"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func usage(w io.Writer) {
	fmt.Fprint(w, `whyrebuilt explains why a Go build missed the build cache, by comparing the cache keys of two runs.

Usage:
  whyrebuilt record -o FILE [--] COMMAND [ARGS...]
      Run a go command with GODEBUG=gocachehash=1 and save its cache keys to FILE.
      Stdout passes through. Exits with the command's exit code.
  whyrebuilt compare [--fail] [--json] [--limit N] A B
      Compare two records and explain every key that differs.
      --fail exits 1 when any key differs or an action is in only one run.
  whyrebuilt version

Exit codes: 0 compared, 1 differences with --fail, 2 a record can't be trusted.
`)
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return exitCantCheck
	}
	switch args[0] {
	case "record":
		return cmdRecord(args[1:], stderr)
	case "compare":
		return cmdCompare(args[1:], stdout, stderr)
	case "version", "--version", "-version":
		fmt.Fprintln(stdout, "whyrebuilt", version)
		return exitOK
	case "help", "-h", "--help", "-help":
		usage(stdout)
		return exitOK
	}
	fmt.Fprintf(stderr, "whyrebuilt: unknown command %q\n\n", args[0])
	usage(stderr)
	return exitCantCheck
}

// ---------------------------------------------------------------------------
// record

func cmdRecord(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("o", "", "write the record to `FILE`")
	if err := fs.Parse(args); err != nil {
		return exitCantCheck
	}
	command := fs.Args()
	if *out == "" || len(command) == 0 {
		fmt.Fprintln(stderr, "whyrebuilt record: usage: whyrebuilt record -o FILE [--] COMMAND [ARGS...]")
		return exitCantCheck
	}
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "whyrebuilt record:", err)
		return exitCantCheck
	}
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(stderr, "whyrebuilt record:", err)
		return exitCantCheck
	}
	defer f.Close()
	var sink io.Writer = f
	var zw *gzip.Writer
	if strings.HasSuffix(*out, ".gz") {
		zw = gzip.NewWriter(f)
		sink = zw
	}
	w := bufio.NewWriterSize(sink, 1<<20)

	env := withGodebug(os.Environ())
	gocache := goEnv(env, "GOCACHE")
	before := countCacheEntries(gocache)

	cmdJSON, _ := json.Marshal(command)
	dirJSON, _ := json.Marshal(dir)
	cacheJSON, _ := json.Marshal(gocache)
	fmt.Fprintln(w, headerLine)
	fmt.Fprintf(w, "# command: %s\n", cmdJSON)
	fmt.Fprintf(w, "# dir: %s\n", dirJSON)
	fmt.Fprintf(w, "# start: %s\n", time.Now().UTC().Format(time.RFC3339Nano))
	fmt.Fprintf(w, "# gocache: %s entries %d\n", cacheJSON, before)
	fmt.Fprintln(w, beginLine)

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	pipe, err := cmd.StderrPipe()
	if err != nil {
		fmt.Fprintln(stderr, "whyrebuilt record:", err)
		return exitCantCheck
	}
	lines := 0
	exit := 0
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(stderr, "whyrebuilt record:", err)
		exit = 127
	} else {
		r := bufio.NewReaderSize(pipe, 1<<20)
		for {
			line, rerr := r.ReadString('\n')
			if line != "" {
				line = strings.TrimSuffix(line, "\n")
				if keep(line) {
					w.WriteString(line)
					w.WriteByte('\n')
					lines++
				}
				// Show the command's own messages, such as build errors.
				if !strings.HasPrefix(line, "HASH") {
					fmt.Fprintln(stderr, line)
				}
			}
			if rerr != nil {
				break
			}
		}
		if err := cmd.Wait(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				exit = ee.ExitCode()
				if exit < 0 {
					exit = 128 // killed by a signal
				}
			} else {
				fmt.Fprintln(stderr, "whyrebuilt record:", err)
				exit = 127
			}
		}
	}
	after := countCacheEntries(gocache)
	fmt.Fprintf(w, "# end exit %d lines %d entries %d\n", exit, lines, after)
	if err := w.Flush(); err != nil {
		fmt.Fprintln(stderr, "whyrebuilt record:", err)
		return exitCantCheck
	}
	if zw != nil {
		if err := zw.Close(); err != nil {
			fmt.Fprintln(stderr, "whyrebuilt record:", err)
			return exitCantCheck
		}
	}
	if err := f.Close(); err != nil {
		fmt.Fprintln(stderr, "whyrebuilt record:", err)
		return exitCantCheck
	}
	if exit == 0 && !hasHashLines(*out) {
		fmt.Fprintf(stderr, "whyrebuilt record: %s has no cache keys; was the command a go command, and did it keep GODEBUG?\n", *out)
	}
	return exit
}

// keep reports whether record saves a stderr line: the command's own
// messages and the inputs of compile, vet and link keys. Other hashes, such
// as the module index and file sums, are dropped to keep records small.
func keep(line string) bool {
	if !strings.HasPrefix(line, "HASH") {
		return true
	}
	rest, ok := strings.CutPrefix(line, "HASH[")
	if !ok {
		return false
	}
	end := strings.Index(rest, "]: ")
	if end < 0 {
		end = strings.LastIndex(rest, "]")
	}
	return end >= 0 && tracked(rest[:end])
}

// withGodebug adds gocachehash=1 to GODEBUG, keeping any settings already there.
func withGodebug(env []string) []string {
	out := make([]string, 0, len(env)+1)
	found := false
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "GODEBUG="); ok {
			found = true
			if v != "" {
				v += ","
			}
			kv = "GODEBUG=" + v + "gocachehash=1"
		}
		out = append(out, kv)
	}
	if !found {
		out = append(out, "GODEBUG=gocachehash=1")
	}
	return out
}

// goEnv asks the go command for one setting, with gocachehash turned off so
// that it prints nothing else. It returns "" if go can't be run.
func goEnv(env []string, name string) string {
	clean := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, "GODEBUG=") {
			clean = append(clean, kv)
		}
	}
	cmd := exec.Command("go", "env", name)
	cmd.Env = clean
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// countCacheEntries counts the action entries ("-a" files) in GOCACHE, or
// returns -1 if the directory can't be read.
func countCacheEntries(dir string) int {
	if dir == "" || dir == "off" {
		return -1
	}
	subs, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0 // the go command creates it: the cache starts empty
	}
	if err != nil {
		return -1
	}
	n := 0
	for _, s := range subs {
		if !s.IsDir() || len(s.Name()) != 2 {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, s.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if strings.HasSuffix(f.Name(), "-a") {
				n++
			}
		}
	}
	return n
}

func hasHashLines(path string) bool {
	data, err := readFile(path)
	if err != nil {
		return false
	}
	r := bufio.NewReaderSize(strings.NewReader(data), 1<<20)
	for {
		line, err := r.ReadString('\n')
		if strings.HasPrefix(line, "HASH[") {
			return true
		}
		if err != nil {
			return false
		}
	}
}

// ---------------------------------------------------------------------------
// Parsing a record

// Record is one parsed run.
type Record struct {
	Path          string               `json:"path"`
	Command       []string             `json:"command"`
	Dir           string               `json:"dir"`
	Start         string               `json:"start"`
	GoCache       string               `json:"gocache"`
	EntriesBefore int                  `json:"cache_entries_before"`
	EntriesAfter  int                  `json:"cache_entries_after"`
	Exit          int                  `json:"exit"`
	Lines         int                  `json:"lines"`
	GoVersions    []string             `json:"go_versions"`
	OtherLines    int                  `json:"other_lines"`
	Actions       []*Action            `json:"-"`
	ByName        map[string][]*Action `json:"-"`
	Ambiguous     map[string]bool      `json:"-"`
}

// Action is one cache key: a compile ("build"), "vet" or "link" step.
type Action struct {
	Name   string   // "build example.com/m/lib"
	Salt   string   // first input: the go command's version
	Inputs []string // the other inputs, in order, without trailing newline
	Key    string   // hex action ID
	open   bool
}

// Kind is the first word of the name: build, vet or link.
func (a *Action) Kind() string {
	k, _, _ := strings.Cut(a.Name, " ")
	return k
}

// Pkg is the import path in the name.
func (a *Action) Pkg() string {
	_, p, _ := strings.Cut(a.Name, " ")
	return p
}

// trackedKinds are the actions whose names identify a package. Other hashes
// (testResult, testInputs, moduleIndex, ...) share one name across many
// computations and are not compared.
var trackedKinds = map[string]bool{"build": true, "vet": true, "link": true}

// ParseError says why a record can't be trusted.
type ParseError struct {
	Path string
	Msg  string
}

func (e *ParseError) Error() string { return e.Path + ": " + e.Msg }

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// errorLine matches a compiler, assembler, cgo or vet error position, such
// as "a/a.go:2:23: cannot use ...".
var errorLine = regexp.MustCompile(`^\S+\.(go|s|c|h|cc|cpp|m):\d+(:\d+)?: `)

// ReadRecord parses and checks a record file.
func ReadRecord(path string) (*Record, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, err
	}
	return ParseRecord(path, data)
}

// readFile reads a file, decompressing it if it is gzip. A damaged or
// cut-off gzip stream is an error.
func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		return string(data), nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", &ParseError{Path: path, Msg: "damaged gzip file: " + err.Error()}
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		return "", &ParseError{Path: path, Msg: "damaged or cut-off gzip file: " + err.Error()}
	}
	return string(out), nil
}

// ParseRecord parses and checks record text. Anything that could hide a
// failed, cut-off or edited run is an error.
func ParseRecord(path, data string) (*Record, error) {
	bad := func(format string, a ...any) error {
		return &ParseError{Path: path, Msg: fmt.Sprintf(format, a...)}
	}
	if data == "" {
		return nil, bad("empty file")
	}
	if !strings.HasPrefix(data, headerLine+"\n") {
		if strings.Contains(data, "HASH[") {
			return nil, bad("not a whyrebuilt record: it looks like raw GODEBUG=gocachehash=1 output, which can't show whether the run finished or succeeded. Record the run with: whyrebuilt record -o FILE -- go build ./...")
		}
		return nil, bad("not a whyrebuilt record (no %q first line). Record a run with: whyrebuilt record -o FILE -- go build ./...", headerLine)
	}
	if !strings.HasSuffix(data, "\n") {
		return nil, bad("the file is cut off: its last line is incomplete")
	}
	lines := strings.Split(strings.TrimSuffix(data, "\n"), "\n")
	r := &Record{Path: path, ByName: map[string][]*Action{}, Ambiguous: map[string]bool{}}

	// Header.
	i := 1
	field := func(prefix string) (string, error) {
		if i >= len(lines) || !strings.HasPrefix(lines[i], prefix) {
			return "", bad("the header is damaged: expected a %q line", strings.TrimSpace(prefix))
		}
		v := strings.TrimPrefix(lines[i], prefix)
		i++
		return v, nil
	}
	v, err := field("# command: ")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(v), &r.Command); err != nil || len(r.Command) == 0 {
		return nil, bad("the header is damaged: bad command line")
	}
	if v, err = field("# dir: "); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(v), &r.Dir); err != nil {
		return nil, bad("the header is damaged: bad dir line")
	}
	if r.Start, err = field("# start: "); err != nil {
		return nil, err
	}
	if v, err = field("# gocache: "); err != nil {
		return nil, err
	}
	cachePath, entries, ok := strings.Cut(v, " entries ")
	if !ok || json.Unmarshal([]byte(cachePath), &r.GoCache) != nil {
		return nil, bad("the header is damaged: bad gocache line")
	}
	if r.EntriesBefore, err = strconv.Atoi(entries); err != nil {
		return nil, bad("the header is damaged: bad gocache entry count")
	}
	if i >= len(lines) || lines[i] != beginLine {
		return nil, bad("the header is damaged: no %q line", beginLine)
	}
	i++

	// Footer.
	last := lines[len(lines)-1]
	if len(lines)-1 < i || !strings.HasPrefix(last, "# end exit ") {
		return nil, bad("the file is cut off: it has no end line, so the run may not have finished")
	}
	var nlines int
	if _, err := fmt.Sscanf(last, "# end exit %d lines %d entries %d", &r.Exit, &nlines, &r.EntriesAfter); err != nil {
		return nil, bad("the end line is damaged: %q", last)
	}
	body := lines[i : len(lines)-1]
	if len(body) != nlines {
		return nil, bad("the record has %d output lines but its end line says %d: lines were lost, added or joined", len(body), nlines)
	}
	r.Lines = nlines

	// Body.
	versions := map[string]bool{}
	firstError := ""
	for n, line := range body {
		lineNo := i + n + 1
		if !strings.HasPrefix(line, "HASH") {
			r.OtherLines++
			if firstError == "" && errorLine.MatchString(line) && !strings.Contains(line, "warning:") {
				firstError = fmt.Sprintf("line %d is a compiler or vet error: %.200s", lineNo, line)
			}
			continue
		}
		if !strings.HasPrefix(line, "HASH[") {
			// "HASH subkey ..." and "HASH /path: sum" lines need no comparing.
			continue
		}
		rest := line[len("HASH["):]
		if j := strings.Index(rest, "]: "); j >= 0 {
			name, val := rest[:j], rest[j+3:]
			if strings.HasPrefix(val, `"`) {
				s, err := strconv.Unquote(val)
				if err != nil {
					return nil, bad("line %d is damaged: %.80q", lineNo, line)
				}
				if err := r.write(name, s); err != nil {
					return nil, bad("line %d: %v", lineNo, err)
				}
				continue
			}
			if isHex64(val) {
				if err := r.sum(name, val); err != nil {
					return nil, bad("line %d: %v", lineNo, err)
				}
				continue
			}
			return nil, bad("line %d is damaged: %.80q", lineNo, line)
		}
		if strings.HasSuffix(rest, "]") {
			r.start(strings.TrimSuffix(rest, "]"))
			continue
		}
		return nil, bad("line %d is damaged: %.80q", lineNo, line)
	}
	for _, a := range r.Actions {
		if a.open {
			return nil, bad("the key for %q was never finished: the run was cut off or the file was edited", a.Name)
		}
		if a.Salt != "" {
			versions[a.Salt] = true
		}
	}
	for v := range versions {
		r.GoVersions = append(r.GoVersions, v)
	}
	sort.Strings(r.GoVersions)

	if r.Exit != 0 {
		msg := fmt.Sprintf("the recorded command %s failed (exit %d). A failed run can stop early, so its keys can't be compared. Fix the build and record again", commandLine(r.Command), r.Exit)
		for _, a := range r.Command {
			if a == "test" {
				msg += ", or record go test -run '^$' to build the tests without running them"
				break
			}
		}
		return nil, bad("%s", msg)
	}
	if firstError != "" {
		return nil, bad("%s. The build failed even though the command exited 0; a wrapper such as || true may hide the failure", firstError)
	}
	if len(r.Actions) == 0 {
		return nil, bad("no compile, vet or link keys: the command printed no GODEBUG=gocachehash=1 output. Record a go command, such as go build ./... or go test ./...")
	}
	return r, nil
}

func tracked(name string) bool {
	k, _, ok := strings.Cut(name, " ")
	return ok && trackedKinds[k]
}

func (r *Record) openAction(name string) *Action {
	list := r.ByName[name]
	if n := len(list); n > 0 && list[n-1].open {
		return list[n-1]
	}
	return nil
}

func (r *Record) start(name string) {
	if !tracked(name) {
		return
	}
	if r.openAction(name) != nil {
		// Two keys with the same name were computed at the same time, so
		// their input lines are mixed and can't be told apart.
		r.Ambiguous[name] = true
	}
	a := &Action{Name: name, open: true}
	r.Actions = append(r.Actions, a)
	r.ByName[name] = append(r.ByName[name], a)
}

func (r *Record) write(name, s string) error {
	if !tracked(name) {
		return nil
	}
	a := r.openAction(name)
	if a == nil {
		return fmt.Errorf("input for %q outside its key: the file was cut or edited", name)
	}
	if r.Ambiguous[name] {
		return nil
	}
	if a.Salt == "" && len(a.Inputs) == 0 && !strings.HasSuffix(s, "\n") {
		a.Salt = s
		return nil
	}
	for _, part := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		a.Inputs = append(a.Inputs, part)
	}
	return nil
}

func (r *Record) sum(name, key string) error {
	if !tracked(name) {
		return nil
	}
	list := r.ByName[name]
	// With ambiguous names, close the earliest open one.
	for _, a := range list {
		if a.open {
			a.open = false
			a.Key = key
			return nil
		}
	}
	return fmt.Errorf("key for %q without its start: the file was cut or edited", name)
}

// ---------------------------------------------------------------------------
// Comparing

// Change is one input that differs between the runs for one action.
type Change struct {
	Slot string   `json:"slot"`
	A    []string `json:"a"`
	B    []string `json:"b"`
}

// Diff is one action whose key differs.
type Diff struct {
	Name    string   `json:"name"`
	KeyA    string   `json:"key_a"`
	KeyB    string   `json:"key_b"`
	Own     []Change `json:"own_changes"`
	Deps    []string `json:"changed_dependencies"` // action names
	Causes  []string `json:"causes"`               // group IDs, own and inherited
	rootSet map[string]bool
	busy    bool
	done    bool
}

// Group is one root cause: the same kind of change in one or more actions.
type Group struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Advice   string   `json:"advice"`
	Values   []Pair   `json:"values"`
	Details  []string `json:"details"`
	Roots    []string `json:"actions"`
	Cascaded []string `json:"rebuilt_as_a_result"`
	// Steps and RebuiltSteps count every step, so a package and its test
	// variant with the same name count twice.
	Steps        int `json:"steps"`
	RebuiltSteps int `json:"rebuilt_steps"`
}

// Pair is one input's value in run A and in run B.
type Pair struct {
	A string `json:"a"`
	B string `json:"b"`
}

// Result is the comparison of two records.
type Result struct {
	A           *Record  `json:"a"`
	B           *Record  `json:"b"`
	Compared    int      `json:"compared"`
	Same        int      `json:"same"`
	Different   int      `json:"different"`
	OnlyA       []string `json:"only_in_a"`
	OnlyB       []string `json:"only_in_b"`
	Ambiguous   []string `json:"not_compared"`
	Groups      []*Group `json:"causes"`
	Unexplained []string `json:"unexplained"`
	Diffs       []*Diff  `json:"different_actions"`
	Notes       []string `json:"notes"`
}

// fillEmpty replaces nil lists with empty ones, so JSON has [] instead of null.
func (res *Result) fillEmpty() {
	fill := func(p *[]string) {
		if *p == nil {
			*p = []string{}
		}
	}
	for _, p := range []*[]string{&res.OnlyA, &res.OnlyB, &res.Ambiguous, &res.Unexplained, &res.Notes, &res.A.GoVersions, &res.B.GoVersions} {
		fill(p)
	}
	if res.Groups == nil {
		res.Groups = []*Group{}
	}
	if res.Diffs == nil {
		res.Diffs = []*Diff{}
	}
	for _, g := range res.Groups {
		fill(&g.Details)
		fill(&g.Roots)
		fill(&g.Cascaded)
		if g.Values == nil {
			g.Values = []Pair{}
		}
	}
	for _, d := range res.Diffs {
		fill(&d.Deps)
		fill(&d.Causes)
		if d.Own == nil {
			d.Own = []Change{}
		}
		for i := range d.Own {
			fill(&d.Own[i].A)
			fill(&d.Own[i].B)
		}
	}
}

// Clean reports whether every action was compared and has the same key.
func (res *Result) Clean() bool {
	return res.Different == 0 && len(res.OnlyA) == 0 && len(res.OnlyB) == 0 && len(res.Ambiguous) == 0
}

// slot names the input an input line sets, so that the two runs' lines for
// the same input can be paired.
func slot(line string) string {
	switch {
	case strings.HasPrefix(line, "file "):
		f := strings.Fields(line)
		if len(f) >= 3 {
			return "file " + strings.Join(f[1:len(f)-1], " ")
		}
	case strings.HasPrefix(line, `import "`):
		return "importpath"
	case strings.HasPrefix(line, "import "):
		if j := strings.LastIndex(line, " "); j > len("import ") {
			return "import " + line[len("import "):j]
		}
	case strings.HasPrefix(line, "packagefile "):
		p, _, _ := strings.Cut(strings.TrimPrefix(line, "packagefile "), "=")
		return "packagefile " + p
	case strings.HasPrefix(line, "packageshlib "):
		p, _, _ := strings.Cut(strings.TrimPrefix(line, "packageshlib "), "=")
		return "packageshlib " + p
	case strings.HasPrefix(line, "vetout "):
		f := strings.Fields(line)
		if len(f) >= 3 {
			p, err := strconv.Unquote(f[1])
			if err == nil {
				return "vetout " + p
			}
		}
	case strings.HasPrefix(line, "magic "):
		k, _, _ := strings.Cut(strings.TrimPrefix(line, "magic "), "=")
		return "magic " + k
	case strings.HasPrefix(line, "CC ID"), strings.HasPrefix(line, "CXX ID"), strings.HasPrefix(line, "FC ID"):
		k, _, _ := strings.Cut(line, " ID")
		return k + " ID"
	case strings.HasPrefix(line, "="):
		return "archenv"
	}
	end := strings.IndexAny(line, " =")
	if end < 0 {
		return line
	}
	return line[:end]
}

// cascadeTarget returns the name of the action a dependency input refers to,
// for inputs that change only because another action's output changed.
func cascadeTarget(action *Action, s string) (string, bool) {
	switch {
	case strings.HasPrefix(s, "import ") && s != "importpath":
		return "build " + strings.TrimPrefix(s, "import "), true
	case strings.HasPrefix(s, "packagefile "):
		return "build " + strings.TrimPrefix(s, "packagefile "), true
	case strings.HasPrefix(s, "packageshlib "):
		return "build " + strings.TrimPrefix(s, "packageshlib "), true
	case s == "packagemain":
		return "build " + action.Pkg(), true
	case s == "pkg" && action.Kind() == "vet":
		return "build " + action.Pkg(), true
	case strings.HasPrefix(s, "vetout "):
		return "vet " + strings.TrimPrefix(s, "vetout "), true
	}
	return "", false
}

func multisetDiff(a, b []string) (onlyA, onlyB []string) {
	count := map[string]int{}
	for _, s := range a {
		count[s]++
	}
	for _, s := range b {
		count[s]--
	}
	for _, s := range a {
		if count[s] > 0 {
			onlyA = append(onlyA, s)
			count[s]--
		}
	}
	for _, s := range b {
		if count[s] < 0 {
			onlyB = append(onlyB, s)
			count[s]++
		}
	}
	return
}

func sameKeys(as, bs []*Action) bool {
	if len(as) != len(bs) {
		return false
	}
	ka := make([]string, len(as))
	kb := make([]string, len(bs))
	for i := range as {
		ka[i], kb[i] = as[i].Key, bs[i].Key
	}
	x, y := multisetDiff(ka, kb)
	return len(x) == 0 && len(y) == 0
}

func shared(a, b *Action) int {
	x, _ := multisetDiff(a.Inputs, b.Inputs)
	return len(a.Inputs) - len(x)
}

// pair matches the instances of one name in A and B: equal keys first, then
// by the most shared inputs.
func pair(as, bs []*Action) (pairs [][2]*Action, onlyA, onlyB []*Action) {
	usedA := make([]bool, len(as))
	usedB := make([]bool, len(bs))
	for i, a := range as {
		for j, b := range bs {
			if !usedB[j] && a.Key == b.Key {
				pairs = append(pairs, [2]*Action{a, b})
				usedA[i], usedB[j] = true, true
				break
			}
		}
	}
	for {
		bi, bj, best := -1, -1, -1
		for i, a := range as {
			if usedA[i] {
				continue
			}
			for j, b := range bs {
				if usedB[j] {
					continue
				}
				if s := shared(a, b); s > best {
					bi, bj, best = i, j, s
				}
			}
		}
		if bi < 0 {
			break
		}
		pairs = append(pairs, [2]*Action{as[bi], bs[bj]})
		usedA[bi], usedB[bj] = true, true
	}
	for i, a := range as {
		if !usedA[i] {
			onlyA = append(onlyA, a)
		}
	}
	for j, b := range bs {
		if !usedB[j] {
			onlyB = append(onlyB, b)
		}
	}
	return
}

func changes(a, b *Action) []Change {
	var out []Change
	if a.Salt != b.Salt {
		out = append(out, Change{Slot: "salt", A: []string{a.Salt}, B: []string{b.Salt}})
	}
	da, db := multisetDiff(a.Inputs, b.Inputs)
	bySlot := map[string]*Change{}
	var order []string
	add := func(s string, isA bool) {
		k := slot(s)
		c := bySlot[k]
		if c == nil {
			c = &Change{Slot: k}
			bySlot[k] = c
			order = append(order, k)
		}
		if isA {
			c.A = append(c.A, s)
		} else {
			c.B = append(c.B, s)
		}
	}
	for _, s := range da {
		add(s, true)
	}
	for _, s := range db {
		add(s, false)
	}
	for _, k := range order {
		out = append(out, *bySlot[k])
	}
	return out
}

// Compare compares two records.
func Compare(a, b *Record) (*Result, error) {
	res := &Result{A: a, B: b}
	if a.Start == b.Start && a.Dir == b.Dir && strings.Join(a.Command, "\x00") == strings.Join(b.Command, "\x00") && a.Lines == b.Lines {
		return nil, &ParseError{Path: b.Path, Msg: "this is the same recording as " + a.Path + "; compare two different runs"}
	}
	names := map[string]bool{}
	for n := range a.ByName {
		names[n] = true
	}
	for n := range b.ByName {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	diffs := map[string][]*Diff{}
	for _, n := range sorted {
		as, bs := a.ByName[n], b.ByName[n]
		if (a.Ambiguous[n] || b.Ambiguous[n]) && len(as) > 0 && len(bs) > 0 {
			// The inputs can't be split between the instances, but the keys
			// can: equal sets of keys mean the same cache entries.
			if sameKeys(as, bs) {
				res.Compared += len(as)
				res.Same += len(as)
			} else {
				res.Ambiguous = append(res.Ambiguous, n)
			}
			continue
		}
		pairs, oa, ob := pair(as, bs)
		for range oa {
			res.OnlyA = append(res.OnlyA, n)
		}
		for range ob {
			res.OnlyB = append(res.OnlyB, n)
		}
		for _, p := range pairs {
			res.Compared++
			if p[0].Key == p[1].Key {
				res.Same++
				continue
			}
			res.Different++
			d := &Diff{Name: n, KeyA: p[0].Key, KeyB: p[1].Key}
			for _, c := range changes(p[0], p[1]) {
				if t, ok := cascadeTarget(p[0], c.Slot); ok {
					d.Deps = append(d.Deps, t)
					continue
				}
				d.Own = append(d.Own, c)
			}
			if len(d.Own) == 0 && len(d.Deps) == 0 {
				// Same inputs, different key: only possible if lines were
				// lost. Treat it as unexplained rather than guess.
				d.Own = append(d.Own, Change{Slot: "unknown"})
			}
			diffs[n] = append(diffs[n], d)
			res.Diffs = append(res.Diffs, d)
		}
	}
	if res.Compared == 0 {
		return nil, &ParseError{Path: b.Path, Msg: "the two runs have no compile, vet or link step in common, so there is nothing to compare. Record the same command in both runs"}
	}

	// Group the own changes into causes.
	groups := map[string]*Group{}
	var groupOrder []string
	for _, d := range res.Diffs {
		d.rootSet = map[string]bool{}
		for _, c := range d.grouped() {
			g := describe(c)
			if existing, ok := groups[g.ID]; ok {
				existing.Values = appendPair(existing.Values, g.Values...)
				g = existing
			} else {
				groups[g.ID] = g
				groupOrder = append(groupOrder, g.ID)
			}
			if !d.rootSet[g.ID] {
				g.Steps++
				g.Roots = appendUnique(g.Roots, d.Name)
			}
			if g.ID == "file" {
				g.Details = appendUnique(g.Details, fileDetail(d.Name, c))
			}
			d.rootSet[g.ID] = true
		}
	}
	onlyIn := map[string]bool{}
	for _, n := range res.OnlyA {
		onlyIn[n] = true
	}
	for _, n := range res.OnlyB {
		onlyIn[n] = true
	}
	// Inherit causes through changed dependencies.
	var resolve func(d *Diff) map[string]bool
	resolve = func(d *Diff) map[string]bool {
		if d.done || d.busy {
			return d.rootSet
		}
		d.busy = true
		deps := d.Deps
		if d.rootSet["salt"] {
			deps = nil // with another Go version, every dependency differs too
		}
		for _, dep := range deps {
			found := false
			for _, dd := range diffs[dep] {
				found = true
				for id := range resolve(dd) {
					d.rootSet[id] = true
				}
			}
			if !found && onlyIn[dep] {
				g := groups["only"]
				if g == nil {
					g = &Group{ID: "only", Title: "Dependencies built in only one run",
						Advice: "These packages are built in one run and not the other, so everything that imports them differs. Build tags, CGO_ENABLED, GOOS or the package list differ between the runs."}
					groups["only"] = g
					groupOrder = append(groupOrder, "only")
				}
				if len(g.Roots) == len(appendUnique(g.Roots, dep)) {
					// already counted
				} else {
					g.Roots = append(g.Roots, dep)
					g.Steps++
				}
				d.rootSet["only"] = true
			} else if !found {
				d.rootSet["?dep"] = true
			}
		}
		d.busy = false
		d.done = true
		return d.rootSet
	}
	for _, d := range res.Diffs {
		resolve(d)
	}
	for _, d := range res.Diffs {
		ownIDs := map[string]bool{}
		for _, c := range d.grouped() {
			ownIDs[describe(c).ID] = true
		}
		for id := range d.rootSet {
			d.Causes = append(d.Causes, id)
			if id == "?dep" {
				if len(d.rootSet) == 1 {
					res.Unexplained = append(res.Unexplained, d.Name)
				}
				continue
			}
			if !ownIDs[id] {
				groups[id].Cascaded = appendUnique(groups[id].Cascaded, d.Name)
				groups[id].RebuiltSteps++
			}
		}
		sort.Strings(d.Causes)
	}
	for _, id := range groupOrder {
		g := groups[id]
		if id == "modinfo" {
			g.Advice += modinfoAdvice(g.Values)
		}
		res.Groups = append(res.Groups, g)
	}
	sort.SliceStable(res.Groups, func(i, j int) bool {
		gi, gj := res.Groups[i], res.Groups[j]
		return gi.Steps+gi.RebuiltSteps > gj.Steps+gj.RebuiltSteps
	})

	// Notes about the runs themselves.
	if commandLine(a.Command) != commandLine(b.Command) {
		res.Notes = append(res.Notes, fmt.Sprintf("The runs used different commands: A ran %s and B ran %s.", commandLine(a.Command), commandLine(b.Command)))
	}
	if b.EntriesBefore == 0 {
		res.Notes = append(res.Notes, fmt.Sprintf("B's GOCACHE (%s) was empty when B started, so B rebuilt everything, whatever the keys. The cache wasn't restored, or was restored to another path than go env GOCACHE.", b.GoCache))
	} else if b.EntriesBefore < 0 {
		res.Notes = append(res.Notes, "B's GOCACHE couldn't be read when B started, so this can't tell whether the cache was there.")
	}
	if a.GoCache != "" && b.GoCache != "" && a.GoCache != b.GoCache {
		res.Notes = append(res.Notes, fmt.Sprintf("The runs used different GOCACHE paths (%s, then %s). Matching keys only help if B's cache holds A's results.", a.GoCache, b.GoCache))
	}
	return res, nil
}

// grouped returns the changes that count as this action's own causes. When
// the Go version differs, every other input is expected to differ with it
// (tool IDs, standard library files, defaults), so only the version counts.
func (d *Diff) grouped() []Change {
	for _, c := range d.Own {
		if c.Slot == "salt" {
			return []Change{c}
		}
	}
	return d.Own
}

func modinfoAdvice(values []Pair) string {
	var all strings.Builder
	for _, v := range values {
		all.WriteString(v.A + "\n" + v.B + "\n")
	}
	text := all.String()
	var out string
	if strings.Contains(text, "vcs.") {
		out += " The vcs settings come from -buildvcs, which stamps the commit, its time and whether the tree was modified, so every new commit recompiles package main. Use -buildvcs=false if you don't need them."
	}
	if strings.Contains(text, "-ldflags=") {
		out += " -ldflags is part of the stamp."
	}
	if strings.Contains(text, "CGO_") {
		out += " The CGO_ settings are part of the stamp."
	}
	return out
}

func appendPair(list []Pair, ps ...Pair) []Pair {
	for _, p := range ps {
		dup := false
		for _, x := range list {
			if x == p {
				dup = true
				break
			}
		}
		if !dup {
			list = append(list, p)
		}
	}
	return list
}

func unquoteAll(lines []string, prefix string) []string {
	var out []string
	for _, l := range lines {
		v := strings.TrimPrefix(l, prefix)
		if u, err := strconv.Unquote(v); err == nil {
			v = u
		}
		out = append(out, v)
	}
	return out
}

// godebugDiff returns the comma-separated settings only in a and only in b.
func godebugDiff(a, b string) (string, string) {
	split := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(s, ",")
	}
	oa, ob := multisetDiff(split(a), split(b))
	return strings.Join(oa, ","), strings.Join(ob, ",")
}

// shortenGodebug replaces a DefaultGODEBUG build setting present on both
// sides with only the settings that differ.
func shortenGodebug(a, b []string) ([]string, []string) {
	const key = "build\tDefaultGODEBUG="
	ia, ib := -1, -1
	for i, l := range a {
		if strings.HasPrefix(l, key) {
			ia = i
		}
	}
	for i, l := range b {
		if strings.HasPrefix(l, key) {
			ib = i
		}
	}
	if ia < 0 || ib < 0 {
		return a, b
	}
	da, db := godebugDiff(strings.TrimPrefix(a[ia], key), strings.TrimPrefix(b[ib], key))
	a = append([]string{}, a...)
	b = append([]string{}, b...)
	a[ia] = "DefaultGODEBUG has " + orNone(da)
	b[ib] = "DefaultGODEBUG has " + orNone(db)
	return a, b
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// commandLine joins a command for display, quoting arguments with spaces.
func commandLine(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\"'$^*?[]|&;<>()`\\!#~{}") {
			if !strings.Contains(a, "'") {
				a = "'" + a + "'"
			} else {
				a = strconv.Quote(a)
			}
		}
		out[i] = a
	}
	return strings.Join(out, " ")
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

func fileDetail(action string, c Change) string {
	file := strings.TrimPrefix(c.Slot, "file ")
	switch {
	case len(c.A) == 0:
		return fmt.Sprintf("%s: %s added", action, file)
	case len(c.B) == 0:
		return fmt.Sprintf("%s: %s removed", action, file)
	}
	return fmt.Sprintf("%s: %s changed", action, file)
}

// ---------------------------------------------------------------------------
// Describing causes

func value(lines []string, prefix string) string {
	if len(lines) == 0 {
		return "(none)"
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimPrefix(l, prefix)
	}
	return strings.Join(out, "; ")
}

// pathChange strips the common trailing path elements of two paths, so
// that /a/src/m/lib and /b/src/m/lib give /a and /b.
func pathChange(a, b string) (string, string) {
	pa := strings.Split(a, "/")
	pb := strings.Split(b, "/")
	for len(pa) > 1 && len(pb) > 1 && pa[len(pa)-1] == pb[len(pb)-1] {
		pa, pb = pa[:len(pa)-1], pb[:len(pb)-1]
	}
	return strings.Join(pa, "/"), strings.Join(pb, "/")
}

func modinfoLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		v := strings.TrimPrefix(l, "modinfo ")
		s, err := strconv.Unquote(v)
		if err != nil {
			s = v
		}
		out = append(out, strings.Split(strings.TrimSpace(s), "\n")...)
	}
	return out
}

func describe(c Change) *Group {
	g := &Group{}
	one := func(id, title, advice string, prefix string) *Group {
		g.Title, g.Advice = title, advice
		g.ID = id
		g.Values = []Pair{{value(c.A, prefix), value(c.B, prefix)}}
		return g
	}
	switch s := c.Slot; {
	case s == "salt":
		return one("salt", "Different Go toolchain",
			"Every key starts with the go command's version, so nothing built by one version is reused by another. Use the same Go version in both runs, for example with the same setup-go version and a toolchain line in go.mod.", "")
	case s == "dir":
		pa, pb := pathChange(value(c.A, "dir "), value(c.B, "dir "))
		g.Title = "Package directory changed, and the build doesn't use -trimpath"
		g.Advice = "Without -trimpath the compiler writes the absolute source directory into debug info, so the directory is part of the key. Build in the same directory every time, or add -trimpath to both runs."
		g.Values = []Pair{{pa, pb}}
		g.ID = "dir"
		return g
	case s == "trimpath":
		return one("trimpath", "-trimpath used in only one run",
			"-trimpath changes what the compiler records, so builds with and without it don't share cache entries. Use the same flags in both runs, including GOFLAGS.", "")
	case s == "go":
		return one("go", "go line in go.mod changed",
			"The module's go version is part of every key of its packages. This is expected after a go.mod change.", "go ")
	case s == "goos" || s == "buildmode":
		return one(s, "Different target platform or build mode",
			"GOOS, GOARCH and -buildmode are part of the key. Builds for different targets never share entries.", "")
	case s == "archenv" || (strings.HasPrefix(s, "GO") && s != "GOEXPERIMENT" && s != "GOROOT" && s != "GO_EXTLINK_ENABLED"):
		return one("arch", "Different architecture level (GOAMD64, GOARM64 and similar)",
			"The architecture level variable is part of every key. Set it the same way in both runs.", "")
	case s == "GOEXPERIMENT":
		return one(s, "Different GOEXPERIMENT", "GOEXPERIMENT changes the compiler's output, so it is part of every key.", "GOEXPERIMENT=")
	case s == "GOROOT":
		return one(s, "GOROOT path changed, and the build doesn't use -trimpath",
			"The linker records GOROOT unless -trimpath is set, so every link step misses. This happens when Go is installed to a path that includes its version or a temporary directory. Compile steps are not affected.", "GOROOT=")
	case s == "GO_EXTLINK_ENABLED":
		return one(s, "GO_EXTLINK_ENABLED differs", "This variable decides whether the external linker is used, so it is part of the link key.", "GO_EXTLINK_ENABLED=")
	case s == "compile":
		return one(s, "Compiler version or compiler flags (-gcflags, -race, -cover and similar) differ",
			"The compiler's ID and its flags are part of every compile key. Check -gcflags, -race and GOFLAGS in both runs.", "compile ")
	case s == "asm":
		return one(s, "Assembler version or -asmflags differ", "The assembler's ID and flags are part of the key of packages with assembly.", "asm ")
	case s == "link":
		return one(s, "Linker version, forced linker flags or build mode differ", "The linker's ID and the flags the go command adds are part of every link key.", "link ")
	case s == "linkflags":
		return one(s, "-ldflags differ",
			"-ldflags is part of every link key, and Go also stamps it into package main's build information. Values that change on every run, such as -X main.version with a commit or a date, recompile package main and relink every time. Other packages are not affected.", "linkflags ")
	case s == "cgo" || s == "CC" || s == "CC ID" || s == "CXX" || s == "CXX ID" || s == "FC" || s == "FC ID":
		return one("cgo", "cgo settings differ: the C compiler, its version, or CGO_CFLAGS, CGO_CPPFLAGS, CGO_LDFLAGS",
			"Packages with cgo include the C compiler path, its version output and the CGO_ flags in their key. Install the same C compiler and set the same CGO_ variables in both runs.", "")
	case s == "cover":
		return one(s, "Coverage instrumentation differs", "-cover and -coverpkg change how packages are compiled. Steps with and without coverage don't share entries.", "cover ")
	case s == "fuzz":
		return one(s, "Fuzz instrumentation differs", "Fuzzing compiles packages with extra instrumentation.", "fuzz ")
	case s == "modinfo":
		la, lb := modinfoLines(c.A), modinfoLines(c.B)
		oa, ob := multisetDiff(la, lb)
		oa, ob = shortenGodebug(oa, ob)
		g.Title = "Build information stamped into package main differs"
		g.Advice = "Go stamps module versions and build settings into package main, and the stamp is part of its key, so package main recompiles and relinks. Other packages are not affected."
		g.Values = []Pair{{orNone(strings.Join(oa, "; ")), orNone(strings.Join(ob, "; "))}}
		g.ID = "modinfo"
		return g
	case s == "defaultgodebug":
		g := one(s, "Default GODEBUG settings differ",
			"The link key includes the GODEBUG defaults from go.mod's go and godebug lines and //go:debug directives.", "defaultgodebug ")
		a, b := unquoteAll(c.A, "defaultgodebug "), unquoteAll(c.B, "defaultgodebug ")
		if len(a) == 1 && len(b) == 1 {
			da, db := godebugDiff(a[0], b[0])
			g.Values = []Pair{{orNone(da), orNone(db)}}
		}
		return g
	case strings.HasPrefix(s, "file "):
		g.ID = "file"
		g.Title = "Source files changed"
		g.Advice = "File contents are part of the key, so a changed file rebuilds its package and the packages that import it. That is expected after a code change. A file that changes on every run, such as generated code with a timestamp, makes these steps miss every time."
		return g
	case s == "module":
		return one(s, "Module version changed", "With -trimpath the key includes the module path and version.", "module ")
	case s == "pgofile":
		return one(s, "PGO profile changed", "The profile used with -pgo (default.pgo) is part of the key of every package it affects.", "")
	case s == "vet" || s == "vetflags" || s == "vetxonly":
		return one("vet", "vet tool or vet flags differ", "The vet tool's ID and its flags are part of every vet key.", "")
	case s == "omitdebug" || s == "forcelibrary" || s == "importpath":
		return one(s, "How the package is built differs ("+s+")", "This setting comes from the command, such as go run or go test compared with go build.", "")
	case strings.HasPrefix(s, "magic "):
		return one(s, "A compiler debug variable is set ("+strings.TrimPrefix(s, "magic ")+")", "GOSSAFUNC, GOSSADIR, GOCOMPILEDEBUG and GOCLOBBERDEADHASH change the compile key. Unset them.", "magic ")
	case s == "unknown":
		g.ID = "unknown"
		g.Title = "Different key with the same recorded inputs"
		g.Advice = "The record doesn't show what changed. Record both runs again with the same whyrebuilt version."
		return g
	}
	return one("other:"+c.Slot, "Other key input differs ("+c.Slot+")", "whyrebuilt doesn't know this input. The values are shown as recorded.", "")
}

// ---------------------------------------------------------------------------
// compare command and text report

func cmdCompare(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fail := fs.Bool("fail", false, "exit 1 when any key differs")
	asJSON := fs.Bool("json", false, "print the full result as JSON")
	limit := fs.Int("limit", 5, "show at most `N` actions per cause, 0 for all")
	if err := fs.Parse(reorder(args)); err != nil {
		return exitCantCheck
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "whyrebuilt compare: usage: whyrebuilt compare [--fail] [--json] [--limit N] A B")
		return exitCantCheck
	}
	a, err := ReadRecord(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "whyrebuilt: can't use run A:", err)
		return exitCantCheck
	}
	b, err := ReadRecord(fs.Arg(1))
	if err != nil {
		fmt.Fprintln(stderr, "whyrebuilt: can't use run B:", err)
		return exitCantCheck
	}
	res, err := Compare(a, b)
	if err != nil {
		fmt.Fprintln(stderr, "whyrebuilt:", err)
		return exitCantCheck
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		res.fillEmpty()
		if err := enc.Encode(res); err != nil {
			fmt.Fprintln(stderr, "whyrebuilt:", err)
			return exitCantCheck
		}
	} else {
		writeText(stdout, res, *limit)
	}
	if *fail && !res.Clean() {
		return exitDiffer
	}
	return exitOK
}

// reorder moves flags before the two file arguments, so that
// "compare a b --fail" works as well as "compare --fail a b".
func reorder(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		s := args[i]
		if s == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if strings.HasPrefix(s, "-") && len(s) > 1 {
			flags = append(flags, s)
			if (s == "--limit" || s == "-limit") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		rest = append(rest, s)
	}
	return append(flags, rest...)
}

func describeRun(w io.Writer, label string, r *Record) {
	fmt.Fprintf(w, "%s: %s (%s in %s)\n", label, r.Path, commandLine(r.Command), r.Dir)
	cache := "GOCACHE unknown"
	if r.GoCache != "" {
		before, after := strconv.Itoa(r.EntriesBefore), strconv.Itoa(r.EntriesAfter)
		if r.EntriesBefore < 0 {
			before = "unreadable"
		}
		if r.EntriesAfter < 0 {
			after = "unreadable"
		}
		cache = fmt.Sprintf("GOCACHE %s: %s entries before, %s after", r.GoCache, before, after)
	}
	fmt.Fprintf(w, "   %s, %s\n", strings.Join(r.GoVersions, " and "), cache)
}

func countKinds(names []string) string {
	c := map[string]int{}
	for _, n := range names {
		k, _, _ := strings.Cut(n, " ")
		c[k]++
	}
	var parts []string
	for _, k := range []string{"build", "vet", "link"} {
		if c[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c[k], k))
		}
	}
	return strings.Join(parts, ", ")
}

func sample(names []string, limit int) string {
	if limit > 0 && len(names) > limit {
		return strings.Join(names[:limit], ", ") + fmt.Sprintf(", and %d more", len(names)-limit)
	}
	return strings.Join(names, ", ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func writeText(w io.Writer, res *Result, limit int) {
	describeRun(w, "A", res.A)
	describeRun(w, "B", res.B)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Compared %s: %d with the same key, %d different.\n", plural(res.Compared, "step", "steps"), res.Same, res.Different)
	if len(res.OnlyA) > 0 {
		fmt.Fprintf(w, "Only in A: %s (%s): %s\n", plural(len(res.OnlyA), "step", "steps"), countKinds(res.OnlyA), sample(res.OnlyA, limit))
	}
	if len(res.OnlyB) > 0 {
		fmt.Fprintf(w, "Only in B: %s (%s): %s\n", plural(len(res.OnlyB), "step", "steps"), countKinds(res.OnlyB), sample(res.OnlyB, limit))
	}
	if len(res.Ambiguous) > 0 {
		fmt.Fprintf(w, "Not explained: %s with different keys, whose inputs the go command printed mixed with another step of the same name, such as a package and its test variant: %s. Record both runs with -p 1 to explain them.\n", plural(len(res.Ambiguous), "step", "steps"), sample(res.Ambiguous, limit))
	}
	if res.Clean() {
		fmt.Fprintln(w, "Every step has the same key in both runs. B can reuse everything A built, if B's GOCACHE holds A's results.")
	}
	for i, g := range res.Groups {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%d. %s: %s", i+1, g.Title, plural(g.Steps, "step", "steps"))
		if g.RebuiltSteps > 0 {
			fmt.Fprintf(w, ", and %d more that depend on them", g.RebuiltSteps)
		}
		fmt.Fprintln(w)
		values := g.Values
		if limit > 0 && len(values) > limit {
			values = values[:limit]
		}
		for _, v := range values {
			fmt.Fprintf(w, "   A: %s\n", clip(v.A))
			fmt.Fprintf(w, "   B: %s\n", clip(v.B))
		}
		if len(values) < len(g.Values) {
			fmt.Fprintf(w, "   and %d more values (see --json)\n", len(g.Values)-len(values))
		}
		details := g.Details
		if limit > 0 && len(details) > limit {
			details = append(append([]string{}, details[:limit]...), fmt.Sprintf("and %d more", len(g.Details)-limit))
		}
		for _, d := range details {
			fmt.Fprintf(w, "   %s\n", d)
		}
		if g.ID != "file" {
			fmt.Fprintf(w, "   Steps: %s\n", sample(g.Roots, limit))
		}
		if len(g.Cascaded) > 0 {
			fmt.Fprintf(w, "   Because of them: %s\n", sample(g.Cascaded, limit))
		}
		fmt.Fprintf(w, "   %s\n", g.Advice)
	}
	if len(res.Unexplained) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%s changed only because a dependency's output changed, and that dependency's key couldn't be compared: %s\n", plural(len(res.Unexplained), "step", "steps"), sample(res.Unexplained, limit))
	}
	if len(res.Notes) > 0 {
		fmt.Fprintln(w)
		for _, n := range res.Notes {
			fmt.Fprintf(w, "Note: %s\n", n)
		}
	}
}

func clip(s string) string {
	const max = 300
	if len(s) > max {
		return s[:max] + fmt.Sprintf("... (%d more bytes, see --json)", len(s)-max)
	}
	return s
}
