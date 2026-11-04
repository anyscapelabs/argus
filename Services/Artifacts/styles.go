// The document stylesheet. One sheet serves every derived format: the browser
// prints it into the PDF, and LibreOffice reads it when converting the same html
// into .docx. That is the whole reason the export pipeline can promise that a
// table looks like a table in all three files — there is one description of what
// a heading, a row or a quote means, and nothing authors it twice.
//
// Headings stop at level 4 because that is where the validator stops too; a
// document cannot contain a construct this sheet has no rule for.
package Artifacts

const documentCSS = `
:root { color-scheme: light; }
* { box-sizing: border-box; }
html { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
body {
  margin: 0 auto;
  padding: 0;
  max-width: 720px;
  color: #111111;
  background: #ffffff;
  font-family: "Google Sans Flex", "Google Sans Text", system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  font-size: 11.5pt;
  line-height: 1.55;
  text-rendering: optimizeLegibility;
}
article { padding: 8px 0 40px; }
h1, h2, h3, h4 { font-weight: 600; line-height: 1.25; margin: 1.6em 0 0.5em; }
h1 { font-size: 20pt; margin-top: 0; }
h2 { font-size: 15pt; }
h3 { font-size: 12.5pt; }
h4 { font-size: 11.5pt; }
.byline { margin: -0.6em 0 1.8em; color: #555555; font-size: 10pt; }
p { margin: 0 0 0.9em; }
a { color: #0b57d0; text-decoration: none; }
a:hover { text-decoration: underline; }
ul, ol { margin: 0 0 0.9em; padding-left: 1.4em; }
li { margin: 0 0 0.3em; }
li::marker { color: #666666; }
ul.task-list { list-style: none; padding-left: 0.2em; }
ul.task-list input { margin-right: 0.5em; }
blockquote {
  margin: 0 0 1em;
  padding: 0.2em 1em;
  border-left: 3px solid #d0d0d0;
  color: #333333;
}
code, pre {
  font-family: "Google Sans Code", ui-monospace, "SFMono-Regular", Menlo, Consolas, monospace;
  font-size: 10pt;
}
code { background: #f2f2f2; border-radius: 3px; padding: 0.1em 0.35em; }
pre {
  background: #f7f7f7;
  border: 1px solid #e2e2e2;
  border-radius: 6px;
  padding: 10px 12px;
  overflow-x: auto;
  white-space: pre-wrap;
  page-break-inside: avoid;
}
pre code { background: none; padding: 0; }
table {
  width: 100%;
  border-collapse: collapse;
  margin: 0 0 1.2em;
  font-size: 10.5pt;
  page-break-inside: avoid;
}
th, td { border: 1px solid #dddddd; padding: 6px 9px; text-align: left; vertical-align: top; }
th { background: #f5f5f5; font-weight: 600; }
tbody tr:nth-child(even) td { background: #fafafa; }
img { max-width: 100%; height: auto; display: block; margin: 1em 0; }
hr { border: none; border-top: 1px solid #dddddd; margin: 1.8em 0; }
del { color: #777777; }
h2, h3, h4 { page-break-after: avoid; }
`
