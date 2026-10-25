package css

import "testing"

// Background decoration fidelity (meta1 S10). libcss 0.9.2 computes the
// background family only as far as CSS 2.1 reaches: it knows background-
// repeat and background-position as computed values (which the engine
// reads here for the first time), but it discards every value form that
// postdates it — #rrggbbaa colours and image functions like
// linear-gradient(...) alike — and it knows nothing about cascade layers
// or var(). The tests below pin (1) what libcss genuinely discards (the
// gap the manual passes fill), (2) the computed repeat/position mapping,
// (3) the manual gradient cascade over the author sheets, and (4) the
// var()-resolved background application.

// --- (0) the libcss gaps the manual passes exist because of -------------

func TestLibcssDiscardsUnsupportedBackgroundText(t *testing.T) {
	// #rrggbbaa (CSS Color 4) never reaches a computed style: the
	// declaration is dropped whole, exactly like any other unknown value.
	s := styleOf(t,
		"<style>.card { background-color:#144b49b2 }</style>"+
			"<div class=\"card\">x</div>",
		".card")
	if s.Background != nil {
		t.Errorf("hex8 colour: got %+v, want discarded (nil)", s.Background)
	}

	// A `background` shorthand whose image libcss cannot parse is dropped
	// whole too: the earlier background-color SURVIVES (no partial reset).
	s = styleOf(t,
		"<style>.card { background-color:red;"+
			"background:-webkit-linear-gradient(top,#144B49B2 40%,#144B49) }</style>"+
			"<div class=\"card\">x</div>",
		".card")
	if s.Background == nil || !colorNear(*s.Background, 1, 0, 0) {
		t.Errorf("vendor gradient shorthand: got %+v, want the red kept", s.Background)
	}

	// A background declaration containing var() is discarded by libcss
	// (the rewriter captured it before the sheet was ever parsed).
	s = styleOf(t,
		"<style>.c { background-color:var(--x) }</style><div class=\"c\">x</div>",
		".c")
	if s.Background != nil {
		t.Errorf("var background: got %+v, want nothing applied yet", s.Background)
	}
}

func TestLibcssBackgroundShorthandResetsImageSlots(t *testing.T) {
	// The reverse pin: a PARSEABLE `background` shorthand does expand to
	// every longhand (background-clip spec: omitted components take their
	// initial values). libcss handles this part of CSS 2.1 faithfully.
	s := styleOf(t,
		"<style>.c { background-color:red; background:url(t.png) no-repeat center bottom }</style>"+
			"<div class=\"c\">x</div>",
		".c")
	if s.Background != nil {
		t.Errorf("shorthand colour reset: got %+v, want transparent", s.Background)
	}
	if !s.HasBackgroundImage {
		t.Fatalf("shorthand image slot: url did not survive the cascade")
	}
}

// --- (2) computed background-repeat / background-position ----------------

