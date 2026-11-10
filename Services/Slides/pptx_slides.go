package Slides

import "strings"

// slideXML renders one <p:sld> part. Each slide carries its own background
// rect and the shapes its layout asks for; the theme's colors and fonts are put
// directly on the text runs, so the deck renders identically whether or not the
// machine has a matching theme installed.
func slideXML(d Deck, s Slide, t Theme) string {
	var b strings.Builder
	b.WriteString(`<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"`)
	b.WriteString(` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`)
	b.WriteString(` xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">`)
	b.WriteString(`<p:cSld><p:spTree>`)
	b.WriteString(groupProps())
	b.WriteString(bgShape(t))
	switch s.Layout {
	case LayoutTitle:
		b.WriteString(textShape(2, "Title", centerX(600), 180, 600, 120, 36, true, t.Text, t.HeadingFont, "ctr", s.Title))
		b.WriteString(textShape(3, "Subtitle", 60, 320, 600, 60, 18, false, t.Accent, t.BodyFont, "ctr", s.Subtitle))
		b.WriteString(accentBar(centerX(120), 430, t))
	case LayoutSection:
		b.WriteString(textShape(2, "Title", 60, 210, 600, 120, 44, true, t.Text, t.HeadingFont, "ctr", s.Title))
		b.WriteString(accentBar(centerX(120), 430, t))
	case LayoutTwoCol:
		b.WriteString(textShape(2, "Title", marginX, marginTop, bulletsW, titleH, 28, true, t.Text, t.HeadingFont, "l", s.Title))
		b.WriteString(bulletsShape(3, "Left", 60, bulletsTop, 300, s.Left, t))
		b.WriteString(bulletsShape(4, "Right", 420, bulletsTop, 300, s.Right, t))
		b.WriteString(accentBar(marginX, 150, t))
	default:
		b.WriteString(textShape(2, "Title", marginX, marginTop, bulletsW, titleH, 28, true, t.Text, t.HeadingFont, "l", s.Title))
		b.WriteString(bulletsShape(3, "Body", 60, bulletsTop, 600, s.Bullets, t))
		b.WriteString(accentBar(marginX, 150, t))
	}
	b.WriteString(`</p:spTree></p:cSld>`)
	b.WriteString(`<p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr>`)
	b.WriteString(`</p:sld>`)
	return b.String()
}

// groupProps is the required shape-tree group with its transform.
func groupProps() string {
	return `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
		`<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/>` +
		`<a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`
}

// bgShape paints the full page in the theme's background colour.
func bgShape(t Theme) string {
	return `<p:sp><p:nvSpPr><p:cNvPr id="2" name="Background"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr/></p:nvSpPr>` +
		`<p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="` + itoa64(emu(pageW)) + `" cy="` + itoa64(emu(pageH)) + `"/></a:xfrm>` +
		`<a:solidFill><a:srgbClr val="` + hex(t.Background) + `"/></a:solidFill></p:spPr>` +
		`<p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody></p:sp>`
}

// accentBar draws a short horizontal rule in the accent colour.
func accentBar(x, y float64, t Theme) string {
	return `<p:sp><p:nvSpPr><p:cNvPr id="5" name="AccentBar"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>` +
		`<p:spPr><a:xfrm><a:off x="` + itoa64(emu(x)) + `" y="` + itoa64(emu(y)) + `"/>` +
		`<a:ext cx="` + itoa64(emu(120)) + `" cy="` + itoa64(emu(accentH)) + `"/></a:xfrm>` +
		`<a:solidFill><a:srgbClr val="` + hex(t.Accent) + `"/></a:solidFill></p:spPr>` +
		`<p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody></p:sp>`
}

// textShape is one single-paragraph text box.
func textShape(id int, name string, x, y, w, h, sizePt float64, bold bool, color, font, align, content string) string {
	if content == "" {
		return ""
	}
	sz := halfPt(sizePt)
	b := boldFlag(bold)
	return `<p:sp><p:nvSpPr><p:cNvPr id="` + itoa(id) + `" name="` + name + `"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>` +
		`<p:spPr><a:xfrm><a:off x="` + itoa64(emu(x)) + `" y="` + itoa64(emu(y)) + `"/>` +
		`<a:ext cx="` + itoa64(emu(w)) + `" cy="` + itoa64(emu(h)) + `"/></a:xfrm><a:noFill/></p:spPr>` +
		`<p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:pPr algn="` + align + `"/>` +
		`<a:r><a:rPr lang="en-US" sz="` + itoa64(sz) + `" b="` + b + `">` +
		`<a:solidFill><a:srgbClr val="` + hex(color) + `"/></a:solidFill>` +
		`<a:latin typeface="` + font + `"/></a:rPr><a:t>` + escapeXML(content) + `</a:t></a:r></a:p></p:txBody></p:sp>`
}

// bulletsShape is one text box containing a list of bulleted paragraphs.
func bulletsShape(id int, name string, x, y, w float64, items []string, t Theme) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<p:sp><p:nvSpPr><p:cNvPr id="` + itoa(id) + `" name="` + name + `"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>`)
	b.WriteString(`<p:spPr><a:xfrm><a:off x="` + itoa64(emu(x)) + `" y="` + itoa64(emu(y)) + `"/>` +
		`<a:ext cx="` + itoa64(emu(w)) + `" cy="` + itoa64(emu(bulletsH)) + `"/></a:xfrm><a:noFill/></p:spPr>`)
	b.WriteString(`<p:txBody><a:bodyPr/><a:lstStyle/>`)
	for _, item := range items {
		b.WriteString(`<a:p><a:pPr algn="l" marL="0" indent="0"/>`)
		b.WriteString(`<a:r><a:rPr lang="en-US" sz="` + itoa64(halfPt(20)) + `">` +
			`<a:solidFill><a:srgbClr val="` + hex(t.Accent) + `"/></a:solidFill>` +
			`<a:latin typeface="` + t.BodyFont + `"/></a:rPr><a:t>• </a:t></a:r>`)
		b.WriteString(`<a:r><a:rPr lang="en-US" sz="` + itoa64(halfPt(20)) + `">` +
			`<a:solidFill><a:srgbClr val="` + hex(t.Text) + `"/></a:solidFill>` +
			`<a:latin typeface="` + t.BodyFont + `"/></a:rPr><a:t>` + escapeXML(item) + `</a:t></a:r></a:p>`)
	}
	b.WriteString(`</p:txBody></p:sp>`)
	return b.String()
}

func boldFlag(bold bool) string {
	if bold {
		return "1"
	}
	return "0"
}
