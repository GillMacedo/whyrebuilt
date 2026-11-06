// Package tests runs whyrebuilt the way the skill does: as a built binary,
// on the recorded example runs, on live go builds of a small module, and on
// broken inputs that must never be reported as matching.
package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "whyrebuilt-bin")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "whyrebuilt")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	// Build the single file, as the skill runs it.
	out, err := exec.Command("go", "build", "-o", bin, "../plugin/scripts/whyrebuilt.go").CombinedOutput()
	if err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// whyrebuilt runs the binary in dir and returns stdout, stderr and the exit code.
func whyrebuilt(t *testing.T, dir string, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		code = ee.ExitCode()
	}
	return out.String(), errb.String(), code
}

func rec(name string) string {
	p, err := filepath.Abs(filepath.Join("..", "examples", "records", name+".rec.gz"))
	if err != nil {
		panic(err)
	}
	return p
}

func real(name string) string {
	p, err := filepath.Abs(filepath.Join("..", "examples", "real", "gojq", name+".rec.gz"))
	if err != nil {
		panic(err)
	}
	return p
}

// compare runs compare with and without --fail and checks both exit codes.
func compare(t *testing.T, a, b string, wantFail int) string {
	t.Helper()
	out, errOut, code := whyrebuilt(t, ".", nil, "compare", a, b)
	if code != 0 {
		t.Fatalf("compare exit %d: %s", code, errOut)
	}
	out2, _, code := whyrebuilt(t, ".", nil, "compare", "--fail", a, b)
	if code != wantFail {
		t.Fatalf("compare --fail exit %d, want %d", code, wantFail)
	}
	if out2 != out {
		t.Fatal("--fail changed the report")
	}
	return out
}

func mustContain(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}

func mustNotContain(t *testing.T, out string, bad ...string) {
	t.Helper()
	for _, b := range bad {
		if strings.Contains(out, b) {
			t.Errorf("output has %q:\n%s", b, out)
		}
	}
}

const clean = "Every step has the same key in both runs."

func TestHealthyFreshCheckoutStaysQuiet(t *testing.T) {
	out := compare(t, rec("base"), rec("fresh-checkout"), 0)
	mustContain(t, out, "202 with the same key, 0 different", clean)
	mustNotContain(t, out, "1. ", "Note:")
}

func TestTrimpathAtTwoPathsStaysQuiet(t *testing.T) {
	out := compare(t, rec("trimpath"), rec("trimpath-other-path"), 0)
	mustContain(t, out, clean)
}

func TestOtherPath(t *testing.T) {
	out := compare(t, rec("base"), rec("other-path"), 1)
	mustContain(t, out,
		"5 different",
		"1. Package directory changed, and the build doesn't use -trimpath: 4 steps, and 1 more that depend on them",
		"A: /tmp/whyrebuilt-ci/runner-1\n",
		"B: /tmp/whyrebuilt-ci/runner-2\n",
		"Because of them: link example.com/inventory/cmd/inventory")
	mustNotContain(t, out, clean, "2. ")
}

func TestLdflags(t *testing.T) {
	out := compare(t, rec("ldflags-1"), rec("ldflags-2"), 1)
	mustContain(t, out, "2 different", "-ldflags differ: 1 step", "Version=build-1", "Version=build-2", "Build information stamped into package main")
}

func TestGeneratedFile(t *testing.T) {
	out := compare(t, rec("base"), rec("generated"), 1)
	mustContain(t, out, "Source files changed: 1 step, and 3 more", "build example.com/inventory/internal/version: version.go changed")
	mustNotContain(t, out, "2. ")
}

func TestGoVersion(t *testing.T) {
	out := compare(t, rec("base"), rec("go1.22.12"), 1)
	mustContain(t, out, "1. Different Go toolchain", "A: go1.27.3", "B: go1.22.12")
	// The version hides everything that follows from it.
	mustNotContain(t, out, "2. ")
}