func TestComputedBackgroundRepeatAndPosition(t *testing.T) {
	// The hero's exact homepage.css shape: no-repeat + the two keyword
	// pair. libcss computes keywords to percentages (CSS 2.1 §8.14.1
	// values; center=50%, bottom=100% — see css__cascade_background_
	// position in libcss' select/properties/background_position.c).
	s := styleOf(t,
		"<style>header { background-repeat:no-repeat; background-position:center bottom }</style>"+
			"<header>h</header>",
		"header")
	if s.BackgroundRepeat != BackgroundRepeatNoRepeat {
		t.Errorf("repeat: got %v, want no-repeat", s.BackgroundRepeat)
	}
	if !s.BackgroundPositionX.Percent || !near(s.BackgroundPositionX.Value, 50) {
		t.Errorf("position x: got %+v, want 50%%", s.BackgroundPositionX)
	}
	if !s.BackgroundPositionY.Percent || !near(s.BackgroundPositionY.Value, 100) {
		t.Errorf("position y: got %+v, want 100%%", s.BackgroundPositionY)
	}

	// Axis modes libcss maps from the CSS 2.1 keywords.
	s = styleOf(t, "<style>.x { background-repeat:repeat-x }</style><div class=\"x\"></div>", ".x")
	if s.BackgroundRepeat != BackgroundRepeatRepeatX {
		t.Errorf("repeat-x: got %v", s.BackgroundRepeat)
	}
	s = styleOf(t, "<style>.x { background-repeat:repeat-y }</style><div class=\"x\"></div>", ".x")
	if s.BackgroundRepeat != BackgroundRepeatRepeatY {
		t.Errorf("repeat-y: got %v", s.BackgroundRepeat)
	}

	// Length positions come back in px (CSS 2.1 §8.14.1 semantics).
	s = styleOf(t,
		"<style>.x { background-position:10px 20px }</style><div class=\"x\"></div>", ".x")
	if s.BackgroundPositionX.Percent || !near(s.BackgroundPositionX.Value, 10) {
		t.Errorf("position x px: got %+v, want 10px", s.BackgroundPositionX)
	}
	if s.BackgroundPositionY.Percent || !near(s.BackgroundPositionY.Value, 20) {
		t.Errorf("position y px: got %+v, want 20px", s.BackgroundPositionY)
	}

	// No declaration at all: the initial repeat/position leave the tiling
	// where the engine has always painted it.
	s = styleOf(t, "<style>.x { color:red }</style><div class=\"x\"></div>", ".x")
	if s.BackgroundRepeat != BackgroundRepeatRepeat {
		t.Errorf("initial repeat: got %v, want repeat", s.BackgroundRepeat)
	}
	if !near(s.BackgroundPositionX.Value, 0) || !near(s.BackgroundPositionY.Value, 0) {
		t.Errorf("initial position: got %+v %+v, want 0 0",
			s.BackgroundPositionX, s.BackgroundPositionY)
	}
}

// --- (3) the manual linear-gradient cascade ------------------------------

func tealNear(c Color, r, g, b, a float64) bool {
	const d = 0.01
	return c.R > r-d && c.R < r+d && c.G > g-d && c.G < g+d &&
		c.B > b-d && c.B < b+d && c.A > a-d && c.A < a+d
}

