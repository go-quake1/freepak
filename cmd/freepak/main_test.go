package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-quake1/freepak"
)

func run(args ...string) (string, string, error) {
	var out, errb bytes.Buffer
	c := RootCmd()
	c.SetArgs(args)
	c.SetOut(&out)
	c.SetErr(&errb)
	err := c.Execute()
	return out.String(), errb.String(), err
}

func TestBuildNeedsReferenceChoice(t *testing.T) {
	_, _, err := run("build", "--librequake", "x.zip", "--out", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("err=%v", err)
	}
	_, _, err = run("build", "--out", t.TempDir())
	if err == nil {
		t.Fatal("missing --librequake accepted")
	}
}

func TestBuildRefusesOtherArchive(t *testing.T) {
	z := filepath.Join(t.TempDir(), "x.zip")
	if err := os.WriteFile(z, []byte("zip?"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := run("build", "--no-reference", "--librequake", z, "--out", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("err=%v", err)
	}
	if _, _, err := run("maps", "--librequake", z); err == nil {
		t.Fatal("maps accepted another archive")
	}
}

func TestLayoutWritesPathsOnly(t *testing.T) {
	dir := t.TempDir()
	secret := []byte("SECRET-CONTENT-OF-THE-REFERENCE")
	pak, err := freepak.WritePak([]freepak.File{{Name: "a/b.txt", Data: secret}})
	if err != nil {
		t.Fatal(err)
	}
	ref := filepath.Join(dir, "ref.pak")
	if err := os.WriteFile(ref, pak, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "layout.txt")
	if _, _, err := run("layout", "--reference", ref, "--out", out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	if !strings.Contains(string(b), "a/b.txt 31") || bytes.Contains(b, secret) {
		t.Fatalf("layout:\n%s", b)
	}
	if _, _, err := run("layout", "--reference", filepath.Join(dir, "none"), "--out", out); err == nil {
		t.Error("missing reference accepted")
	}
	if err := os.WriteFile(ref, []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run("layout", "--reference", ref, "--out", out); err == nil {
		t.Error("junk reference accepted")
	}
}

func TestMainExitsNonZeroOnError(t *testing.T) {
	code := -1
	osExit = func(c int) { code = c }
	defer func() { osExit = os.Exit }()
	old := os.Args
	os.Args = []string{"freepak", "build"}
	defer func() { os.Args = old }()
	main()
	if code != 1 {
		t.Fatalf("exit code %d", code)
	}
}

func TestMapsOnPinnedArchive(t *testing.T) {
	z := os.Getenv("FREEPAK_LQ")
	if z == "" {
		t.Skip("FREEPAK_LQ not set")
	}
	out, _, err := run("maps", "--librequake", z)
	if err != nil || !strings.Contains(out, "lq_e0m1") {
		t.Fatalf("%v\n%s", err, out)
	}
}
