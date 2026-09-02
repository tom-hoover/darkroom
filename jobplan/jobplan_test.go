package jobplan

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// testCmd stands in for a real command. The suffix is deliberately neither
// "-bw" nor "-ciba": a test that passed only for one command's suffix would
// hide a hard-coded value, which is exactly what this package exists to
// eliminate.
var testCmd = Command{Name: "testcmd", Suffix: "-test"}

func touch(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanPrefersJPEGOverHEIC(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "IMG_0897.heic")
	touch(t, dir, "IMG_0897.jpg")

	jobs, dupes, err := testCmd.Scan(dir, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1", len(jobs))
	}
	if filepath.Ext(jobs[0].Src) != ".jpg" {
		t.Errorf("chose %s, want the .jpg (decoding HEIC is the expensive path)", jobs[0].Src)
	}
	if len(dupes) != 1 || filepath.Ext(dupes[0]) != ".heic" {
		t.Errorf("dupes = %v, want the .heic reported as skipped", dupes)
	}
}

func TestScanOutputPathsAreUnique(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{
		"IMG_0897.heic", "IMG_0897.jpg",
		"IMG_0927.heic", "IMG_0927.jpg",
		"square.jpg", "glacier.jpg", "us.jpg",
		"square_upscayl_4x_high-fidelity-4x.png",
	} {
		touch(t, dir, n)
	}
	jobs, _, err := testCmd.Scan(dir, "", false)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, j := range jobs {
		if prev, ok := seen[j.Dst]; ok {
			t.Fatalf("output %s claimed by both %s and %s", j.Dst, prev, j.Src)
		}
		seen[j.Dst] = j.Src
	}
	if len(jobs) != 6 {
		t.Fatalf("got %d jobs, want 6 distinct photographs", len(jobs))
	}
}

func TestScanIgnoresUnsupportedFiles(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "notes.txt")
	touch(t, dir, "a.jpg")
	jobs, _, err := testCmd.Scan(dir, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1", len(jobs))
	}
}

