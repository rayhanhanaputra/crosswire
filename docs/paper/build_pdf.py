"""Render crosswire-paper.md as a two-column academic PDF.

    uv run --with markdown --with weasyprint docs/paper/build_pdf.py

Writes crosswire-paper.html (intermediate) and crosswire-paper.pdf beside the
source. Pure CSS paged media; no LaTeX needed.
"""
import pathlib
import re

import markdown
from weasyprint import HTML

HERE = pathlib.Path(__file__).resolve().parent
SRC = HERE / "crosswire-paper.md"
text = SRC.read_text()

# Title and author line come from the first two lines of the markdown.
lines = text.split("\n")
title = lines[0].lstrip("# ").strip()
author_line = next(l for l in lines[1:] if l.strip()).strip()
body_md = "\n".join(lines[lines.index(author_line) + 1:])

# Author line: "**[Author], [affiliation], [email]** · Draft of ... · Target: ..."
parts = [p.strip() for p in author_line.split("·")]
authors = parts[0].strip("* ")
note = " · ".join(parts[1:])

# Drop the "The first draft ... are kept." housekeeping sentence from the note if present.
note = re.sub(r"\s*The first draft.*$", "", note)

# Abstract: the section headed "## Abstract" becomes a styled block.
m = re.search(r"## Abstract\n\n(.*?)\n\n## ", body_md, re.S)
abstract = m.group(1).strip() if m else ""
body_md = body_md.replace(m.group(0), "\n\n## ", 1) if m else body_md

# Table captions: an italic paragraph starting with "*Table N." right after a table.
body_md = re.sub(r"\n\*(Table \d+\..*?)\*\n", r'\n<p class="caption">\1</p>\n', body_md)

html_body = markdown.markdown(body_md, extensions=["tables", "smarty"])
abstract_html = markdown.markdown(abstract, extensions=["smarty"])

# Numbered sections are already numbered in the source ("## 1. Introduction").
CSS = """
@import url('https://fonts.googleapis.com/css2?family=STIX+Two+Text:ital,wght@0,400;0,700;1,400;1,700&family=Source+Code+Pro:wght@400&display=swap');
@page { size: A4; margin: 22mm 18mm 22mm 18mm;
  @bottom-center { content: counter(page); font: 9pt 'STIX Two Text', 'DejaVu Serif', serif; color: #333; } }
html { font-family: 'STIX Two Text', 'Liberation Serif', 'Times New Roman', 'DejaVu Serif', serif; font-size: 9.6pt; line-height: 1.28; color: #111; }
body { margin: 0; }
header { column-span: all; text-align: center; margin-bottom: 10pt; }
header h1 { font-size: 17pt; font-weight: bold; margin: 0 0 8pt 0; line-height: 1.2; }
header .authors { font-size: 10.5pt; margin: 0 0 3pt 0; }
header .note { font-size: 8.5pt; color: #444; margin: 0 0 6pt 0; }
.abstract { margin: 0 0 8pt 0; padding: 0 0 6pt 0; border-bottom: 0.5pt solid #777; }
.abstract h2 { font-size: 9.6pt; font-weight: bold; margin: 0 0 3pt 0; text-transform: none; }
.abstract p { text-align: justify; margin: 0; font-size: 9pt; }
main { column-count: 2; column-gap: 7mm; column-fill: auto; }
h2 { font-size: 11pt; font-weight: bold; margin: 10pt 0 4pt 0; break-after: avoid; }
h3 { font-size: 9.8pt; font-weight: bold; font-style: italic; margin: 7pt 0 3pt 0; break-after: avoid; }
p { margin: 0 0 5pt 0; text-align: justify; hyphens: auto; orphans: 2; widows: 2; }
p.caption { font-size: 8.3pt; text-align: left; color: #333; margin: 2pt 0 7pt 0; }
ol, ul { margin: 0 0 5pt 0; padding-left: 14pt; }
li { margin: 0 0 2pt 0; text-align: justify; }
strong { font-weight: bold; }
code { font-family: 'Source Code Pro', 'DejaVu Sans Mono', monospace; font-size: 8.4pt; }
table { border-collapse: collapse; width: 100%; font-size: 7.9pt; margin: 4pt 0 2pt 0; break-inside: avoid; }
th, td { padding: 1.6pt 3pt; text-align: left; vertical-align: top; border: none; }
thead tr, tr:first-child { border-top: 0.8pt solid #111; border-bottom: 0.5pt solid #111; }
tr:last-child { border-bottom: 0.8pt solid #111; }
th { font-weight: bold; }
blockquote { margin: 0 0 5pt 0; padding: 0 0 0 6pt; border-left: 1pt solid #999; color: #333; font-size: 8.8pt; }
#references + p, .refs p { font-size: 8.3pt; text-align: left; margin: 0 0 2pt 0; padding-left: 12pt; text-indent: -12pt; }
h2.refs-h { break-before: auto; }
a { color: inherit; text-decoration: none; }
hr { border: none; border-top: 0.5pt solid #999; margin: 6pt 0; }
"""

# References: each "[n] ..." line becomes its own hanging-indent paragraph.
html_body = re.sub(
    r"(<h2>References</h2>)\s*<p>(.*?)</p>",
    lambda mm: mm.group(1) + '<div class="refs">' + "".join(
        f"<p>{line.strip()}</p>" for line in re.split(r"<br\s*/?>|\n", mm.group(2)) if line.strip()) + "</div>",
    html_body, flags=re.S)

doc = f"""<!doctype html><html><head><meta charset="utf-8"><title>{title}</title><style>{CSS}</style></head>
<body>
<header><h1>{title}</h1><p class="authors">{authors}</p><p class="note">{note}</p></header>
<main>
<div class="abstract"><h2>Abstract</h2>{abstract_html}</div>
{html_body}
</main>
</body></html>"""

out_html = HERE / "crosswire-paper.html"
out_pdf = HERE / "crosswire-paper.pdf"
out_html.write_text(doc)
HTML(string=doc, base_url=str(HERE)).write_pdf(str(out_pdf))
print("wrote", out_pdf, out_pdf.stat().st_size, "bytes")
