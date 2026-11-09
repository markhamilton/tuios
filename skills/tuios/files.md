# Files

The daemon answers file verbs for paths on this machine. Send one verb
yourself with `tuios call VERB '{...}'`. A client that is not the CLI writes
JSON to `$TUIOS_SOCKET` (`tuios --skill events`). `list-verbs` lists every
parameter.

```sh
tuios list-verbs file-list
```

A pane needs `admin` (`tuios --skill grants`). Under the default
`mode = "open"` every pane holds `admin`.

## The file verbs

- `file-list` lists a folder, folders first, in natural order. `file-stat`
  describes one path.
- `file-read` reads up to 4 MiB, base64. `file-hash` is the sha256 of a file
  or a range of it. `file-preview` makes a small preview of a file: a scaled
  image, a PDF page, a film frame, or the start of a text file.
- `file-mkdir`, `file-rename` and `file-remove` change files.
  `file-remove` of a folder that holds files needs `recursive`, and never
  removes the home folder or `/`. Ask the person before you remove what you
  did not make.

The error codes are `no_file`, `file_exists`, `no_permission`,
`cross_device` and `disk_full` (`tuios --skill errors`).
