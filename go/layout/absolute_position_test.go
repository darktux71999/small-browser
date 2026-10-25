package layout

import "testing"

// CSS 2.1 §§9.3.1, 10.1, 10.3.7, and 10.6.4: absolute boxes leave their
// formatting context and use the nearest positioned ancestor's padding box.
func TestAbsolutePillStretchesAcrossPositionedFlexCard(t *testing.T) {
	html := `<style>html,body{margin:0} #card{display:flex;position:relative;width:300px;font-size:16px}` +
		`#card>span{display:block;flex:0 0 100px;height:20px} #pill{display:block;position:absolute;` +
		`top:-3em;left:-2em;right:-2em;white-space:nowrap}</style>` +
		`<div id="card"><span id="one">1</span><span id="two">2</span><span id="three">3</span>` +
		`<div id="pill">Register your account</div></div>`
	doc, _, page := cascade(t, html)
	card := boxFor(t, page, doc, "#card")
	pill := boxFor(t, page, doc, "#pill")
	if pill.X != card.X-32 || pill.W != card.W+64 {
		t.Errorf("pill geometry = x:%v w:%v, want card x:%v - 32 and width %v + 64", pill.X, pill.W, card.X, card.W)
	}
	if pill.Y != card.Y-48 {
		t.Errorf("pill y = %v, want card y %v - 3em (48px)", pill.Y, card.Y)
	}
	if got := len(textIn(page, pill)); got != 1 {
		t.Errorf("pill text lines = %d, want one line", got)
	}
	for _, id := range []string{"#one", "#two", "#three"} {
		boxFor(t, page, doc, id)
	}
	if card.H != 20 {
		t.Errorf("card height = %v, want flex row height 20px (abspos pill excluded)", card.H)
	}
}

func TestAbsoluteAllAutoInsetsUseStaticFlowPosition(t *testing.T) {
	html := `<style>html,body{margin:0} #parent{position:relative;width:300px} .item{display:block;height:20px}` +
		`#absolute{position:absolute;width:50px;height:10px}</style>` +
		`<div id="parent"><div id="before" class="item">before</div>` +
		`<div id="absolute">absolute</div><div id="after" class="item">after</div></div>`
	doc, _, page := cascade(t, html)
	parent := boxFor(t, page, doc, "#parent")
	before := boxFor(t, page, doc, "#before")
	absolute := boxFor(t, page, doc, "#absolute")
	after := boxFor(t, page, doc, "#after")
	if absolute.X != before.X || absolute.Y != before.Y+before.H {
		t.Errorf("all-auto box at (%v,%v), want static flow point (%v,%v)", absolute.X, absolute.Y, before.X, before.Y+before.H)
	}
	if after.Y != before.Y+before.H {
		t.Errorf("following sibling y = %v, want %v (absolute box contributes no flow height)", after.Y, before.Y+before.H)
	}
	if parent.H != before.H+after.H {
		t.Errorf("parent height = %v, want in-flow children only: %v", parent.H, before.H+after.H)
	}
}

func TestAbsoluteOverConstrainedLTRUsesLeftInset(t *testing.T) {
	html := `<style>html,body{margin:0} #parent{position:relative;width:300px} #absolute{position:absolute;` +
		`left:10px;right:20px;width:100px;height:10px;top:5px}</style>` +
		`<div id="parent"><div id="absolute">x</div></div>`
	doc, _, page := cascade(t, html)
	parent := boxFor(t, page, doc, "#parent")
	absolute := boxFor(t, page, doc, "#absolute")
	if absolute.X != parent.X+10 || absolute.W != 100 {
		t.Errorf("LTR over-constrained geometry = x:%v w:%v, want left edge %v and fixed width 100", absolute.X, absolute.W, parent.X+10)
	}
}

