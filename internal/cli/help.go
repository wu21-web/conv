package cli

const helpText = `conv converts files between formats using installed backends.

Usage:
  conv [options] <inputs...> <destination>
  conv [options] --in <file-or-pattern> --out <destination>

Inputs and destination:
  --in, --from VALUE  input file or glob, repeatable
  --out, --to VALUE   output file or directory
  --ext VALUE         target format for directory output (for example "png")

Selection:
  --gnu               prefer registered GNU variants
  --no-gnu            prefer registered native variants (default)
  --jobs N            maximum concurrent conversions (default 1)
  --dry-run           print the complete plan without converting
  --config PATH       use an explicitly supplied recipe registry

Diagnostics:
  -s, --silent        suppress conv messages and backend diagnostics
  -q, --quiet         show failures only
  -V, --verbose       show selection, executable, argument and process detail
  -v, --version       print the build reference
  -h, --help          print this help

Destination rules:
  An existing directory is directory output, and so is a new path that ends
  with the platform directory separator. Directory output requires --ext and
  keeps input basenames; --ext is only valid with directory output. Multiple
  inputs require directory output. Any other destination is an explicit output
  file whose extension decides the target format.

Exit status:
  0 successful conversion, help, version or dry run
  1 input, registry, planning, backend, conversion or publication failure
  2 invalid usage or malformed glob syntax
  130 canceled by the user

Examples:
  conv photo.jpg photo.png
  conv photo.jpg converted/ --ext png
  conv '*.jpg' converted/ --ext png
  conv --from notes.md --to notes.docx
  conv '*.mov' converted/ --ext mp4 --jobs 2 --dry-run
`

func HelpText() string { return helpText }