func TestLinearGradientBackgroundFromAuthorSheets(t *testing.T) {
	// The codeberg.org about-card: vendor legacy syntax with a stop
	// position (CSS Images 3 §3.4 formal syntax, prefixed legacy grammar).
	cases := []struct {
		name string
		css  string
		// want nil expects no gradient painted (today's behavior).
		want *LinearGradient
	}{
		{
			"codeberg card: -webkit-linear-gradient(top, hex8 40%, hex)",
			".card{background-color:#144b49b2;background:-webkit-linear-gradient(top,#144B49B2 40%,#144B49)}",
			&LinearGradient{
				From: Color{R: 20.0 / 255, G: 75.0 / 255, B: 73.0 / 255, A: 178.0 / 255},
				To:   Color{R: 20.0 / 255, G: 75.0 / 255, B: 73.0 / 255, A: 1},
			},
		},
		{
			"codeberg last vendor wins: -o- after -moz-",
			".card{background:-moz-linear-gradient(top,#111111,#222222);" +
				"background:-webkit-linear-gradient(top,#333333,#444444);" +
				"background:-o-linear-gradient(top,#144B49B2 40%,#144B49)}",
			&LinearGradient{
				From: Color{R: 20.0 / 255, G: 75.0 / 255, B: 73.0 / 255, A: 178.0 / 255},
				To:   Color{R: 20.0 / 255, G: 75.0 / 255, B: 73.0 / 255, A: 1},
			},
		},
		{
			"standard two-stop form",
			".card{background-image:linear-gradient(#144B49B2, #144B49)}",
			&LinearGradient{
				From: Color{R: 20.0 / 255, G: 75.0 / 255, B: 73.0 / 255, A: 178.0 / 255},
				To:   Color{R: 20.0 / 255, G: 75.0 / 255, B: 73.0 / 255, A: 1},
			},
		},
		{
			"explicit to bottom",
			".card{background-image:linear-gradient(to bottom, #111111, #222222)}",
			&LinearGradient{
				From: Color{R: 17.0 / 255, G: 17.0 / 255, B: 17.0 / 255, A: 1},
				To:   Color{R: 34.0 / 255, G: 34.0 / 255, B: 34.0 / 255, A: 1},
			},
		},
		{
			"to top reverses the stops",
			".card{background-image:linear-gradient(to top, #111111, #222222)}",
			&LinearGradient{
				From: Color{R: 34.0 / 255, G: 34.0 / 255, B: 34.0 / 255, A: 1},
				To:   Color{R: 17.0 / 255, G: 17.0 / 255, B: 17.0 / 255, A: 1},
			},
		},
		{
			// Other shapes keep today's behavior: nothing paints.
			"diagonal is out of the two-stop vertical subset",
			".card{background:linear-gradient(to top right,#111111,#222222)}",
			nil,
		},
		{
			"left/right legacy direction keeps today's behavior",
			".card{background:-webkit-linear-gradient(left, #111111, #222222)}",
			nil,
		},
		{
			"a later url declaration clears the gradient slot",
			".card{background-image:linear-gradient(#111111,#222222);background-image:url(t.png)}",
			nil,
		},
		{
			"a later plain background shorthand clears the gradient slot",
			".card{background-image:linear-gradient(#111111,#222222);background:#fff}",
			nil,
		},
		{
			"no background image at all",
			".card{color:red}",
			nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := styleOf(t, "<style>"+tc.css+"</style><div class=\"card\">x</div>", ".card")
			if tc.want == nil {
				if s.BackgroundGradient != nil {
					t.Errorf("got %+v, want no gradient", *s.BackgroundGradient)
				}
				return
			}
			if s.BackgroundGradient == nil {
				t.Fatalf("got no gradient, want %+v", *tc.want)
			}
			if !tealNear(s.BackgroundGradient.From, tc.want.From.R, tc.want.From.G, tc.want.From.B, tc.want.From.A) ||
				!tealNear(s.BackgroundGradient.To, tc.want.To.R, tc.want.To.G, tc.want.To.B, tc.want.To.A) {
				t.Errorf("gradient = %+v, want %+v", *s.BackgroundGradient, *tc.want)
			}
		})
	}
}

func TestLinearGradientCascadeSpecificityAndMedia(t *testing.T) {
	// A higher-specificity layered-free rule wins over the class rule.
	s := styleOf(t,
		"<style>.card{background-image:linear-gradient(#111111,#222222)}"+
			"div.card{background-image:linear-gradient(#333333,#444444)}</style>"+
			"<div class=\"card\">x</div>",
		".card")
	if s.BackgroundGradient == nil || !tealNear(s.BackgroundGradient.From, 51.0/255, 51.0/255, 51.0/255, 1) {
		t.Fatalf("specificity: got %+v, want the #333 gradient", s.BackgroundGradient)
	}
	// The viewport gate of the manual cascades: a max-width query that
	// does not match never reaches the slot (at 800px it DOES match — the
	// 500px query below never applies, so the gradient survives).
	s = styleOf(t,
		"<style>.card{background-image:linear-gradient(#111111,#222222)}"+
			"@media screen and (max-width:500px){.card{background-image:none}}</style>"+
			"<div class=\"card\">x</div>",
		".card")
	if s.BackgroundGradient == nil {
		t.Fatalf("non-matching media query cleared the gradient")
	}
	// A matching query wins: background-image:none clears the slot.
	s = styleOf(t,
		"<style>.card{background-image:linear-gradient(#111111,#222222)}"+
			"@media screen and (max-width:801px){.card{background-image:none}}</style>"+
			"<div class=\"card\">x</div>",
		".card")
	if s.BackgroundGradient != nil {
		t.Errorf("matching media none: got %+v, want cleared", *s.BackgroundGradient)
	}
	// style="" wins over the sheet, like every other manual cascade.
	s = styleOf(t,
		"<style>.card{background-image:linear-gradient(#111111,#222222)}</style>"+
			"<div class=\"card\" style=\"background-image:linear-gradient(#333333,#444444)\">x</div>",
		".card")
	if s.BackgroundGradient == nil || !tealNear(s.BackgroundGradient.From, 51.0/255, 51.0/255, 51.0/255, 1) {
		t.Fatalf("inline: got %+v, want the inline gradient", s.BackgroundGradient)
	}
}

