package css

import "testing"

// The rewriter is ported verbatim in behaviour from
// src/css_libcss.rs (rewrite_gap_carriers et al): the gap
// declarations travel in the min-width (row) carrier and native column-gap,
// the author's own min-width is dropped while max-width is retained, the rest of the
// stylesheet is copied byte for byte, and @font-face never reaches libcss.
// The expectations below mirror the Rust rewriter traced rule by rule:
// a substituted declaration is written WITHOUT the whitespace that
// surrounded the author's declaration (the carriers carry no surrounding
// spaces), while untouched declarations and trivia are copied verbatim.
// Each case pins the row-carrier/native-column-gap separator and the verbatim passthrough.

type rewriteCase struct {
	name string
	in   string
	out  string
}

func runRuleListCases(t *testing.T, cases []rewriteCase) {
	t.Helper()
	for _, tc := range cases {
		if got := RewriteGapCarriers(tc.in); got != tc.out {
			t.Errorf("%s:\n  in:  %q\n  got: %q\n  exp: %q", tc.name, tc.in, got, tc.out)
		}
	}
}

func TestGapCarrierRewrites(t *testing.T) {
	runRuleListCases(t, []rewriteCase{
		{
			"gap shorthand writes row carrier and native column gap",
			"#gap { display: flex; gap: 20px }",
			"#gap { display: flex;min-width:20px;column-gap:20px}",
		},
		{
			"gap shorthand two values splits across the carriers",
			"#gap { gap: 10px 20px }",
			"#gap {min-width:10px;column-gap:20px}",
		},
		{
			"row-gap only writes the row carrier",
			"div { row-gap: 4px }",
			"div {min-width:4px}",
		},
		{
			"column-gap remains a native computed property",
			"div { column-gap: 8px }",
			"div {column-gap:8px}",
		},
		{
			"longhand row-gap and column-gap side by side",
			"div { row-gap: 4px; column-gap: 8px }",
			"div {min-width:4px;column-gap:8px}",
		},
		{
			"important flag travels into both rewritten gap axes",
			"div { gap: 20px !important }",
			"div {min-width:20px!important;column-gap:20px!important}",
		},
		{
			"author min-width is dropped to free the row carrier",
			"div { min-width: 5px }",
			"div {}",
		},
		{
			"author max-width is preserved for layout",
			"div { max-width: 10px }",
			"div { max-width: 10px }",
		},
		{
			"dropped declarations leave their separator in place",
			"div { min-width: 5px; max-width: 10px }",
			"div {; max-width: 10px }",
		},
		{
			"the two carriers of one gap: exactly one separator in between",
			"div { gap: 10px 20px !important }",
			"div {min-width:10px!important;column-gap:20px!important}",
		},
		{
			"verbatim declaration is copied byte for byte",
			"div { color: red; font-weight: bold }",
			"div { color: red; font-weight: bold }",
		},
		{
			"unrelated selectors keep their spacing verbatim",
			"a ,\n b { color: blue }",
			"a ,\n b { color: blue }",
		},
	})
}

// Pin the actual max-width loss point found during the about-card
// investigation: before mapping or layout sees the declaration, the
// rewriter must leave it in the stylesheet text for libcss to compute.
func TestRewriterPreservesMaxWidthForLayout(t *testing.T) {
	const authored = "#card { max-width: 950px }"
	if got := RewriteGapCarriers(authored); got != authored {
		t.Errorf("rewritten stylesheet = %q, want max-width declaration preserved as %q", got, authored)
	}
}

func TestGapCarrierRewritesInsideAtRules(t *testing.T) {
	runRuleListCases(t, []rewriteCase{
		{
			"@media groups recurse into rule lists",
			"@media (min-width: 500px) { #x { gap: 20px } span { min-width: 8px } }",
			"@media (min-width: 500px) { #x {min-width:20px;column-gap:20px} span {} }",
		},
		{
			"@import is not a block: copied verbatim",
			"@import url(x.css); div { color: red }",
			"@import url(x.css); div { color: red }",
		},
		{
			"@page body is treated as declarations",
			"@page { gap: 8px }",
			"@page {min-width:8px;column-gap:8px}",
		},
		{
			"a keyframes-like at-rule groups rules",
			"@spin-keyframes { 0% { min-width: 1px } }",
			"@spin-keyframes { 0% {} }",
		},
		{
			"a stray closing brace is copied",
			"} div { gap: 4px }",
			"} div {min-width:4px;column-gap:4px}",
		},
		{
			"a comment before the property keeps the declaration verbatim",
			"div::before { content: \";\"; /* gap: 99px */ column-gap: 2px }",
			"div::before { content: \";\"; /* gap: 99px */ column-gap: 2px }",
		},
	})
}

