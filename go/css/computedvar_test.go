package css

import "testing"

func TestVarComputedColorFamily(t *testing.T) {
	html := `<style>
html { --inherited: red; --blue: blue; --green: green }
.inherit { color: var(--inherited) }
.fallback { color: var(--missing, blue) }
.drop { color: var(--missing) }
.drop { color: var(--green) }
@media (min-width: 500px) { .media { color: var(--inherited) } }
@media (min-width: 900px) { .media { color: blue } }
@media (min-width: 500px) { html { --media-color: red } }
@media (min-width: 900px) { html { --media-color: blue } }
.media-variable { color: var(--media-color, green) }
@layer early { .layer { color: var(--inherited) } }
@layer late { .layer { color: var(--blue) } }
</style>
<div class="inherit"></div><div class="fallback"></div><div class="drop"></div>
<div class="media"></div><div class="media-variable"></div><div class="layer"></div>`
	d, styles := doc(t, html, 800)
	for selector, want := range map[string][3]float64{
		".inherit":        {1, 0, 0},
		".fallback":       {0, 0, 1},
		".drop":           {0, 128.0 / 255, 0},
		".media":          {1, 0, 0},
		".media-variable": {1, 0, 0},
		".layer":          {0, 0, 1},
	} {
		if got := styles[mustSelect(t, d, selector)].Color; !colorNear(got, want[0], want[1], want[2]) {
			t.Errorf("%s color: %+v (want %v)", selector, got, want)
		}
	}
	low, lowStyles := doc(t, html, 400)
	if got := lowStyles[mustSelect(t, low, ".media-variable")].Color; !colorNear(got, 0, 128.0/255, 0) {
		t.Errorf("media-gated custom-property fallback at 400px: %+v (want green)", got)
	}
}

func TestVarComputedFontFamilyGroup(t *testing.T) {
	html := `<style>
html { --family: "Liberation Serif", serif; --size: 24px; --weight: 700; --semibold: 600; --leading: 2; --fixed-leading: 30px; --small: 18px; --large: 26px }
.font { font-family: var(--family); font-size: var(--size); font-weight: var(--weight); line-height: var(--leading) }
.fallback { font-size: var(--missing, 20px) }
.drop { font-weight: var(--missing) }
.drop { font-weight: var(--semibold) }
@media (min-width: 500px) { .media { font-size: var(--size) } }
@media (min-width: 900px) { .media { font-size: 30px } }
@layer early { .layer { font-size: var(--small) } }
@layer late { .layer { font-size: var(--large) } }
</style>
<div class="font"></div><div class="fallback"></div><div class="drop"></div>
<div class="fixed-leading" style="font-size: 20px; line-height: var(--fixed-leading)"></div>
<div class="media"></div><div class="layer"></div>`
	d, styles := doc(t, html, 800)
	font := styles[mustSelect(t, d, ".font")]
	if font.FontFamily != "Liberation Serif" || len(font.FontFamilies) == 0 || font.FontFamilies[0] != "Liberation Serif" {
		t.Errorf("font family: %q %v", font.FontFamily, font.FontFamilies)
	}
	if !near(font.FontSize, 24) || font.FontWeight != 700 || !near(font.LineHeight, 2) {
		t.Errorf("font metrics: size=%v weight=%d line-height=%v", font.FontSize, font.FontWeight, font.LineHeight)
	}
	if got := styles[mustSelect(t, d, ".fallback")].FontSize; !near(got, 20) {
		t.Errorf("fallback font-size: %v (want 20)", got)
	}
	if got := styles[mustSelect(t, d, ".drop")].FontWeight; got != 600 {
		t.Errorf("sibling font-weight: %d (want 600)", got)
	}
	if got := styles[mustSelect(t, d, ".fixed-leading")].LineHeight; !near(got, 1.5) {
		t.Errorf("line-height against plain font-size: %v (want 1.5)", got)
	}
	if got := styles[mustSelect(t, d, ".media")].FontSize; !near(got, 24) {
		t.Errorf("media font-size: %v (want 24)", got)
	}
	if got := styles[mustSelect(t, d, ".layer")].FontSize; !near(got, 26) {
		t.Errorf("layer font-size: %v (want 26)", got)
	}
}

func TestVarComputedBoxFamily(t *testing.T) {
	html := `<style>
html { --space: 12px; --box-width: 180px; --box-height: 90px; --box-max: 240px; --margin-left: 7px; --narrow: 100px; --wide: 200px }
.box { margin: var(--space); padding: var(--missing, 4px 8px); width: var(--box-width); height: var(--box-height); max-width: var(--box-max) }
.drop { margin-left: var(--missing) }
.drop { margin-left: var(--margin-left) }
@media (min-width: 500px) { .media { padding-top: var(--space) } }
@media (min-width: 900px) { .media { padding-top: 30px } }
@layer early { .layer { width: var(--narrow) } }
@layer late { .layer { width: var(--wide) } }
</style>
<div class="box"></div><div class="drop"></div><div class="media"></div><div class="layer"></div>`
	d, styles := doc(t, html, 800)
	box := styles[mustSelect(t, d, ".box")]
	if box.Margin != [4]float64{12, 12, 12, 12} || box.Padding != [4]float64{4, 8, 4, 8} ||
		box.Width == nil || !near(*box.Width, 180) || box.Height == nil || !near(*box.Height, 90) ||
		box.MaxWidth == nil || !near(*box.MaxWidth, 240) {
		t.Errorf("resolved box family: margin=%v padding=%v width=%v height=%v max-width=%v", box.Margin, box.Padding, box.Width, box.Height, box.MaxWidth)
	}
	if got := styles[mustSelect(t, d, ".drop")].Margin[3]; !near(got, 7) {
		t.Errorf("sibling margin-left: %v (want 7)", got)
	}
	if got := styles[mustSelect(t, d, ".media")].Padding[0]; !near(got, 12) {
		t.Errorf("media padding-top: %v (want 12)", got)
	}
	if got := styles[mustSelect(t, d, ".layer")].Width; got == nil || !near(*got, 200) {
		t.Errorf("layer width: %v (want 200)", got)
	}
}

func TestVarComputedGapFamily(t *testing.T) {
	html := `<style>
html { --space: 6px; --small: 3px; --large: 11px; --good-gap: 9px }
.gap { display: flex; gap: var(--space, 2px) }
.drop { display: flex; gap: var(--missing) }
.drop { display: flex; gap: var(--good-gap) }
.fallback { display: flex; gap: var(--missing, 4px 8px) }
@media (min-width: 500px) { .media { display: flex; row-gap: var(--space) } }
@media (min-width: 900px) { .media { row-gap: 30px } }
@layer early { .layer { display: flex; column-gap: var(--small) } }
@layer late { .layer { column-gap: var(--large) } }
</style>
<div class="gap"></div><div class="drop"></div><div class="fallback"></div><div class="media"></div><div class="layer"></div>`
	d, styles := doc(t, html, 800)
	for selector, want := range map[string][2]float64{
		".gap":      {6, 6},
		".drop":     {9, 9},
		".fallback": {4, 8},
		".media":    {6, 0},
		".layer":    {0, 11},
	} {
		style := styles[mustSelect(t, d, selector)]
		if !near(style.RowGap, want[0]) || !near(style.ColumnGap, want[1]) {
			t.Errorf("%s gaps: row=%v column=%v (want %v)", selector, style.RowGap, style.ColumnGap, want)
		}
	}
}