func TestCgo(t *testing.T) {
	out := compare(t, rec("base"), rec("cgo-cflags"), 1)
	mustContain(t, out, "cgo settings differ", "-fno-omit-frame-pointer", "build net", "build runtime/cgo")
	out = compare(t, rec("base"), rec("cgo-off"), 1)
	mustContain(t, out, "Only in A: 1 step (1 build): build runtime/cgo", "CGO_ENABLED=0")
}

func TestRace(t *testing.T) {
	out := compare(t, rec("test"), rec("test-race"), 1)
	mustContain(t, out, "0 with the same key", `["-race"]`)
}

func TestVcsStamp(t *testing.T) {
	out := compare(t, rec("vcs-1"), rec("vcs-2"), 1)
	mustContain(t, out, "2 different", "vcs.revision=", "-buildvcs=false")
}

func TestEmptyCacheNote(t *testing.T) {
	out := compare(t, rec("base"), rec("empty-cache"), 0)
	mustContain(t, out, clean, "B's GOCACHE (", "was empty when B started")
}

func TestTestRuns(t *testing.T) {
	out := compare(t, rec("test"), rec("test-other-path"), 1)
	mustContain(t, out, "Package directory changed", "example.com/inventory/internal/api.test")
}

func TestRealGojq(t *testing.T) {
	out := compare(t, real("v0.12.16"), real("v0.12.17"), 1)
	mustContain(t, out, "go line in go.mod changed", "A: 1.20", "B: 1.21", "builtin.go changed", "and 10 more")
	out = compare(t, real("v0.12.17"), real("v0.12.17-other-path"), 1)
	mustContain(t, out, "Package directory changed")
	// Without -p 1 a package and its test variant can be printed mixed.
	out = compare(t, real("test"), real("test-other-path"), 1)
	mustContain(t, out, "Not explained: 1 step", "-p 1")
	out = compare(t, real("test-p1"), real("test-p1-other-path"), 1)
	mustNotContain(t, out, "Not explained", "couldn't be compared")
}

