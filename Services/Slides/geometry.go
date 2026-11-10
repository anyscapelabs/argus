package Slides

import "strings"

// Page geometry. A deck is 10in x 7.5in landscape, matching LibreOffice's
// presentation default; every position is authored in points and scaled to EMU
// (1 point = 12700 EMU) for the PPTX package.
const (
	pageW      = 720.0
	pageH      = 540.0
	marginX    = 60.0
	marginTop  = 90.0
	titleH     = 70.0
	accentH    = 8.0
	bulletsTop = 200.0
	bulletsW   = 600.0
	bulletsH   = 300.0
	emuPerPt   = 12700.0
)

// emu converts a point measurement to EMU.
func emu(v float64) int64 {
	return int64(v * emuPerPt)
}

// halfPt converts a point measurement to the hundredths-of-a-point unit PPTX
// font sizes use.
func halfPt(v float64) int64 {
	return int64(v * 100)
}

// centerX shifts a block of width w so it is horizontally centred on the page.
func centerX(w float64) float64 {
	return (pageW - w) / 2
}

// hex is the six-digit color a PPTX srgbClr wants, uppercased and without '#'.
func hex(c string) string {
	c = strings.TrimPrefix(strings.TrimSpace(c), "#")
	return strings.ToUpper(c)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits [24]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}

func itoa64(n int64) string {
	return itoa(int(n))
}

// escapeXML makes text safe to embed in an OOXML part.
func escapeXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}
