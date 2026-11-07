`macro01.xlsm` is the unchanged Excel-produced comparison fixture from
[XlsxWriter](https://github.com/jmcnamara/XlsxWriter/blob/af5927c110cf01571046a3138600f63b256ea89a/xlsxwriter/test/comparison/xlsx_files/macro01.xlsm),
revision `af5927c110cf01571046a3138600f63b256ea89a`, under the accompanying
`XLSXWRITER-LICENSE` (BSD-2-Clause).

Upstream's `test_macro01.py` identifies the reference as created by Excel and
compares it with a workbook whose A1 cell contains `123`. It has the
macro-enabled workbook content type and an `xl/vbaProject.bin` part.
Static VBA source inspection shows a recorded `Macro1` that selects A1 and
sets its value to `123`; the workbook and sheet modules have no procedure.
The tests read cell data and never execute VBA. This finite fixture does not
establish compatibility with every macro producer or certify arbitrary macros.

SHA-256: `09c35d1580eb6d7e678ba8249cdd1cbc0bd245fbb0eed8794981728715944736`.