func TestJSON(t *testing.T) {
	out, _, code := whyrebuilt(t, ".", nil, "compare", "--json", rec("base"), rec("other-path"))
	if code != 0 {
		t.Fatal(code)
	}
	var res struct {
		Different int `json:"different"`
		Causes    []struct {
			ID     string   `json:"id"`
			Roots  []string `json:"actions"`
			Values []struct{ A, B string }
		} `json:"causes"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "null") {
		t.Error("JSON has null instead of an empty list")
	}
	if res.Different != 5 || len(res.Causes) != 1 || res.Causes[0].ID != "dir" || len(res.Causes[0].Roots) != 4 {
		t.Fatalf("%+v", res)
	}
}

// broken returns inputs that must never be compared: each must exit 2 with
// nothing on stdout, on either side, with and without --fail.
func broken(t *testing.T) map[string]string {
	dir := t.TempDir()
	write := func(name, data string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	raw, err := os.ReadFile(rec("base"))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"empty":            write("empty.rec", ""),
		"garbage":          write("garbage.rec", "\x7fELF\x02\x01 not a record\n"),
		"cut gzip":         write("cut.rec.gz", string(raw[:len(raw)/2])),
		"missing":          filepath.Join(dir, "missing.rec"),
		"directory":        dir,
		"failed build":     rec("failed-build"),
		"hidden failure":   rec("hidden-failure"),
		"failing tests":    real("test-failing"),
		"build error text": write("build-error.txt", "# example.com/inventory/internal/version\ninternal/version/broken.go:3:28: cannot use \"x\" (untyped string constant) as int value in return statement\n"),
	}
}

func TestBrokenInputsAreRefused(t *testing.T) {
	good := rec("base")
	other := rec("other-path")
	for name, bad := range broken(t) {
		for _, args := range [][]string{
			{"compare", bad, good},
			{"compare", good, bad},
			{"compare", "--fail", bad, other},
			{"compare", "--fail", other, bad},
			{"compare", "--json", bad, good},
		} {
			out, errOut, code := whyrebuilt(t, ".", nil, args...)
			if code != 2 || out != "" {
				t.Errorf("%s: %v: exit %d, stdout %q", name, args, code, out)
			}
			if !strings.Contains(errOut, "whyrebuilt") {
				t.Errorf("%s: no message: %q", name, errOut)
			}
		}
	}
}

func TestSameRecordTwiceIsRefused(t *testing.T) {
	for _, args := range [][]string{{"compare", rec("base"), rec("base")}, {"compare", "--fail", rec("base"), rec("base")}} {
		out, _, code := whyrebuilt(t, ".", nil, args...)
		if code != 2 || out != "" {
			t.Errorf("%v: exit %d, stdout %q", args, code, out)
		}
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"compare"}, {"compare", rec("base")}, {"nope"}, {"record"}, {"record", "-o", "x"}} {
		out, _, code := whyrebuilt(t, ".", nil, args...)
		if code != 2 || out != "" {
			t.Errorf("%v: exit %d, stdout %q", args, code, out)
		}
	}
}

// ---------------------------------------------------------------------------
// Live runs

// module writes a small module with a library and a main package.
func module(t *testing.T, dir string, broken bool) {
	t.Helper()
	files := map[string]string{
		"go.mod":          "module example.com/live\n\ngo 1.22\n",
		"lib/lib.go":      "package lib\n\nimport \"strings\"\n\nfunc Up(s string) string { return strings.ToUpper(s) }\n",
		"lib/lib_test.go": "package lib\n\nimport \"testing\"\n\nfunc TestUp(t *testing.T) {\n\tif Up(\"a\") != \"A\" {\n\t\tt.Fatal()\n\t}\n}\n",
		"main.go":         "package main\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/live/lib\"\n)\n\nfunc main() { fmt.Println(lib.Up(\"hi\")) }\n",
	}
	if broken {
		files["lib/broken.go"] = "package lib\n\nfunc Broken() int { return \"x\" }\n"
	}
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func goEnv() []string {
	// Keep the user's cache, but don't let the user's GOFLAGS or a
	// toolchain switch change the builds.
	return []string{"GOFLAGS=", "GOTOOLCHAIN=local", "CGO_ENABLED=0"}
}

func record(t *testing.T, dir, out string, env []string, command ...string) int {
	t.Helper()
	_, errOut, code := whyrebuilt(t, dir, append(goEnv(), env...), append([]string{"record", "-o", out, "--"}, command...)...)
	if code != 0 {
		t.Logf("record %v: exit %d: %s", command, code, errOut)
	}
	return code
}

func TestLiveHealthyAndMoved(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "a", "live")
	b := filepath.Join(base, "b", "live")
	module(t, a, false)
	module(t, b, false)
	r1, r2, r3 := filepath.Join(base, "1.rec"), filepath.Join(base, "2.rec.gz"), filepath.Join(base, "3.rec")
	for _, r := range []struct{ dir, out string }{{a, r1}, {a, r2}, {b, r3}} {
		if code := record(t, r.dir, r.out, nil, "go", "build", "-o", os.DevNull, "."); code != 0 {
			t.Fatalf("record exit %d", code)
		}
	}
	out := compare(t, r1, r2, 0)
	mustContain(t, out, clean)
	out = compare(t, r1, r3, 1)
	mustContain(t, out, "Package directory changed", "build example.com/live/lib", "Because of them: link example.com/live")
}

func TestLiveOtherTarget(t *testing.T) {
	// Pick a target that isn't this machine's, whatever it is.
	target := "amd64"
	if runtime.GOARCH == "amd64" {
		target = "arm64"
	}
	dir := t.TempDir()
	m := filepath.Join(dir, "live")
	module(t, m, false)
	host, cross := filepath.Join(dir, "host.rec"), filepath.Join(dir, "cross.rec")
	if record(t, m, host, []string{"GOOS=linux", "GOARCH=" + runtime.GOARCH}, "go", "build", "-o", os.DevNull, ".") != 0 {
		t.Fatal("host record failed")
	}
	if record(t, m, cross, []string{"GOOS=linux", "GOARCH=" + target}, "go", "build", "-o", os.DevNull, ".") != 0 {
		t.Fatal("cross record failed")
	}
	out := compare(t, host, cross, 1)
	mustContain(t, out, "Different target platform", "goarch "+target)
	mustNotContain(t, out, clean)
}

func TestLiveFailedBuild(t *testing.T) {
	dir := t.TempDir()
	m := filepath.Join(dir, "live")
	module(t, m, true)
	good := filepath.Join(dir, "good.rec")
	goodDir := filepath.Join(dir, "ok", "live")
	module(t, goodDir, false)
	if record(t, goodDir, good, nil, "go", "build", "./...") != 0 {
		t.Fatal("good record failed")
	}
	failed := filepath.Join(dir, "failed.rec")
	if code := record(t, m, failed, nil, "go", "build", "./..."); code == 0 {
		t.Fatal("record of a failed build exited 0")
	}
	failedTest := filepath.Join(dir, "failed-test.rec")
	if code := record(t, m, failedTest, nil, "go", "test", "./..."); code == 0 {
		t.Fatal("record of a failed test exited 0")
	}
	cases := []string{failed, failedTest}
	if runtime.GOOS != "windows" {
		hidden := filepath.Join(dir, "hidden.rec")
		if code := record(t, m, hidden, nil, "sh", "-c", "go build ./... || true"); code != 0 {
			t.Fatalf("sh exit %d", code)
		}
		cases = append(cases, hidden)
	}
	for _, bad := range cases {
		for _, args := range [][]string{{"compare", bad, good}, {"compare", good, bad}, {"compare", "--fail", bad, good}, {"compare", "--fail", good, bad}} {
			out, errOut, code := whyrebuilt(t, ".", nil, args...)
			if code != 2 || out != "" {
				t.Errorf("%v: exit %d, stdout %q, stderr %s", args, code, out, errOut)
			}
		}
	}
}

func TestLiveNotAGoCommand(t *testing.T) {
	dir := t.TempDir()
	m := filepath.Join(dir, "live")
	module(t, m, false)
	good := filepath.Join(dir, "good.rec")
	if record(t, m, good, nil, "go", "build", "./...") != 0 {
		t.Fatal("record failed")
	}
	// go version and go list compute no build keys.
	for i, command := range [][]string{{"go", "version"}, {"go", "list", "./..."}} {
		p := filepath.Join(dir, "none"+string(rune('0'+i))+".rec")
		if record(t, m, p, nil, command...) != 0 {
			t.Fatalf("%v failed", command)
		}
		for _, args := range [][]string{{"compare", p, good}, {"compare", "--fail", good, p}} {
			out, _, code := whyrebuilt(t, ".", nil, args...)
			if code != 2 || out != "" {
				t.Errorf("%v: exit %d, stdout %q", args, code, out)
			}
		}
	}
	// A command that doesn't exist.
	p := filepath.Join(dir, "missing.rec")
	if code := record(t, m, p, nil, "whyrebuilt-no-such-command"); code == 0 {
		t.Fatal("missing command exited 0")
	}
	out, _, code := whyrebuilt(t, ".", nil, "compare", p, good)
	if code != 2 || out != "" {
		t.Errorf("missing command record: exit %d", code)
	}
}

func TestLiveRecordKeepsGodebugAndExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "x.rec")
	_, _, code := whyrebuilt(t, dir, []string{"GODEBUG=http2debug=1"}, "record", "-o", p, "--", "sh", "-c", "echo \"$GODEBUG\" >&2; exit 3")
	if code != 3 {
		t.Fatalf("exit %d, want 3", code)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\nhttp2debug=1,gocachehash=1\n") || !strings.HasSuffix(string(data), "# end exit 3 lines 1 entries ") && !strings.Contains(string(data), "# end exit 3 lines 1 entries") {
		t.Fatalf("record:\n%s", data)
	}
}
