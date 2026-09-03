// Package jobplan plans and executes the work of a rendering command: it
// finds the source photographs under a target, decides where each one's
// output belongs, refuses any plan that would write over an original, and
// renders the batch in parallel. The pixels are somebody else's business —
// a caller supplies a Renderer — so one copy of the naming rules and the
// data-loss guards serves every tool built on it.
package jobplan

import (
	"errors"
	"fmt"
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tom-hoover/darkroom/imaging"
)

// ContactSuffix marks a contact sheet written by preview mode. It and the
// command's own Suffix are both skipped so a scan never treats the command's
// own output as a source photograph — a preview writes <base>-contact.png,
// and .png is a supported input.
const ContactSuffix = "-contact"

// Renderer turns a decoded source image into the image to write. It is a
// function rather than an interface because the two commands' recipe types
// are unrelated — bw.Style and ciba.Look share no method set — while both
// render to something satisfying image.Image.
type Renderer func(image.Image) image.Image

// Command describes one rendering command's naming conventions.
type Command struct {
	Name   string // "skyburn" / "ciba": message prefix and temp-file pattern
	Suffix string // "-bw" / "-ciba": marks the command's own output
}

// validate refuses a Command that cannot recognise its own output.
//
// An empty Suffix is not a harmless default. isOwnOutput would become
// strings.HasSuffix(base, ""), which is true of every filename, so Scan would
// skip every image it walked past and hand back nothing; the tool then prints
// "no images found" on a full directory and exits 0. A silent no-op reported
// as success is the hardest failure to notice, and ScanOne's aliasing error
// degrades to naming a `""` suffix at the same time.
//
// While the suffix was a compile-time const in each command this could not
// happen. It can now, and jobplan is shortly a public package with consumers
// in separate repositories, so the guard goes where the invariant lives
// rather than in each caller — the same reasoning as RenderFile's
// unconditional samePath re-check.
func (c Command) validate() error {
	if c.Suffix == "" {
		return errors.New("jobplan: Command.Suffix is empty, which would make every filename look like the command's own output")
	}
	return nil
}

// Job pairs a source image with the file to write.
type Job struct {
	Src string
	Dst string
}

// decodePreference ranks extensions for the duplicate-basename rule. A lower
// number wins.
//
// The same photograph commonly exists as both X.heic and X.jpg — any
// HEIC-to-JPEG converter produces exactly that pairing — and both would otherwise map to one output
// path, letting two workers race and one rendering overwrite the other. The
// already-decoded JPEG is preferred because decoding HEIC is the expensive
// path and the JPEG is derived from it.
func decodePreference(path string) int {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return 0
	case ".png":
		return 1
	case ".heic", ".heif":
		return 2
	}
	return 3
}

// isOwnOutput reports whether base (a filename with its extension stripped)
// names something the command itself wrote, rather than a source photograph.
func (c Command) isOwnOutput(base string) bool {
	return strings.HasSuffix(base, c.Suffix) || strings.HasSuffix(base, ContactSuffix)
}

