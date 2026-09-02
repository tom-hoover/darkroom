package jobplan

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/tom-hoover/darkroom/imaging"
)

// RenderFile converts one image and writes the result.
//
// The output goes to a temporary file and is renamed into place, so an
// interrupted run never leaves a truncated JPEG that a later run would mistake
// for finished work. The original is only ever read.
func (c Command) RenderFile(j Job, render Renderer, quality int) error {
	// Last-resort guard on an irreversible operation. The write below ends in
	// os.Rename onto j.Dst, so a destination equal to the source replaces the
	// original with its own render and no copy of it survives. Scan and ScanOne
	// already refuse such a job; this refuses it again, unconditionally, so a
	// future caller that constructs a Job by hand cannot route around the check.
	same, err := samePath(j.Src, j.Dst)
	if err != nil {
		return err
	}
	if same {
		return fmt.Errorf("refusing to write over the source image %s", j.Src)
	}

	img, exif, err := imaging.Decode(j.Src)
	if err != nil {
		return err
	}

	dir := filepath.Dir(j.Dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+c.Name+"-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	if len(exif) > imaging.MaxExifPayload {
		c.warnf("%s: EXIF block of %d bytes does not fit in a JPEG, dropping it", j.Src, len(exif))
		exif = nil
	}
	if err := imaging.WriteJPEG(tmp, render(img), quality, exif); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// CreateTemp makes the file private; photos should be readable.
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), j.Dst)
}

// warnf reports a non-fatal condition, prefixed with the command's name so a
// message from a worker goroutine says which tool produced it.
func (c Command) warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, c.Name+": warning: "+format+"\n", args...)
}

// Partition splits jobs into those to render and a count whose output already
// exists, so a re-run after adding photos costs nothing.
func Partition(jobs []Job, force bool) (todo []Job, skipped int) {
	for _, j := range jobs {
		if !force {
			if _, err := os.Stat(j.Dst); err == nil {
				skipped++
				continue
			}
		}
		todo = append(todo, j)
	}
	return todo, skipped
}

// RenderAll renders jobs across workers goroutines and returns the number that
// failed. One bad file does not stop the rest of the batch.
func (c Command) RenderAll(jobs []Job, render Renderer, quality, workers int, verbose bool) int {
	if workers > len(jobs) {
		workers = len(jobs)
	}
	queue := make(chan Job)
	var mu sync.Mutex
	failed := 0

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range queue {
				err := c.RenderFile(j, render, quality)
				mu.Lock()
				switch {
				case err != nil:
					failed++
					fmt.Fprintf(os.Stderr, c.Name+": %s: %v\n", j.Src, err)
				case verbose:
					fmt.Printf("%s -> %s\n", j.Src, j.Dst)
				}
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		queue <- j
	}
	close(queue)
	wg.Wait()
	return failed
}
