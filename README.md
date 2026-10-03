# conv

In simple words, conv is a command runner just to figure out how to convert files.

```sh
conv photo.jpg photo.png
conv '*.jpg' converted/ --ext png
conv --from notes.md --to notes.docx
conv '*.mov' converted/ --ext mp4 --jobs 2
```

Direct, one-file-to-one-file conversions through
FFmpeg, ImageMagick's `magick`, Pandoc, and the standard compression tools. ~~Glob mode and wildcard patterns are on the roadmap, not in this release.~~

```sh
conv x.tar x.tar.gz # Compress a tar archive.
conv x.tar.gz x.tar # And decompress it again.
```

## Prerequisites

> [!IMPORTANT]
> Conv is only a command runner, it does not include runtime command line dependencies. Install the following to make sure `conv` works as expected.

- FFmpeg (`ffmpeg`) for audio and video
- ImageMagick 7 (`magick`) for images
- Pandoc (`pandoc`) for documents
- `gzip`, `bzip2`, `xz` and/or `zstd` for compressed tar archives

Commands (including GNU flavoured tools) must be in `PATH` if you want to use them.

## Install

From source, with Go 1.23 or newer:

```sh
go build -o conv ./cmd/conv
go install github.com/wu21-web/conv/cmd/conv@latest
```

To stamp a build reference, pass `-ldflags`:

```sh
go build -ldflags "-X github.com/wu21-web/conv/internal/version.Build=$(git describe --tags)" -o conv ./cmd/conv
conv -v
```