// Scan finds the images under root and returns one job per distinct
// photograph, along with the paths skipped because a preferred file shared
// their basename.
func (c Command) Scan(root, outDir string, recursive bool) ([]Job, []string, error) {
	if err := c.validate(); err != nil {
		return nil, nil, err
	}

	// Keyed by directory + basename so identically named files in different
	// directories stay separate under -r.
	type key struct{ dir, base string }
	best := map[key]string{}
	var dupes []string

	// An output directory inside the scanned tree would otherwise feed its own
	// contents back in on a later run, nesting one level deeper each time.
	// Under -out the output is named <base>.jpg
	// with no suffix, so isOwnOutput cannot recognise it by name; the
	// directory has to be skipped by path.
	//
	// The one case deliberately NOT skipped is -out naming the scanned root
	// itself. Skipping it would silently yield an empty scan; letting the walk
	// proceed lets the src == dst check below report that fatal aliasing.
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	skipDir := ""
	if outDir != "" {
		if skipDir, err = filepath.Abs(outDir); err != nil {
			return nil, nil, err
		}
		if skipDir == absRoot {
			skipDir = ""
		}
	}

	walk := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir != "" {
				if abs, err := filepath.Abs(path); err == nil && abs == skipDir {
					return fs.SkipDir
				}
			}
			if !recursive && path != root {
				return fs.SkipDir
			}
			return nil
		}
		if !imaging.IsSupported(path) {
			return nil
		}
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if c.isOwnOutput(base) {
			return nil
		}
		k := key{filepath.Dir(path), base}
		prev, ok := best[k]
		if !ok {
			best[k] = path
			return nil
		}
		if decodePreference(path) < decodePreference(prev) {
			best[k] = path
			dupes = append(dupes, prev)
		} else {
			dupes = append(dupes, path)
		}
		return nil
	}

	if err := filepath.WalkDir(root, walk); err != nil {
		return nil, nil, err
	}

	jobs := make([]Job, 0, len(best))
	for k, src := range best {
		dst := filepath.Join(k.dir, k.base+c.Suffix+".jpg")
		if outDir != "" {
			rel, err := filepath.Rel(root, k.dir)
			if err != nil || strings.HasPrefix(rel, "..") {
				rel = "."
			}
			dst = filepath.Join(outDir, rel, k.base+".jpg")
		}
		jobs = append(jobs, Job{Src: src, Dst: dst})
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Src < jobs[j].Src })
	sort.Strings(dupes)
	// Checked after sorting so the path named in the error is deterministic
	// rather than whichever one the map happened to yield first.
	for _, j := range jobs {
		if err := c.checkNotSelf(j); err != nil {
			return nil, nil, err
		}
	}
	return jobs, dupes, nil
}

// resolveDir resolves symlinks in a path's directory component, leaving the
// final element alone because a destination file need not exist yet.
//
// Comparing paths lexically is not enough: a -out directory that is a symlink
// to the source directory produces different strings for the same file, and
// os.Rename resolves the link before replacing the target. That path silently
// overwrote originals.
func resolveDir(p string) string {
	d, b := filepath.Split(filepath.Clean(p))
	if r, err := filepath.EvalSymlinks(filepath.Clean(d)); err == nil {
		d = r
	}
	return filepath.Join(d, b)
}

// samePath reports whether two paths name the same file, comparing them as
// resolved absolute cleaned paths. filepath.Abs cleans as it resolves, so a
// trailing slash, a leading "./", and a relative-versus-absolute mix all
// collapse to the same string; resolveDir then collapses symlinked
// directories, which no amount of lexical cleaning can.
func samePath(a, b string) (bool, error) {
	pa, err := filepath.Abs(a)
	if err != nil {
		return false, err
	}
	pb, err := filepath.Abs(b)
	if err != nil {
		return false, err
	}
	return resolveDir(pa) == resolveDir(pb), nil
}

// checkNotSelf refuses a job whose output path is its own input.
//
// Under -out the output is named <base>.jpg with no suffix, so an -out that
// resolves to the source's own directory makes every destination equal its
// source — and RenderFile renames its result over that path. This is a
// fatal configuration error rather than a per-file skip: the user has asked
// for something that would destroy the originals the tool promises never to
// modify, and skipping would quietly report success on nothing.
func (c Command) checkNotSelf(j Job) error {
	same, err := samePath(j.Src, j.Dst)
	if err != nil {
		return err
	}
	if same {
		return fmt.Errorf("%s: the output path is the source image itself — under -out, output is named <base>.jpg with no %q suffix, so -out must not be the source directory", j.Src, c.Suffix)
	}
	return nil
}

// ScanOne builds the single job for an explicit file argument.
func (c Command) ScanOne(path, outDir string) (Job, error) {
	if err := c.validate(); err != nil {
		return Job{}, err
	}
	if !imaging.IsSupported(path) {
		return Job{}, os.ErrInvalid
	}
	dir := filepath.Dir(path)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	dst := filepath.Join(dir, base+c.Suffix+".jpg")
	if outDir != "" {
		dst = filepath.Join(outDir, base+".jpg")
	}
	j := Job{Src: path, Dst: dst}
	if err := c.checkNotSelf(j); err != nil {
		return Job{}, err
	}
	return j, nil
}
