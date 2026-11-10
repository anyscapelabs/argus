package Slides

import "fmt"

// pptxMime is the package media type for a PowerPoint presentation; it must be
// the first, stored entry of the zip.
const pptxMime = "application/vnd.openxmlformats-officedocument.presentationml.presentation"

// ToPPTX renders a deck as a PowerPoint package entirely in Go — no LibreOffice,
// no external tool — so a deck can be produced on any platform the app runs on.
// The package is a zip whose parts are written by the same hand-rolled writer
// the ODP path used (see zip.go), because the standard writer's data-descriptor
// flag trips strict OOXML readers.
func ToPPTX(d Deck) ([]byte, error) {
	n := len(d.Slides)
	entries := []zipEntry{
		{name: "mimetype", data: []byte(pptxMime), store: true},
		{name: "[Content_Types].xml", data: []byte(fmt.Sprintf(contentTypesXML, slideContentTypes(n)))},
		{name: "_rels/.rels", data: []byte(rootRelsXML)},
		{name: "docProps/core.xml", data: []byte(coreXML)},
		{name: "docProps/app.xml", data: []byte(appXML)},
		{name: "ppt/presentation.xml", data: []byte(presentationXML(n))},
		{name: "ppt/_rels/presentation.xml.rels", data: []byte(presentationRelsXML(n))},
		{name: "ppt/slideMasters/slideMaster1.xml", data: []byte(slideMasterXML)},
		{name: "ppt/slideMasters/_rels/slideMaster1.xml.rels", data: []byte(slideMasterRelsXML)},
		{name: "ppt/slideLayouts/slideLayout1.xml", data: []byte(slideLayoutXML)},
		{name: "ppt/slideLayouts/_rels/slideLayout1.xml.rels", data: []byte(slideLayoutRelsXML)},
		{name: "ppt/theme/theme1.xml", data: []byte(theme1XML)},
		{name: "ppt/presProps.xml", data: []byte(presPropsXML)},
		{name: "ppt/viewProps.xml", data: []byte(viewPropsXML)},
		{name: "ppt/tableStyles.xml", data: []byte(tableStylesXML)},
	}
	for i, s := range d.Slides {
		idx := i + 1
		entries = append(entries,
			zipEntry{name: "ppt/slides/slide" + itoa(idx) + ".xml", data: []byte(slideXML(d, s, d.Theme))},
			zipEntry{name: "ppt/slides/_rels/slide" + itoa(idx) + ".xml.rels", data: []byte(slideRelsXML)},
		)
	}
	return buildZip(entries)
}