> [!NOTE]
> You can also download the lastest compiled binary from the [Releases](https://github.com/wu21-web/conv/releases/latest) page. Be aware that macOS gatekeeper might block it.

## Supported conversion pairs

Support conversions pairs are restricted by the backend, not conv, unless conv missed a supported pair or solution in `command.json`. Contributions are welcome.
Compressed tar archives are registered for `gzip`, `bzip2`, `xz` and `zstd`. Creating and extracting archives themselves is still planned, see the roadmap. Conv's aim is to build a converter that converts just about anything.

| Backend     | Input extensions                               | Output |
| ----------- | ---------------------------------------------- | ------ |
| FFmpeg      | mov, mpeg, mpg, avi, mkv, webm, m4v            | mp4    |
| FFmpeg      | mov, mp4, mpeg, mpg, avi, mkv, m4v             | webm   |
| FFmpeg      | mp3, wav, flac, ogg, opus, aac                 | m4a    |
| FFmpeg      | wav, flac, m4a, aac, ogg, opus, aiff, wma      | mp3    |
| FFmpeg      | mp3, m4a, aac, flac, ogg, opus, aiff, wma, wav | wav    |
| ImageMagick | jpg, jpeg                                      | png    |
| ImageMagick | png                                            | jpg    |
| ImageMagick | png                                            | jpeg   |
| ImageMagick | png                                            | tiff   |
| ImageMagick | jpg, jpeg                                      | tiff   |
| ImageMagick | tif, tiff                                      | png    |
| ImageMagick | tif, tiff                                      | jpg    |
| ImageMagick | bmp                                            | png    |
| Pandoc      | md, markdown                                   | docx   |
| Pandoc      | md, markdown                                   | html   |
| Pandoc      | html, htm                                      | docx   |
| Pandoc      | html, htm                                      | md     |
| Pandoc      | docx                                           | md     |
| gzip        | tar                                            | gz     |
| gzip        | gz                                             | tar    |
| bzip2       | tar                                            | bz2    |
| bzip2       | bz2                                            | tar    |
| xz          | tar                                            | xz     |
| xz          | xz                                             | tar    |
| zstd        | tar                                            | zst    |
| zstd        | zst                                            | tar    |

> [!NOTE]
> `gzip`, `bzip2` and `xz` have no output-path option and compress in place by default, which would delete your input. Those recipes stream the result on stdout instead, and conv captures the stream into the staged output. `zstd` accepts `-o`, so it writes the file itself.

## Usage

```text
conv [options] <inputs...> <destination>
conv [options] --in <file-or-pattern> --out <destination>
```

> [!WARNING]
> You must provide an `--ext` value if the you used glob and pattern as an input. **`conv` does not support the destination to be a glob, it must be a directory.** This will only convert the filetype, renaming cannot be done.
> If you wish to batch covert + name, consider using a _for_ loop.

| Flag                   | Meaning                                                                         |
| ---------------------- | ------------------------------------------------------------------------------- |
| `--in`, `--from VALUE` | input file or glob, repeatable                                                  |
| `--out`, `--to VALUE`  | output file or directory                                                        |
| `--ext VALUE`          | target format for directory output, for example `png`                           |
| `--gnu` / `--no-gnu`   | prefer GNU or native variants of equivalent recipes (`--no-gnu` is the default) |
| `--jobs N`             | maximum concurrent conversions, default `1`                                     |
| `--dry-run`            | print the complete plan without converting                                      |
| `--config PATH`        | use the registry at `PATH` instead of the built-in one                          |
| `-s`, `--silent`       | no output at all, including errors                                              |
| `-q`, `--quiet`        | failures only                                                                   |
| `-V`, `--verbose`      | selection, executable, argument and process detail                              |
| `-v`, `--version`      | print the build reference                                                       |
| `-h`, `--help`         | print usage                                                                     |

`--in`/`--from` name inputs and `--out`/`--to` name destinations; none of them
name a format. Options may appear before or after positional arguments,
`--name=value` works for value-taking long options, and `--` ends option
parsing.

Examples:

```sh
./conv picture.png picture.jpg # Convert using ImageMagick as the command backend.
./conv pictures/*.jpg png/ --ext png # Convert pictures/*.jpg to png format and output under png/. No renaming.
./conv -v
./conv pictures/*.png jpg/ --ext jpg -V # Prints all details including subprocess creation.
```

## Inputs

Shell-expanded lists and quoted globs both work. `conv` expands globs itself
with `filepath.Glob`, sorts the matches, and only then picks recipes. A literal
file always wins over glob interpretation, so a real file named
`image[1].jpg` stays OK. Note that you cannot pass malformed inputs.

Paths are normalized and made absolute before anything runs, and repeated
references to the same file (including hard links and symlinks to it) are
converted once.

> [!NOTE]
> We might reject symlinks later on. It is better to dereference them at first in runtime.

Extension matching is **case-insensitive** and **ignores a leading dot**, so
`--ext png`, `--ext .png` and `--ext PNG` are the same request.

## Destinations and naming

An existing directory is directory output, and so is a path ending in the
platform's directory separator, even if it does not exist yet. Directory
output always requires `--ext`, and `--ext` is valid
only with directory output. A new directory is created during a real run.

## Backend selection

For each input/output pair, `conv` looks for registered recipes that are
installed, then orders the candidates by the recipe `priority`. **Recipes with higher priority wins.**

`--gnu` and `--no-gnu` only affect recipes registered as equivalent variants
of each other through `variant_group`.

Two failures that look similar but reported differently:

- Unsupported pair: `no recipe converts "jpg" to "webm" (registered "jpg" targets: png, tiff)`
- Missing backend: `no installed backend for "jpg" to "png": executable "magick" not found in PATH (recipe "magick-jpeg-to-png")`

> [!NOTE]
> To force GNU flavoured coreutils or the system flavoured commands (which means no fallbacks if the requested one is unavaliable), use `-f` or `--force`.

## Roadmap

GNU flavoured tool variants are still to be registered. Compressing and decompressing tar archives landed with `gzip`, `bzip2`, `xz` and `zstd`; what is left is the archive layer itself, which does not fit the one-file-to-one-file model, because it changes how many files go in and come out:

```sh
./conv archive.tar.gz archive/ # Extraction: one input, a directory tree out.
./conv archive/ archive.tar.gz # Packing: a directory tree in, one file out.
```

## Dry run

`--dry-run` does the normal read-only work. It tried to find and print the solution command and information.

```console
$ conv photo.jpg 'second photo.jpeg' out/ --ext png --dry-run
conv dry run: 2 conversion(s), jobs=1
output directory: /home/me/out (will be created)
[1] /home/me/photo.jpg -> /home/me/out/photo.png
    input:  jpg
    recipe: magick-jpeg-to-png (ImageMagick) priority=100 implementation=other
    backend: /usr/bin/magick
    argv: /usr/bin/magick /home/me/photo.jpg "<staged>/photo.png"
[2] /home/me/second photo.jpeg -> /home/me/out/second photo.png
    input:  jpeg
    recipe: magick-jpeg-to-png (ImageMagick) priority=100 implementation=other
    backend: /usr/bin/magick
    argv: /usr/bin/magick "/home/me/second photo.jpeg" "<staged>/second photo.png"
```

Codex plan mode alike, allows you to see if the conversion is possible or not.

```console
$ ./conv x.tar x.tar.gz --dry-run
conv dry run: 1 conversion(s), jobs=1
[1] /home/me/x.tar -> /home/me/x.tar.gz
    input:  tar
    recipe: gzip-tar-to-gz (gzip) priority=100 implementation=other
    backend: /usr/bin/gzip
    argv: /usr/bin/gzip -c -n -- /home/me/x.tar
    capture: stdout -> "<staged>/x.tar.gz"
```

## Concurrency

`--jobs N` sets the maximum number of conversions running at once, and the
default is `1`.
**Temporary files are removed after success, failure and cancellation. This does not have much to do with "Concurrency", but I put this here anyway.**

## Backend Diagnostics

Normal mode writes one line per conversion and a batch summary to stderr.
`--quiet` keeps failures only, `--verbose` adds selection decisions,
executable paths, argument vectors, process starts and backend output, and
`--silent` suppresses everything from `conv` and its children while still
returning a meaningful exit status.

```console
$ conv broken.jpg broken.png
conv: broken.jpg: backend failed: exit status 1
    magick: insufficient image data in file `/home/me/broken.jpg' @ error/jpeg.c/ReadOneJPEGImage/1554.
conv: 1 of 1 conversion(s) failed
```

stdout carries help, version and the dry-run plan; everything else goes to
stderr.

| Exit code | Meaning                                                                                                       |
| --------- | ------------------------------------------------------------------------------------------------------------- |
| 0         | all conversions succeeded, or successful help, version or dry run                                             |
| 1         | input, registry, planning, backend availability, conversion or publication failure, including a partial batch |
| 2         | invalid usage or malformed glob syntax                                                                        |
| 130       | canceled by the user                                                                                          |

## Custom registries

The built-in registry lives in `commands.json` and is embedded in the binary.
Point at a config explicitly with `--config PATH`.

```json
{
  "version": 1,
  "recipes": [
    {
      "id": "pandoc-markdown-to-docx",
      "name": "Pandoc",
      "bin": "pandoc",
      "implementation": "other",
      "priority": 100,
      "inputs": ["md", "markdown"],
      "output": "docx",
      "args": [
        "--from=markdown",
        "--to=docx",
        "{input}",
        "--output",
        "{output}"
      ]
    }
  ]
}
```

Required fields are `id`, `name`, `bin`, `implementation`, `inputs`, `output`
and `args`; `priority` is optional and defaults to `0`. Recipes that are
equivalent alternatives can share a `variant_group` so `--gnu` and `--no-gnu` can choose
between them. A `variant_group` is only valid on recipes whose
`implementation` is `"gnu"` or `"native"`.

`output_mode` is optional and defaults to `"file"`, which means the backend writes the path given as `{output}`. Set it to `"stdout"` when the backend can only stream its result, and conv will capture that stream into the staged output file:

```json
{
  "id": "gzip-tar-to-gz",
  "name": "gzip",
  "bin": "gzip",
  "implementation": "other",
  "priority": 100,
  "inputs": ["tar"],
  "output": "gz",
  "output_mode": "stdout",
  "args": ["-c", "-n", "--", "{input}"]
}
```

A `"stdout"` recipe must not reference `{output}` in its args, because conv owns the output file; asking for both is rejected when the registry loads.

## Development

```sh
gofmt -l .
go vet ./...
go test ./...
go test -race ./...   # where the platform supports it
go build ./cmd/conv
```

Tests this before submission (if modified):

```sh
go test -v -run TestSmoke ./internal/smoke/
```
