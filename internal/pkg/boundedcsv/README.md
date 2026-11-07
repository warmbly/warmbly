# Bounded CSV reader

This internal package contains the Go **1.26.10** `encoding/csv` reader, with
one optional per-record field limit. The package name remains `csv`, and
`reader_test.go` is the unchanged upstream reader test file. No writer is copied.
The Go Authors' BSD 3-Clause license is retained in `LICENSE` and the source headers.
The Go lint configuration exempts only this pinned test file's upstream
`reflect.DeepEqual` error comparisons from the `deepequalerrors` diagnostic.

## Source provenance

The sources are pinned to the `go1.26.10` tag of `golang/go`:

| Local file | Upstream source | Upstream SHA-256 |
| --- | --- | --- |
| `reader.go` (derivative) | https://github.com/golang/go/blob/go1.26.10/src/encoding/csv/reader.go | `c37960d74220590547febce08339aa321f4d85c524740451e14692ebbc9fef1e` |
| `reader_test.go` (unchanged) | https://github.com/golang/go/blob/go1.26.10/src/encoding/csv/reader_test.go | `290e930b31250102589928f953c39b4fee0159f794f7b68d38f90862c3b72f38` |
| `LICENSE` (unchanged) | https://github.com/golang/go/blob/go1.26.10/LICENSE | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |

## Local contract

`MaxFieldsPerRecord` defaults to zero, preserving stock unlimited behavior.
Positive values limit each logical record; nonpositive values are unlimited.
At the start of the upstream `parseField` loop, the reader checks the number of
completed fields before parsing another field or appending its metadata. A final
empty field after a delimiter counts. Quoted delimiters and quoted newlines stay
inside their field, using the upstream parser.

On exceeding the limit, `Read` returns a nil record and a `ParseError` wrapping
`ErrFieldLimit`. Its position identifies the next field, before trimming leading
space. `ReadAll` returns nil records on any error, as upstream does. A limit error
stops parsing the record; callers must stop reading that input. Buffering can read
ahead, so `InputOffset` after this error is not a record-resumption boundary.
Other parsing errors retain upstream partial-record behavior and positions.

The complete local derivative consists of the package documentation adjustment,
the new error, the optional field, the `Read` documentation, and the guard.
Unbounded parsing, quote handling, line normalization and delimiter validation
remain upstream code. `limit_test.go` checks the public limit contract and bounded,
deterministic differential grammar/error cases against stock `encoding/csv`.

Only `attachmenttype.Validate`, the new campaign attachment upload classifier,
enables the 4096-field ceiling for CSV and TSV. Delimiter detection remains in
`spreadsheet.Delimiter`. Spreadsheet imports continue to use stock `encoding/csv`.
Existing attachments, restoration, duplication and queued/internal transport keep
their preservation paths. No stored representation or rollback contract changes.

This bounds field metadata length and result field count, allowing normal bounded
slice-capacity growth. Physical-line and record-byte buffers still depend on input
size; the upload boundary caps the file at 15 MiB. This is not a total heap or
concurrency budget.

## Maintenance

Warmbly backend maintainers own this derivative alongside `attachmenttype`.
On each supported Go toolchain update, review upstream CSV reader fixes, compare
`reader.go` against the pinned tag, and rebase only the local additions listed
above. Copy the new upstream reader tests unchanged, retain its license, update
the version, source links and hashes here, and check the resulting diff directly.
Do not add a second grammar or migrate other importers incidentally.

Run these checks with the supported toolchain before accepting an update:

```sh
go test -count=1 ./internal/pkg/boundedcsv ./internal/pkg/attachmenttype
make fmt
make lint
```

Also rerun the upload-handler and attachment preservation tests. Verify both zero
and enabled limits, trailing empty fields, multiline/CRLF and malformed inputs,
and metadata growth with small limits. Keep the new-upload limit documentation
and composer helper aligned. Remove this copy only when stock Go offers the same
pre-field guard and the compatibility tests prove the replacement's behavior.
