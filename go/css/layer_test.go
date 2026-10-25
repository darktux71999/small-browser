package css

import "testing"

// Cascade layers in the var()/custom-property manual cascade (meta1 S10,
// the codeberg nav background). libcss 0.9.2 predates CSS Cascade 5 and
// drops @layer blocks whole, so layered declarations only ever live in
// the rewriter's side table. The engine orders that side table by the
// layer rules: unlayered normal declarations beat layered ones, a later
// layer beats an earlier one, and !important reverses the layer order
// (CSS Cascade 5 §6.4.2 and §6.4.3; "layer order" is the first-appearance
// order of the layer names in the document).

func TestCustomPropertyUnlayeredBeatsLayered(t *testing.T) {
	// The layered declaration is LAST in document order — without layer
	// awareness the side table's document-order tie-break would pick it.
	_, cp, a, _, _ := customFixture(t,
		"#a { --color: #222222 } @layer theme { #a { --color: #111111 } }")
	if got := cp.EffectiveMap(a)["--color"]; got != "#222222" {
		t.Errorf("layered last: --color = %q, want the unlayered #222222", got)
	}
	// And the reverse placement: unlayered still wins.
	_, cp, a, _, _ = customFixture(t,
		"@layer theme { #a { --color: #111111 } } #a { --color: #222222 }")
	if got := cp.EffectiveMap(a)["--color"]; got != "#222222" {
		t.Errorf("layered first: --color = %q, want the unlayered #222222", got)
	}
}

func TestCustomPropertiesLaterLayerBeatsEarlier(t *testing.T) {
	// Layer order is by name appearance: a is layer 1, b is layer 2 — so
	// the SECOND-appearance a declaration below, though last in document
	// order, still loses to b.
	_, cp, a, _, _ := customFixture(t,
		`@layer a { #a { --color: #aaaaaa } }`+
			`@layer b { #a { --color: #bbbbbb } }`+
			`@layer a { #a { --color: #ff0000 } }`)
	if got := cp.EffectiveMap(a)["--color"]; got != "#bbbbbb" {
		t.Errorf("--color = %q, want #bbbbbb from the later layer", got)
	}
}

func TestCustomPropertiesImportantReversesLayerOrder(t *testing.T) {
	// !important reverses the layer order: the EARLIER layer wins.
	_, cp, a, _, _ := customFixture(t,
		`@layer a { #a { --color: #aaaaaa !important } }`+
			`@layer b { #a { --color: #bbbbbb !important } }`)
	if got := cp.EffectiveMap(a)["--color"]; got != "#aaaaaa" {
		t.Errorf("important: --color = %q, want #aaaaaa from the earlier layer", got)
	}
	// Layered !important beats an unlayered !important.
	_, cp, a, _, _ = customFixture(t,
		`#a { --color: #cccccc !important }`+
			`@layer b { #a { --color: #bbbbbb !important } }`)
	if got := cp.EffectiveMap(a)["--color"]; got != "#bbbbbb" {
		t.Errorf("important vs unlayered: --color = %q, want #bbbbbb", got)
	}
	// Within one layer, specificity and order keep deciding as before.
	_, cp, a, _, _ = customFixture(t,
		`@layer b { #a { --color: #bbbbbb } div { --color: #dddddd } }`)
	if got := cp.EffectiveMap(a)["--color"]; got != "#bbbbbb" {
		t.Errorf("same layer: --color = %q, want the id-specificity #bbbbbb", got)
	}
}

func TestCustomDeclarationsCarryTheirLayer(t *testing.T) {
	// The capture records the innermost @layer name and keeps the layer
	// name out of the @media condition (the replay wrappers must stay
	// parseable).
	doc := parseFakeHTML(`<div id="a"></div>`)
	cp := NewCustomProperties(doc, []string{
		`@media (min-width: 100px) { @layer theme { #a { --c: red } } }` +
			`@layer outer { @layer inner { #a { --d: blue } } }` +
			`@layer { #a { --e: teal } }`,
	})
	want := map[string]string{"--c": "theme", "--d": "outer.inner", "--e": "#1"}
	seen := map[string]string{}
	for _, d := range cp.Declarations() {
		seen[d.Property] = d.Layer
		if d.Property == "--c" && d.Media != "(min-width: 100px)" {
			t.Errorf("media polluted by layer name: %q", d.Media)
		}
	}
	for name, layer := range want {
		if seen[name] != layer {
			t.Errorf("%s: layer = %q, want %q", name, seen[name], layer)
		}
	}
}

// TestCodebergNavBackgroundFromLayeredLightTheme is the live-shaped pin:
// the theme-codeberg-auto.css pattern (two layers inside @media blocks,
// re-opened for dark) plus the unlayered index.css consumer. With correct
// layer order the later codeberg-theme layer wins over the forgejo-theme
// chain — including over the dark-mode forgejo block that comes LAST in
// document order and would otherwise win the tie.
func TestCodebergNavBackgroundFromLayeredLightTheme(t *testing.T) {
	html := "<style>" +
		// index.css (first sheet, unlayered, top level): the consumer.
		"#navbar { background: var(--color-nav-bg) }" +
		"</style>" +
		"<style>" +
		// theme-codeberg-auto.css, abridged to the nav-relevant shape.
		"@media(prefers-color-scheme:light){@layer forgejo-theme{" +
		"html{--zinc-100:#f4f4f5;--color-nav-bg:var(--zinc-100)}}}" +
		"@media(prefers-color-scheme:light){@layer codeberg-theme{" +
		"#navbar{--color-nav-bg:#2185d0}}}" +
		"@media(prefers-color-scheme:dark){@layer codeberg-theme{" +
		"#navbar{--color-nav-bg:#2185d0}}}" +
		// The interleaving that document order alone resolves wrongly:
		// an earlier layer redefining the property on the SAME element,
		// last in the sheet.
		"@media(prefers-color-scheme:dark){@layer forgejo-theme{" +
		"#navbar{--color-nav-bg:#10161d}}}" +
		"</style>" +
		"<nav id=\"navbar\"><div class=\"navbar-left ui secondary menu\">n</div></nav>"
	d, styles := doc(t, html, testViewport)
	nav := styles[mustSelect(t, d, "#navbar")]

	// Resolution level: the codeberg-theme layer wins.
	cp := NewCustomProperties(d, Stylesheets(d))
	if got := cp.EffectiveMap(int(mustSelect(t, d, "#navbar")))["--color-nav-bg"]; got != "#2185d0" {
		t.Errorf("--color-nav-bg = %q, want #2185d0 from the later layer", got)
	}
	// Application level: the nav paints #2185D0 (srgb(33,133,208)).
	if nav.Background == nil || !colorNear(*nav.Background, 33.0/255, 133.0/255, 208.0/255) {
		t.Errorf("nav background: got %+v, want srgb(33,133,208)", nav.Background)
	}
}
