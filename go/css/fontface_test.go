package css

import "testing"

// @font-face blocks never reach libcss as ordinary rules (the rewriter
// strips them); their family/src/weight/style data is extracted here and
// handed back to the caller, mirroring src/css_libcss.rs
// (font_face_declarations) and the layouts of test/data/typography-*.html.
// Fonts themselves are fetched by a later wave (no net in this unit), so
// the extraction pins only the declaration-level contract, including
// supported-src selection.

func TestFontFaceExtraction(t *testing.T) {
	css := `@media screen {
  @font-face {
    font-family: "T10 Webfont Test";
    src: url("typography-webfont-test.ttf") format("truetype");
    font-weight: 400;
    font-style: normal;
  }
}
@font-face { font-family: "Woff2 Only"; src: url("typography-missing.woff2") format("woff2"); }
@font-face { font-family: "No Src"; }
@font-face { src: url("x.ttf") format("truetype"); }
@font-face {
  font-family: "Mixed Src";
  src: url("typography-missing.woff2") format("woff2"),
       url("typography-webfont-test.ttf") format("truetype");
}
p { color: red }`
	faces := FontFaceDeclarations(css)
	want := []FontFace{
		{
			Family: "T10 Webfont Test",
			Src:    `url("typography-webfont-test.ttf") format("truetype")`,
			Weight: "400",
			Style:  "normal",
		},
		{
			Family: "Woff2 Only",
			Src:    `url("typography-missing.woff2") format("woff2")`,
		},
		{
			Family: "Mixed Src",
			Src: `url("typography-missing.woff2") format("woff2"),
       url("typography-webfont-test.ttf") format("truetype")`,
		},
	}
	if len(faces) != len(want) {
		t.Fatalf("faces: got %d (%+v), want %d", len(faces), faces, len(want))
	}
	for i := range want {
		if faces[i] != want[i] {
			t.Errorf("face %d:\n got %+v\nwant %+v", i, faces[i], want[i])
		}
	}
}

func TestSupportedFontSrcSelection(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string // "" means none supported
	}{
		{"first supported entry wins", `url(a.ttf) format("truetype"), url(b.otf) format("opentype")`, "a.ttf"},
		{"woff skips in favour of a later truetype", `url(x.woff2) format("woff2"), url(y.ttf) format("truetype")`, "y.ttf"},
		{"truetype hint accepted", `url("fonts/a.ttf") format("truetype")`, "fonts/a.ttf"},
		{"opentype hint accepted", `url(a.otf) format("opentype")`, "a.otf"},
		{"extension accepted without a format", `url(a.ttf)`, "a.ttf"},
		{"otf extension accepted", `url(a.otf)`, "a.otf"},
		{"woff2 extension rejected", `url(a.woff2)`, ""},
		{"unknown format rejected", `url(a.ttf) format("cff2")`, ""},
		{"local() alone is not a src", `local("T10 Webfont Test")`, ""},
		{"empty url rejected", `url("") format("truetype")`, ""},
		{"no url at all", `local("X"), format("truetype")`, ""},
	}
	for _, tc := range tests {
		got := SupportedFontSrc(tc.src)
		if got != tc.want {
			t.Errorf("%s: SupportedFontSrc(%q) = %q, want %q", tc.name, tc.src, got, tc.want)
		}
	}
}

func TestTokenArgsStripsQuotesAndCases(t *testing.T) {
	if got := TokenArgs(`URL("a.ttf") format("truetype")`, "url"); got != "a.ttf" {
		t.Errorf("url token: %q", got)
	}
	if got := TokenArgs(`url( a.ttf )`, "url"); got != "a.ttf" {
		t.Errorf("trimmed url token: %q", got)
	}
	if got := TokenArgs(`url("x.ttf") FORMAT("truetype")`, "format"); got != "truetype" {
		t.Errorf("format token: %q", got)
	}
	if got := TokenArgs(`url(  'm.ttf'  )`, "url"); got != "m.ttf" {
		t.Errorf("single-quoted token: %q", got)
	}
	if got := TokenArgs(`urlized`, "url"); got != "" {
		t.Errorf("token without parens is not a value: %q", got)
	}
}

func TestSplitCommaListKeepsNestedCommas(t *testing.T) {
	items := SplitCommaList(`url("a,b.ttf"), url(c.ttf) format("x,y") , serif`)
	want := []string{
		`url("a,b.ttf")`,
		` url(c.ttf) format("x,y") `,
		` serif`,
	}
	if len(items) != len(want) {
		t.Fatalf("got %q, want %q", items, want)
	}
	for i := range want {
		if items[i] != want[i] {
			t.Errorf("item %d: got %q, want %q", i, items[i], want[i])
		}
	}
}

func TestFontFaceQuirksSurvive(t *testing.T) {
	// A comment BEFORE the property hides it: split_declaration only skips
	// comments while scanning, so the comment text stays part of the
	// property and no declaration matches (the Rust oracle drops the whole
	// face for the same reason — pinned here, byte for byte).
	css := `@font-face {
  /* the family carries quotes: stripped */
  font-family: "Go Sans";
  src: /* mid-comment */ url('go-sans.woff2') format("woff2"),
       url("go-sans.ttf");
}`
	if faces := FontFaceDeclarations(css); len(faces) != 0 {
		t.Fatalf("comment-hidden property: got %d faces: %+v", len(faces), faces)
	}

	// The same block with the comments only inside values: extracted, with
	// the src kept raw verbatim and the extension-only fallback applying.
	clean := `@font-face {
  font-family: "Go Sans";
  src: url('go-sans.woff2') format("woff2"),
       url("go-sans.ttf");
}`
	faces := FontFaceDeclarations(clean)
	if len(faces) != 1 {
		t.Fatalf("got %d faces: %+v", len(faces), faces)
	}
	if faces[0].Family != "Go Sans" || faces[0].Src != `url('go-sans.woff2') format("woff2"),
       url("go-sans.ttf")` {
		t.Errorf("block content: %+v", faces[0])
	}
	if got := SupportedFontSrc(faces[0].Src); got != `go-sans.ttf` {
		t.Errorf("extension-only fallback inside a multi-item src: %q", got)
	}
}