func TestScanSkipsItsOwnOutput(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a.jpg")
	touch(t, dir, "a-test.jpg")
	jobs, _, err := testCmd.Scan(dir, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || filepath.Base(jobs[0].Src) != "a.jpg" {
		t.Fatalf("jobs = %v, want only a.jpg (a-test.jpg is our own output)", jobs)
	}
}

func TestScanSkipsContactSheets(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a.jpg")
	touch(t, dir, "a-test.jpg")
	touch(t, dir, "a-contact.png")
	jobs, _, err := testCmd.Scan(dir, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || filepath.Base(jobs[0].Src) != "a.jpg" {
		t.Fatalf("jobs = %v, want only a.jpg (a-test.jpg is our own output, a-contact.png is a preview contact sheet)", jobs)
	}
}

func TestScanNaming(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a.heic")

	jobs, _, _ := testCmd.Scan(dir, "", false)
	if got := filepath.Base(jobs[0].Dst); got != "a-test.jpg" {
		t.Errorf("in-place output = %s, want a-test.jpg", got)
	}

	out := t.TempDir()
	jobs, _, _ = testCmd.Scan(dir, out, false)
	if got := filepath.Base(jobs[0].Dst); got != "a.jpg" {
		t.Errorf("-out output = %s, want a.jpg", got)
	}
	if filepath.Dir(jobs[0].Dst) != out {
		t.Errorf("output landed in %s, want %s", filepath.Dir(jobs[0].Dst), out)
	}

	// ScanOne names its destination independently of Scan, so its in-place
	// spelling needs asserting separately: every other ScanOne test here
	// passes a non-empty -out, which never reaches the c.Suffix branch. A
	// suffix hard-coded in ScanOne therefore passed the whole package until
	// this case existed — the exact drift testCmd's "-test" suffix is here to
	// catch.
	one, err := testCmd.ScanOne(filepath.Join(dir, "a.heic"), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(one.Dst); got != "a-test.jpg" {
		t.Errorf("ScanOne in-place output = %s, want a-test.jpg", got)
	}
	if one, err = testCmd.ScanOne(filepath.Join(dir, "a.heic"), out); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(out, "a.jpg"); one.Dst != want {
		t.Errorf("ScanOne -out output = %s, want %s", one.Dst, want)
	}
}

func TestScanRecursive(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	touch(t, dir, "a.jpg")
	touch(t, sub, "b.jpg")

	jobs, _, _ := testCmd.Scan(dir, "", false)
	if len(jobs) != 1 {
		t.Errorf("non-recursive found %d, want 1", len(jobs))
	}
	jobs, _, _ = testCmd.Scan(dir, "", true)
	if len(jobs) != 2 {
		t.Errorf("recursive found %d, want 2", len(jobs))
	}
	names := []string{filepath.Base(jobs[0].Src), filepath.Base(jobs[1].Src)}
	sort.Strings(names)
	if names[0] != "a.jpg" || names[1] != "b.jpg" {
		t.Errorf("recursive found %v", names)
	}
}

// TestScanRefusesOutDirEqualToSource is the data-loss guard. Under -out the
// output is named <base>.jpg with no suffix, so an -out that resolves to
// the source's own directory makes every Dst equal its Src — and RenderFile
// renames its render over that path. Nine originals would be destroyed and the
// run would report success.
//
// The sneaky spellings matter as much as the obvious one: a trailing slash,
// a "./" prefix, and a relative path mixed with an absolute one all name the
// same directory, and all of them must be caught.
func TestScanRefusesOutDirEqualToSource(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a.jpg")
	touch(t, dir, "b.heic")
	t.Chdir(dir)
	// os.Getwd resolves any symlink in the temp path, which is the form
	// filepath.Abs will produce for a relative argument.
	abs, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct{ name, root, out string }{
		{"identical absolute paths", abs, abs},
		{"trailing slash on -out", abs, abs + string(filepath.Separator)},
		{"trailing slash on the target", abs + string(filepath.Separator), abs},
		{"dot-relative both", ".", "."},
		{"./dir versus dir", abs, "./"},
		{"relative target, absolute -out", ".", abs},
		{"absolute target, relative -out", abs, "."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			jobs, _, err := testCmd.Scan(c.root, c.out, false)
			if err == nil {
				t.Fatalf("testCmd.Scan(%q, %q) returned %d jobs and no error — it would overwrite the originals", c.root, c.out, len(jobs))
			}
			if !strings.Contains(err.Error(), "the output path is the source image itself") {
				t.Errorf("error = %v, want the aliasing error", err)
			}
		})
	}
}

func TestScanOneRefusesOutDirEqualToSource(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a.jpg")
	t.Chdir(dir)
	abs, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct{ name, path, out string }{
		{"identical absolute paths", filepath.Join(abs, "a.jpg"), abs},
		{"trailing slash on -out", filepath.Join(abs, "a.jpg"), abs + string(filepath.Separator)},
		{"dot-relative -out", "a.jpg", "."},
		{"./ -out", "a.jpg", "./"},
		{"relative file, absolute -out", "a.jpg", abs},
		{"absolute file, relative -out", filepath.Join(abs, "a.jpg"), "."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			j, err := testCmd.ScanOne(c.path, c.out)
			if err == nil {
				t.Fatalf("testCmd.ScanOne(%q, %q) returned %+v and no error — it would overwrite the original", c.path, c.out, j)
			}
			if !strings.Contains(err.Error(), "the output path is the source image itself") {
				t.Errorf("error = %v, want the aliasing error", err)
			}
		})
	}
}

// A legitimate -out inside the source tree must still work; the guard above
// must not reject every -out under the target.
func TestScanAcceptsOutDirBelowTheSource(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a.jpg")
	out := filepath.Join(dir, "bw")
	jobs, _, err := testCmd.Scan(dir, out, false)
	if err != nil {
		t.Fatalf("Scan into a subdirectory: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Dst != filepath.Join(out, "a.jpg") {
		t.Fatalf("jobs = %v, want a.jpg -> bw/a.jpg", jobs)
	}
}

// An empty Suffix makes isOwnOutput true of every filename, so an unguarded
// Scan skips a full directory, returns nothing, and lets the tool report "no
// images found" and exit 0. Nothing is written and nothing looks wrong. The
// guard turns that into a loud error, and this pins both entry points: the
// suffix is a struct field now, not a compile-time const, and jobplan is
// shortly consumed from separate repositories.
func TestScanRefusesACommandWithNoSuffix(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a.jpg")
	touch(t, dir, "b.heic")
	bare := Command{Name: "testcmd"}

	jobs, dupes, err := bare.Scan(dir, "", false)
	if err == nil {
		t.Fatalf("Scan returned %d jobs, %d dupes and no error — a full directory reported as empty", len(jobs), len(dupes))
	}
	if !strings.Contains(err.Error(), "Suffix") {
		t.Errorf("error = %v, want it to name the empty Suffix", err)
	}

	if j, err := bare.ScanOne(filepath.Join(dir, "a.jpg"), ""); err == nil {
		t.Fatalf("ScanOne returned %+v and no error", j)
	} else if !strings.Contains(err.Error(), "Suffix") {
		t.Errorf("ScanOne error = %v, want it to name the empty Suffix", err)
	}
}

// TestScanSkipsOutputDirectory mirrors heic2jpg's test of the same name;
// Scan's own comment records that this directory-walk behaviour is
// deliberately copied from heic2jpg's Scan. Without it, a second recursive
// run re-reads the first run's output — and because -out output carries no
// suffix, isOwnOutput cannot recognise it by name — so the tree nests one
// level deeper on every run.
func TestScanSkipsOutputDirectory(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "bw")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	touch(t, dir, "a.jpg")
	touch(t, out, "stray.jpg")

	jobs, _, err := testCmd.Scan(dir, out, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || filepath.Base(jobs[0].Src) != "a.jpg" {
		t.Fatalf("jobs = %v, want only a.jpg — bw/ is our own output directory", jobs)
	}
}

// The failure the skip prevents, stated as behaviour: rendering into -out and
// re-running must plan exactly the same work, not one level deeper.
func TestScanIsStableAcrossRepeatedRunsIntoOutDir(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "bw")
	touch(t, dir, "a.jpg")

	first, _, err := testCmd.Scan(dir, out, true)
	if err != nil {
		t.Fatal(err)
	}
	// Stand in for the first run having written its output.
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	touch(t, out, "a.jpg")

	second, _, err := testCmd.Scan(dir, out, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != len(first) || second[0] != first[0] {
		t.Fatalf("re-run planned %v, want the same %v — output is feeding back in", second, first)
	}
	if got := filepath.Join(out, "bw", "a.jpg"); len(second) > 0 && second[0].Dst == got {
		t.Fatalf("re-run nested output one level deeper: %s", got)
	}
}

// --- fix round 2: a symlinked -out is the same directory, however different
// the two paths look. ---

// symlink creates a symlink and fails loudly rather than skipping, so a
// platform that cannot make one is reported instead of silently passing.
func symlink(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		t.Fatalf("os.Symlink(%q, %q): %v — this platform cannot create symlinks, so the data-loss case this test guards is untested here", oldname, newname, err)
	}
}

// A lexical comparison of two absolute cleaned paths cannot see that "link" and
// "photos" are one directory, and os.Rename resolves the link before replacing
// its target — so "skyburn -f -out link/ photos/" rendered every original over
// itself and reported success. Both spellings must be refused.
func TestScanRefusesSymlinkedOutDir(t *testing.T) {
	cases := []struct {
		name string
		// root and out are built per case from the real directory and the link.
		root, out func(real, link string) string
	}{
		{
			"-out is the symlink, target is the real directory",
			func(real, _ string) string { return real },
			func(_, link string) string { return link },
		},
		{
			"-out is the symlink with a trailing slash",
			func(real, _ string) string { return real },
			func(_, link string) string { return link + string(filepath.Separator) },
		},
		{
			// filepath.WalkDir does not descend a root that is itself a symlink,
			// but a trailing slash makes the lstat resolve to the directory, so
			// this spelling really does walk the originals.
			"-out is the real directory, target is the symlink with a trailing slash",
			func(_, link string) string { return link + string(filepath.Separator) },
			func(real, _ string) string { return real },
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base := t.TempDir()
			real := filepath.Join(base, "photos")
			if err := os.Mkdir(real, 0o755); err != nil {
				t.Fatal(err)
			}
			touch(t, real, "a.jpg")
			touch(t, real, "b.heic")
			link := filepath.Join(base, "link")
			symlink(t, real, link)

			root, out := c.root(real, link), c.out(real, link)
			jobs, _, err := testCmd.Scan(root, out, false)
			if err == nil {
				t.Fatalf("testCmd.Scan(%q, %q) returned %d jobs and no error — every original would be overwritten", root, out, len(jobs))
			}
			if !strings.Contains(err.Error(), "the output path is the source image itself") {
				t.Errorf("error = %v, want the aliasing error", err)
			}
		})
	}
}