// --- (4) var()-resolved background application ---------------------------

func TestVarBackgroundColorApplies(t *testing.T) {
	// The codeberg nav shape: the shorthand resolves to a plain colour.
	s := styleOf(t,
		"<style>html { --color-nav-bg: #2185d0 } #navbar { background: var(--color-nav-bg) }</style>"+
			"<nav id=\"navbar\">n</nav>",
		"#navbar")
	if s.Background == nil || !colorNear(*s.Background, 33.0/255, 133.0/255, 208.0/255) {
		t.Errorf("nav background: got %+v, want #2185d0", s.Background)
	}

	// The longhand form applies too, and an unresolvable var() leaves the
	// element untouched (invalid at computed-value time).
	s = styleOf(t,
		"<style>html { --c: red } .x { background-color: var(--c) } .y { background-color: var(--missing) }</style>"+
			"<div class=\"x\">a</div><div class=\"y\">b</div>",
		".x")
	if s.Background == nil || !colorNear(*s.Background, 1, 0, 0) {
		t.Errorf("background-colour var: got %+v, want red", s.Background)
	}
	s = styleOf(t,
		"<style>html { --c: red } .y { background-color: var(--missing) }</style>"+
			"<div class=\"y\">b</div>",
		".y")
	if s.Background != nil {
		t.Errorf("unresolvable var: got %+v, want untouched", s.Background)
	}

	// A resolved value the engine cannot interpret keeps today's behavior:
	// the declaration is dropped, NOT applied as some default.
	s = styleOf(t,
		"<style>html { --c: rgb(0 100% 0 / 50%) } .x { background: var(--c) }</style>"+
			"<div class=\"x\">a</div>",
		".x")
	if s.Background != nil {
		t.Errorf("unsupported colour syntax: got %+v, want inert", s.Background)
	}

	// The captured @media condition is honoured: a query that does not
	// match the viewport never applies.
	s = styleOf(t,
		"<style>html { --c: red } @media screen and (max-width:500px) { .x { background: var(--c) } }</style>"+
			"<div class=\"x\">a</div>",
		".x")
	if s.Background != nil {
		t.Errorf("non-matching media var: got %+v, want inert", s.Background)
	}

	// A var()-resolved supported gradient lands in the gradient slot.
	s = styleOf(t,
		"<style>html { --g: linear-gradient(#144B49B2, #144B49) } .x { background-image: var(--g) }</style>"+
			"<div class=\"x\">a</div>",
		".x")
	if s.BackgroundGradient == nil {
		t.Fatalf("var gradient did not apply")
	}
	if !tealNear(s.BackgroundGradient.To, 20.0/255, 75.0/255, 73.0/255, 1) {
		t.Errorf("var gradient: got %+v", *s.BackgroundGradient)
	}
}

func TestVarBackgroundShorthandClearsImageSlots(t *testing.T) {
	// Per CSS the background shorthand resets every longhand: the winning
	// resolved colour clears the image slots the libcss cascade computed.
	s := styleOf(t,
		"<style>html { --c: red } .x { background-image: url(t.png); background: var(--c) }</style>"+
			"<div class=\"x\">a</div>",
		".x")
	if s.Background == nil || !colorNear(*s.Background, 1, 0, 0) {
		t.Errorf("shorthand colour: got %+v, want red", s.Background)
	}
	if s.HasBackgroundImage {
		t.Errorf("shorthand kept the image slot: %+v", s.BackgroundImage)
	}
}