func TestAbsoluteSingleInsetDoesNotStretchAutoOpposite(t *testing.T) {
	t.Run("top only", func(t *testing.T) {
		html := `<style>html,body{margin:0} #parent{position:relative;width:300px} #before{display:block;height:20px}` +
			`#absolute{position:absolute;top:15px;width:60px;height:10px}</style>` +
			`<div id="parent"><div id="before">before</div><div id="absolute">x</div></div>`
		doc, _, page := cascade(t, html)
		before := boxFor(t, page, doc, "#before")
		absolute := boxFor(t, page, doc, "#absolute")
		if absolute.X != before.X || absolute.Y != 15 || absolute.W != 60 {
			t.Errorf("top-only geometry = (%v,%v) %v wide, want static x %v, y 15, width 60", absolute.X, absolute.Y, absolute.W, before.X)
		}
	})
	t.Run("left only", func(t *testing.T) {
		html := `<style>html,body{margin:0} #parent{position:relative;width:300px} #before{display:block;height:20px}` +
			`#absolute{position:absolute;left:25px;width:60px;height:10px}</style>` +
			`<div id="parent"><div id="before">before</div><div id="absolute">x</div></div>`
		doc, _, page := cascade(t, html)
		parent := boxFor(t, page, doc, "#parent")
		before := boxFor(t, page, doc, "#before")
		absolute := boxFor(t, page, doc, "#absolute")
		if absolute.X != parent.X+25 || absolute.Y != before.Y+before.H || absolute.W != 60 {
			t.Errorf("left-only geometry = (%v,%v) %v wide, want x %v, static y %v, width 60", absolute.X, absolute.Y, absolute.W, parent.X+25, before.Y+before.H)
		}
	})
}

func TestAbsoluteUsesNearestPositionedAncestor(t *testing.T) {
	html := `<style>html,body{margin:0} #outer{position:relative;left:30px;width:400px} ` +
		`#inner{position:relative;left:40px;margin-left:50px;width:200px;height:100px} ` +
		`#absolute{position:absolute;left:10px;top:5px;width:30px;height:10px}</style>` +
		`<div id="outer"><div id="inner"><div id="absolute">x</div></div></div>`
	doc, _, page := cascade(t, html)
	inner := boxFor(t, page, doc, "#inner")
	absolute := boxFor(t, page, doc, "#absolute")
	if absolute.X != inner.X+10 || absolute.Y != inner.Y+5 {
		t.Errorf("absolute origin = (%v,%v), want nearest positioned ancestor origin (%v,%v) + (10,5)", absolute.X, absolute.Y, inner.X, inner.Y)
	}
}

func TestAbsoluteGridChildDoesNotOccupyGridCell(t *testing.T) {
	html := `<style>html,body{margin:0} #grid{display:grid;position:relative;width:200px;` +
		`grid-template-columns:100px 100px} #grid>div{height:20px} #absolute{position:absolute;` +
		`left:0;top:0;width:10px;height:10px}</style>` +
		`<div id="grid"><div id="first">one</div><div id="absolute">out</div><div id="second">two</div></div>`
	doc, _, page := cascade(t, html)
	grid := boxFor(t, page, doc, "#grid")
	first := boxFor(t, page, doc, "#first")
	second := boxFor(t, page, doc, "#second")
	if first.Y != second.Y || second.X != first.X+100 {
		t.Errorf("grid item positions first=(%v,%v), second=(%v,%v); abspos child consumed a cell", first.X, first.Y, second.X, second.Y)
	}
	if grid.H != 20 {
		t.Errorf("grid height = %v, want one in-flow row at 20px", grid.H)
	}
}

func TestAbsolutePercentageInsetsResolveAgainstContainingBlock(t *testing.T) {
	html := `<style>html,body{margin:0} #parent{position:relative;width:200px;height:100px} ` +
		`#absolute{position:absolute;left:25%;top:10%;width:20px;height:10px}</style>` +
		`<div id="parent"><div id="absolute">x</div></div>`
	doc, _, page := cascade(t, html)
	parent := boxFor(t, page, doc, "#parent")
	absolute := boxFor(t, page, doc, "#absolute")
	if absolute.X != parent.X+50 || absolute.Y != parent.Y+10 {
		t.Errorf("percentage inset origin = (%v,%v), want containing-block origin (%v,%v) + (50,10)", absolute.X, absolute.Y, parent.X, parent.Y)
	}
}

// PRESERVE: relative offsets still move only their own painted subtree; the
// sibling cursor remains normal-flow (CSS 2.1 §9.4.3).
func TestRelativePositionStillLeavesFollowingSiblingInFlow(t *testing.T) {
	html := `<style>html,body{margin:0} #target{position:relative;top:10px;height:20px} ` +
		`#next{height:20px}</style><div id="target">target</div><div id="next">next</div>`
	doc, _, page := cascade(t, html)
	target := boxFor(t, page, doc, "#target")
	next := boxFor(t, page, doc, "#next")
	if target.Y != 10 {
		t.Errorf("relative target y = %v, want 10px", target.Y)
	}
	if next.Y != 20 {
		t.Errorf("following sibling y = %v, want unshifted flow y 20px", next.Y)
	}
}