// The one spelling that does not reach the guard: filepath.WalkDir lstats its
// root, so a bare symlink root is a non-directory entry and the walk visits
// nothing. That is safe — no job means no write — but it must stay safe, so
// this pins "nothing to do", not "success on nine files".
func TestScanOfABareSymlinkRootProducesNoJobs(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "photos")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	touch(t, real, "a.jpg")
	link := filepath.Join(base, "link")
	symlink(t, real, link)

	jobs, _, err := testCmd.Scan(link, real, false)
	if err != nil {
		return // refused outright is fine too
	}
	if len(jobs) != 0 {
		t.Fatalf("testCmd.Scan(%q, %q) returned %v — jobs from a symlinked root must either be refused or absent", link, real, jobs)
	}
}

func TestScanOneRefusesSymlinkedOutDir(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "photos")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	touch(t, real, "a.jpg")
	link := filepath.Join(base, "link")
	symlink(t, real, link)

	cases := []struct{ name, path, out string }{
		{"-out is the symlink", filepath.Join(real, "a.jpg"), link},
		{"-out is the symlink with a trailing slash", filepath.Join(real, "a.jpg"), link + string(filepath.Separator)},
		{"the file is reached through the symlink", filepath.Join(link, "a.jpg"), real},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			j, err := testCmd.ScanOne(c.path, c.out)
			if err == nil {
				t.Fatalf("testCmd.ScanOne(%q, %q) returned %+v and no error — it would overwrite the original", c.path, c.out, j)
			}
			if !strings.Contains(err.Error(), "the output path is the source image itself") {
				t.Errorf("error = %v, want the aliasing error", err)
			}
		})
	}
}

// The guard must catch aliasing, not every symlink: an -out that is a link to a
// genuinely different directory is a normal, useful setup (a scratch disk) and
// must still render.
func TestScanAcceptsSymlinkedOutDirElsewhere(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "photos")
	elsewhere := filepath.Join(base, "scratch")
	for _, d := range []string{real, elsewhere} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	touch(t, real, "a.jpg")
	link := filepath.Join(base, "out-link")
	symlink(t, elsewhere, link)

	jobs, _, err := testCmd.Scan(real, link, false)
	if err != nil {
		t.Fatalf("Scan with -out symlinked elsewhere: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Dst != filepath.Join(link, "a.jpg") {
		t.Fatalf("jobs = %v, want a.jpg -> %s", jobs, filepath.Join(link, "a.jpg"))
	}

	j, err := testCmd.ScanOne(filepath.Join(real, "a.jpg"), link)
	if err != nil {
		t.Fatalf("ScanOne with -out symlinked elsewhere: %v", err)
	}
	if j.Dst != filepath.Join(link, "a.jpg") {
		t.Fatalf("j = %+v, want Dst %s", j, filepath.Join(link, "a.jpg"))
	}
}