func TestFontFamilyNormalizationRewrites(t *testing.T) {
	runRuleListCases(t, []rewriteCase{
		{
			"explicit keyword declaration is already normal: verbatim",
			"p { font-family: serif }",
			"p { font-family: serif }",
		},
		{
			"css-wide keyword keeps the declaration verbatim (no implicit tail)",
			"p { font-family: inherit }",
			"p { font-family: inherit }",
		},
		{
			"unquoted multi-word name gains quotes and the sans tail",
			"p { font-family: T10 Sans Test }",
			"p {font-family:\"T10 Sans Test\", sans-serif}",
		},
		{
			"quoted name-only declaration gains the implicit sans tail",
			"p { font-family: \"T10 Webfont Test\" }",
			"p {font-family:\"T10 Webfont Test\", sans-serif}",
		},
		{
			"generic-like identifier inside an unquoted sequence gains quotes",
			"p { font-family: Liberation Serif }",
			"p {font-family:\"Liberation Serif\", sans-serif}",
		},
		{
			"named stack with explicit generic already normal: verbatim",
			"p { font-family: \"NoSuchFont XYZ\", serif }",
			"p { font-family: \"NoSuchFont XYZ\", serif }",
		},
		{
			"unquoted multi-word name in a stack gains quotes",
			"p { font-family: NoSuchFont XYZ, serif }",
			"p {font-family:\"NoSuchFont XYZ\", serif}",
		},
		{
			"important carries over the rewritten value only, not verbatim",
			"p { font-family: T10 Sans Test !important }",
			"p {font-family:\"T10 Sans Test\", sans-serif!important}",
		},
		{
			"monospace keyword is verbatim even where the UA sheet uses it",
			"pre, code, kbd, samp { font-family: monospace }",
			"pre, code, kbd, samp { font-family: monospace }",
		},
		{
			"stack of a name without spaces keeps verbatim form",
			"p { font-family: MyFont }",
			"p {font-family:MyFont, sans-serif}",
		},
	})
}

func TestInlineDeclarationListRewrites(t *testing.T) {
	// The style="" attribute is a declaration list: the same rewriter with
	// the declaration-list walker (no rule bodies).
	tests := []struct {
		name string
		in   string
		out  string
	}{
		{"gap shorthand in an inline list", "gap: 20px", "min-width:20px;column-gap:20px"},
		{"two-value gap in an inline list", "gap: 10px 20px", "min-width:10px;column-gap:20px"},
		{"author max-width preserved inline", "max-width: 9px; color: red", "max-width: 9px; color: red"},
		{"font-family quoting inline", "font-family: T10 Sans Test", "font-family:\"T10 Sans Test\", sans-serif"},
		{"verbatim inline declarations", "color: red; letter-spacing: 1.5px", "color: red; letter-spacing: 1.5px"},
		{"quoted strings survive the semicolon walk", "content: \"a;b\"; color: red", "content: \"a;b\"; color: red"},
	}
	for _, tc := range tests {
		if got := RewriteGapDeclarations(tc.in); got != tc.out {
			t.Errorf("%s:\n  in:  %q\n  got: %q\n  exp: %q", tc.name, tc.in, got, tc.out)
		}
	}
}

func TestEmptyAndTrivialSheetsCopyVerbatim(t *testing.T) {
	if got := RewriteGapCarriers(""); got != "" {
		t.Errorf("empty sheet: %q", got)
	}
	runRuleListCases(t, []rewriteCase{
		{"blank rule body copies verbatim", "p {}", "p {}"},
		{"prelude-only text copies", "p", "p"},
		{"trailing comment copies", "p {} /* tail */", "p {} /* tail */"},
	})
}
