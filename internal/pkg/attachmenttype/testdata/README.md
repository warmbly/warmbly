`document.docx` is an original short document exported by macOS textutil.
`raster.webp` is `blue-purple-pink.lossless.webp` from golang.org/x/image
v0.45.0 testdata, under the accompanying WEBP-LICENSE.

The tests also produce a real workbook through excelize, an original minimal
presentation package, a classic-xref PDF with calculated object offsets, PNG,
JPEG, GIF and ZIP files through their encoders. All are checked without rewriting
the input bytes.
