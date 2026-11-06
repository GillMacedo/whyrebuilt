package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

func readExample(t *testing.T, name string) string {
	t.Helper()
	data, err := readFile("../../examples/records/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSlot(t *testing.T) {
	cases := map[string]string{
		"file lib.go RQtDPg3RYmOd7jjWNqZc":                       "file lib.go",
		"file my file.go RQtDPg3RYmOd7jjWNqZc":                   "file my file.go",
		`import "example.com/m/lib"`:                             "importpath",
		"import fmt T4OlT4aYFCkynDkgrpjY":                        "import fmt",
		"import example.com/m/lib [example.com/m/lib.test] abcd": "import example.com/m/lib [example.com/m/lib.test]",
		"packagefile example.com/m/lib=xNPmUdglCVjv0M3Ris7x":     "packagefile example.com/m/lib",
		"packagemain 252gOCvJ-PVniAkRgOZi/v_8MLeoRxDCeGZzywAma":  "packagemain",
		`vetout "fmt" 0123`:                                      "vetout fmt",
		"dir /tmp/exp/m/lib":                                     "dir",
		"GOARM64=v8.0":                                           "GOARM64",
		"=":                                                      "archenv",
		`CC=["gcc"] [] [] []`:                                    "CC",
		`CC ID="gcc version 14.2.0"`:                             "CC ID",
		"magic GOSSAFUNC=main":                                   "magic GOSSAFUNC",
		"compile compile version go1.27.3 [] []":                 "compile",
		"trimpath":                                               "trimpath",
		"omitdebug false standard false local false prefix \"\"": "omitdebug",
	}
	for in, want := range cases {
		if got := slot(in); got != want {
			t.Errorf("slot(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCascadeTarget(t *testing.T) {
	link := &Action{Name: "link example.com/m"}
	vet := &Action{Name: "vet example.com/m/lib"}
	cases := []struct {
		a    *Action
		slot string
		want string
		ok   bool
	}{
		{link, "packagefile fmt", "build fmt", true},
		{link, "packagemain", "build example.com/m", true},
		{vet, "pkg", "build example.com/m/lib", true},
		{vet, "vetout fmt", "vet fmt", true},
		{link, "import fmt", "build fmt", true},
		{link, "importpath", "", false},
		{link, "linkflags", "", false},
	}
	for _, c := range cases {
		got, ok := cascadeTarget(c.a, c.slot)
		if got != c.want || ok != c.ok {
			t.Errorf("cascadeTarget(%s, %q) = %q, %v; want %q, %v", c.a.Name, c.slot, got, ok, c.want, c.ok)
		}
	}
}

func TestPathChange(t *testing.T) {
	a, b := pathChange("/tmp/runner-1/work/m/lib", "/tmp/runner-2/work/m/lib")
	if a != "/tmp/runner-1" || b != "/tmp/runner-2" {
		t.Errorf("got %q, %q", a, b)
	}
	a, b = pathChange("/x/m", "/y/n")
	if a != "/x/m" || b != "/y/n" {
		t.Errorf("got %q, %q", a, b)
	}
}

func TestWithGodebug(t *testing.T) {
	got := withGodebug([]string{"A=1", "GODEBUG=http2debug=1"})
	if got[1] != "GODEBUG=http2debug=1,gocachehash=1" {
		t.Errorf("kept GODEBUG: %v", got)
	}
	got = withGodebug([]string{"A=1"})
	if got[len(got)-1] != "GODEBUG=gocachehash=1" {
		t.Errorf("added GODEBUG: %v", got)
	}
	got = withGodebug([]string{"GODEBUG="})
	if got[0] != "GODEBUG=gocachehash=1" {
		t.Errorf("empty GODEBUG: %v", got)
	}
}

func TestKeep(t *testing.T) {
	keepLines := []string{
		"HASH[build fmt]",
		`HASH[build fmt]: "compile\n"`,
		"HASH[link example.com/m]: " + strings.Repeat("a", 64),
		"HASH[build example.com/m/lib [example.com/m/lib.test]]",
		"# example.com/m",
		"go: downloading example.com/x v1.0.0",
	}
	for _, l := range keepLines {
		if !keep(l) {
			t.Errorf("keep(%q) = false", l)
		}
	}
	dropLines := []string{
		"HASH[moduleIndex]",
		`HASH[testResult]: "go1.27.3"`,
		"HASH subkey 00 \"srcfiles\" = 11",
		"HASH /tmp/m/lib.go: " + strings.Repeat("b", 64),
	}
	for _, l := range dropLines {
		if keep(l) {
			t.Errorf("keep(%q) = true", l)
		}
	}
}

func TestCommandLine(t *testing.T) {
	got := commandLine([]string{"go", "build", "-ldflags", "-X a.b=c d", "./..."})
	if got != `go build -ldflags '-X a.b=c d' ./...` {
		t.Errorf("got %s", got)
	}
}

func TestParseRealRecord(t *testing.T) {
	r, err := ParseRecord("base", readExample(t, "base.rec.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Actions) < 100 || r.Exit != 0 || len(r.GoVersions) != 1 {
		t.Fatalf("actions %d, exit %d, versions %v", len(r.Actions), r.Exit, r.GoVersions)
	}
	for _, a := range r.Actions {
		if a.Key == "" || a.Salt == "" || len(a.Inputs) == 0 {
			t.Fatalf("incomplete action %+v", a)
		}
	}
}

// Every proper prefix of a real record, cut at a line end or inside a line,
// must be refused.
func TestEveryCutIsRefused(t *testing.T) {
	data := readExample(t, "fresh-checkout.rec.gz")
	if _, err := ParseRecord("full", data); err != nil {
		t.Fatal(err)
	}
	cuts := 0
	for i := 0; i < len(data)-1; i++ {
		if data[i] != '\n' && i%997 != 0 {
			continue
		}
		cuts++
		if _, err := ParseRecord("cut", data[:i+1]); err == nil {
			t.Fatalf("a record cut at byte %d of %d was accepted", i+1, len(data))
		}
	}
	if cuts < 1000 {
		t.Fatalf("only %d cut points", cuts)
	}
}

// A gzip record cut anywhere must be refused, not read as a shorter record.
func TestCutGzipIsRefused(t *testing.T) {
	raw, err := os.ReadFile("../../examples/records/other-path.rec.gz")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, n := range []int{0, 1, 2, 10, 100, len(raw) / 3, len(raw) / 2, len(raw) - 9, len(raw) - 1} {
		p := dir + "/cut.rec.gz"
		if err := os.WriteFile(p, raw[:n], 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadRecord(p); err == nil {
			t.Errorf("gzip cut to %d of %d bytes was accepted", n, len(raw))
		}
	}
}

func TestDamagedRecords(t *testing.T) {
	good := readExample(t, "base.rec.gz")
	lines := strings.SplitAfter(good, "\n")
	header := strings.Join(lines[:6], "")
	cases := map[string]string{
		"empty":          "",
		"garbage":        "\x00\x01garbage\nmore garbage\n",
		"header only":    lines[0],
		"no begin":       strings.Join(lines[:5], ""),
		"no body":        header + "# end exit 0 lines 0 entries 3\n",
		"line dropped":   strings.Join(lines[:100], "") + strings.Join(lines[101:], ""),
		"line added":     strings.Join(lines[:100], "") + "HASH[build x]\n" + strings.Join(lines[100:], ""),
		"no last nl":     strings.TrimSuffix(good, "\n"),
		"failed":         strings.Replace(good, "# end exit 0 ", "# end exit 1 ", 1),
		"killed":         strings.Replace(good, "# end exit 0 ", "# end exit 128 ", 1),
		"bad footer":     strings.Replace(good, "# end exit 0 ", "# end exit zero ", 1),
		"twice":          good + good,
		"raw":            strings.Join(lines[6:len(lines)-2], ""),
		"bad quote":      strings.Replace(good, `"compile\n"`, `"compile\n`, 1),
		"bad hex":        header + "HASH[build x]\nHASH[build x]: zz\n" + "# end exit 0 lines 2 entries 3\n",
		"unfinished key": header + "HASH[build x]\nHASH[build x]: \"go1\"\n" + "# end exit 0 lines 2 entries 3\n",
		"stray input":    header + "HASH[build x]: \"go1\"\n" + "# end exit 0 lines 1 entries 3\n",
		"stray sum":      header + "HASH[build x]: " + strings.Repeat("a", 64) + "\n" + "# end exit 0 lines 1 entries 3\n",
		"only other":     header + "go: downloading example.com/x v1.0.0\n# end exit 0 lines 1 entries 3\n",
	}
	// Fix the line count of the compile error case so only the error line
	// itself can be the reason it is refused.
	n := len(lines) - 8 + 1
	cases["compile error"] = header + "a/a.go:2:23: cannot use \"x\" as int value\n" + strings.Join(lines[6:len(lines)-2], "") + "# end exit 0 lines " + strconv.Itoa(n) + " entries 3\n"
	for name, data := range cases {
		_, err := ParseRecord(name, data)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		t.Logf("%s: %v", name, err)
	}
	// The same record with a C compiler warning is fine.
	ok := header + "cgo-gcc-prolog:12:3: warning: unused variable\n" + strings.Join(lines[6:len(lines)-2], "") + "# end exit 0 lines " + strconv.Itoa(n) + " entries 3\n"
	if _, err := ParseRecord("warning", ok); err != nil {
		t.Errorf("warning: %v", err)
	}
}

func TestCompareSameRecordIsRefused(t *testing.T) {
	r1, err := ParseRecord("a", readExample(t, "base.rec.gz"))
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := ParseRecord("b", readExample(t, "base.rec.gz"))
	if _, err := Compare(r1, r2); err == nil {
		t.Fatal("the same recording twice was accepted")
	}
}

func TestCompareGroupsCauses(t *testing.T) {
	a, err := ParseRecord("a", readExample(t, "base.rec.gz"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseRecord("b", readExample(t, "other-path.rec.gz"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Compare(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 1 || res.Groups[0].ID != "dir" {
		t.Fatalf("groups %+v", res.Groups)
	}
	g := res.Groups[0]
	if len(g.Roots) != 4 || len(g.Cascaded) != 1 || g.Cascaded[0] != "link example.com/inventory/cmd/inventory" {
		t.Fatalf("roots %v, cascaded %v", g.Roots, g.Cascaded)
	}
	for _, r := range g.Roots {
		if !strings.HasPrefix(r, "build example.com/inventory/") {
			t.Errorf("standard library or other step as root: %s", r)
		}
	}
	if res.Clean() || res.Different != 5 {
		t.Fatalf("different %d", res.Different)
	}
}

func TestGzipRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	io.WriteString(zw, "hello\n")
	zw.Close()
	p := t.TempDir() + "/x.gz"
	os.WriteFile(p, buf.Bytes(), 0o644)
	got, err := readFile(p)
	if err != nil || got != "hello\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}
