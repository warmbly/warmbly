`document.docx` is an original short document exported by macOS textutil.
`document-utf8-bom.docx` and `document-utf16.docx` contain the same document,
with its main XML part encoded as UTF-8 with a BOM or UTF-16LE with a BOM.
The two `*-kids-with-content.pdf` files are original one-page documents with
Helvetica text, calculated classic xref offsets and direct or indirect Kids arrays.
`raster.webp` is `blue-purple-pink.lossless.webp` from golang.org/x/image
v0.45.0 testdata, under the accompanying WEBP-LICENSE.

The tests also produce a real workbook through excelize, an original minimal
presentation package, a classic-xref PDF with calculated object offsets, PNG,
JPEG, GIF and ZIP files through their encoders. All are checked without rewriting
the input bytes.
