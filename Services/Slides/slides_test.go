package Slides

import "testing"

func TestParseRequiresTitleAndSlides(t *testing.T) {
	if _, err := Parse(`{}`); err == nil {
		t.Fatal("expected error for empty deck")
	}
	if _, err := Parse(`{"title":"T"}`); err == nil {
		t.Fatal("expected error for deck without slides")
	}
}

func TestParseNormalizesLayouts(t *testing.T) {
	d, err := Parse(`{"title":"T","slides":[{"layout":"TITLE","title":"A"},{"layout":"weird","title":"B","bullets":["x"]}]}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.Slides[0].Layout != LayoutTitle {
		t.Fatalf("want title layout, got %q", d.Slides[0].Layout)
	}
	if d.Slides[1].Layout != LayoutContent {
		t.Fatalf("unknown layout should default to content, got %q", d.Slides[1].Layout)
	}
}

func TestNormalizeThemeFillsDefaultsAndDropsBadColors(t *testing.T) {
	d, err := Parse(`{"title":"T","theme":{"background":"not-a-color","text":"#fff"},"slides":[{"layout":"content","title":"A"}]}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tm := d.Theme
	if tm.Background != DefaultTheme.Background {
		t.Fatalf("invalid color should fall back to default background, got %q", tm.Background)
	}
	if tm.Text != "#fff" {
		t.Fatalf("valid color should be kept, got %q", tm.Text)
	}
	if tm.Accent != DefaultTheme.Accent {
		t.Fatalf("accent should default, got %q", tm.Accent)
	}
}

func TestCountOfAndTitleOf(t *testing.T) {
	body := `{"title":"Q3 Review","slides":[{"layout":"content","title":"a"},{"layout":"content","title":"b"}]}`
	if TitleOf(body) != "Q3 Review" {
		t.Fatalf("title: %q", TitleOf(body))
	}
	if CountOf(body) != 2 {
		t.Fatalf("count: %d", CountOf(body))
	}
}

func TestToPPTXProducesReadablePackage(t *testing.T) {
	d, err := Parse(`{"title":"T","theme":{"background":"#1F2430","accent":"#FF9D6C","text":"#F5F0E6"},"slides":[{"layout":"content","title":"A","bullets":["one","two"]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	data, err := ToPPTX(d)
	if err != nil {
		t.Fatalf("topptx: %v", err)
	}
	for _, want := range []string{"[Content_Types].xml", "ppt/presentation.xml", "ppt/slides/slide1.xml", "ppt/slideMasters/slideMaster1.xml"} {
		if !zipContains(t, data, want) {
			t.Errorf("package missing %q", want)
		}
	}
}

func TestToPPTXCarriesTextAndTheme(t *testing.T) {
	d, err := Parse(`{"title":"Q3","theme":{"background":"#1f2430","accent":"#ff9d6c","text":"#f5f0e6","heading_font":"Georgia"},"slides":[{"layout":"content","title":"Headlines","bullets":["up 18%","churn low"]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	data, err := ToPPTX(d)
	if err != nil {
		t.Fatal(err)
	}
	xml := zipEntryText(t, data, "ppt/slides/slide1.xml")
	for _, want := range []string{"Headlines", "up 18%", "churn low", "Georgia", hex("#ff9d6c"), hex("#f5f0e6")} {
		if !contains(xml, want) {
			t.Errorf("slide missing %q", want)
		}
	}
}
