# darkroom

`darkroom` is a small collection of image-processing plumbing extracted from
a private monorepo, where it was shared by three command-line photo tools:
`heic2jpg`, `skyburn`, and `ciba`. It is published as four packages here
because those three tools depend on it, not because it set out to be a
general-purpose imaging library. Before adopting it, judge it against that:
the surface is whatever those three commands needed, and nothing more.

```
go get github.com/tom-hoover/darkroom
```

## Packages

**`imaging`** decodes JPEG, PNG, and HEIC/HEIF source images, applying the
EXIF orientation tag to the pixels themselves and normalising the tag on the
image it hands back — so a caller never has to reason about upside-down or
sideways pixels, and a format with no orientation field of its own (PNG)
still comes out right side up. It also writes JPEGs that carry an EXIF block
through, so capture date, GPS, and orientation survive a render.

**`tone`** is the pixel-math kernel underneath the rendering pipelines: the
sRGB transfer function in both directions, a normalised sigmoidal contrast
curve, a three-pass box blur used as a fast approximation of a Gaussian, and
`RadiusPx`, the helper that turns a spatial parameter into a pixel count. See
below for why that last one is not as simple as it looks.

**`sheet`** computes the geometry of a labelled contact sheet — tile layout,
padding, label placement — without opinions about how a tile gets rendered.
The caller supplies the renderer, so the same layout logic serves more than
one pipeline.

**`jobplan`** turns a source directory into a batch job: it finds the images
under a target, decides where each one's output belongs, and refuses to
build a plan that would overwrite an original. Writes go through a temporary
file and an atomic rename, so an interrupted run never leaves a truncated
image behind.

## Stability

This is `v0`. The API is not stable and may change without notice.

## The one constraint that isn't visible in the code's shape

`tone.RadiusPx` takes its spatial parameter as a **fraction of the image's
short edge**, never a pixel count. That's what lets a small contact-sheet
tile predict what the same setting will look like on a full-resolution
render: the same fraction, applied at two different sizes, is supposed to
produce the same picture, just at two different scales.

That property is easy to break by accident and hard to notice when it
breaks. Any cap, clamp, floor, or narrowing conversion added to `RadiusPx`
would silently make every caller resolution-dependent — a tile and its full
render would quietly stop agreeing — and a whole-image difference test
cannot catch this above a certain size, because the discrepancy it produces
is too small a fraction of the image to trip a typical tolerance. It would
not fail loudly; it would just be wrong.

`TestRadiusPxIsProportionalAtEveryScale`, in `tone/tone_test.go`, exists to
catch exactly that: it checks the effective fraction produced by `RadiusPx`
against the fraction requested, across a wide range of image sizes, and
fails if they drift apart by more than what rounding to a whole pixel can
explain. If a change to `RadiusPx` makes that test fail, the right response
is not to relax the test — it's to treat the failure as a report that the
change altered the contract, and reconsider the change.

## History

This repository's `git log` begins at a single import commit; it does not
carry the development history of these packages. That history, along with
the design specs and a lessons file that recorded some of the reasoning
behind decisions like the one above, lives in a private monorepo and isn't
published here. Where that context matters, it has been folded into the code
as comments instead — so if you're looking for the reasoning behind a guard
or an invariant, look in the source before `git blame`, which won't have it.
