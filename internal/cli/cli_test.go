package cli

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseInterspersed(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	days := fs.Int("days", 0, "")
	var to stringList
	fs.Var(&to, "to", "")
	pos, err := parseInterspersed(fs, []string{"a.txt", "--days", "3", "b.txt", "--to", "x@y.z,w@y.z", "--", "--not-a-flag"})
	if err != nil {
		t.Fatal(err)
	}
	if *days != 3 || !reflect.DeepEqual(pos, []string{"a.txt", "b.txt", "--not-a-flag"}) || !reflect.DeepEqual([]string(to), []string{"x@y.z", "w@y.z"}) {
		t.Fatalf("days=%d pos=%v to=%v", *days, pos, to)
	}
}

func TestSplitLeadingFlags(t *testing.T) {
	lead, rest := splitLeadingFlags([]string{"-v", "--config", "c.json", "send", "f.txt"})
	if !reflect.DeepEqual(lead, []string{"-v", "--config", "c.json"}) || !reflect.DeepEqual(rest, []string{"send", "f.txt"}) {
		t.Fatalf("lead=%v rest=%v", lead, rest)
	}
}

func TestCollectFiles(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "folder")
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("aaa"), 0o644)
	os.WriteFile(filepath.Join(root, "sub", "b.pdf"), []byte("bbbb"), 0o644)
	single := filepath.Join(dir, "c.bin")
	os.WriteFile(single, nil, 0o644)
	files, total, err := collectFiles([]string{root, single})
	if err != nil {
		t.Fatal(err)
	}
	if total != 7 || len(files) != 3 {
		t.Fatalf("total=%d files=%v", total, files)
	}
	want := map[string]string{"folder/a.txt": "text/plain", "folder/sub/b.pdf": "application/pdf", "c.bin": "application/octet-stream"}
	for _, f := range files {
		if want[f.Name] != f.MimeType {
			t.Errorf("%s: mime %q", f.Name, f.MimeType)
		}
	}
	if _, _, err := collectFiles([]string{single, single}); err == nil {
		t.Error("duplicate names should fail")
	}
}

func TestSafeRelPathAndUnique(t *testing.T) {
	if got := safeRelPath("../../etc/passwd"); got != "etc/passwd" {
		t.Fatalf("got %q", got)
	}
	if got := safeRelPath("a\\b/./c.txt"); got != "a/b/c.txt" {
		t.Fatalf("got %q", got)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if uniquePath(p) != p {
		t.Fatal("missing file should keep its name")
	}
	os.WriteFile(p, nil, 0o644)
	if got := uniquePath(p); got != filepath.Join(dir, "f (1).txt") {
		t.Fatalf("got %q", got)
	}
}

func TestMatchesAny(t *testing.T) {
	if !matchesAny("folder/sub/c.dat", []string{"*.dat"}) || !matchesAny("folder/a.bin", []string{"a.bin"}) || matchesAny("x.txt", []string{"*.pdf"}) {
		t.Fatal("glob matching broken")
	}
}
