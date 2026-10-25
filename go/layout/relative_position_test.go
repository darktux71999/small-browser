package layout

import "testing"

// CSS 2.1 §9.4.3: relative offsets move a box after normal flow placement;
// they must not change where later siblings are laid out.
func TestRelativeTopMovesBoxWithoutMovingNextSibling(t *testing.T) {
	base := `<style>html,body{margin:0} #target{font-size:20px} #next{font-size:20px}</style>` +
		`<div id="target">target</div><div id="next">next</div>`
	positioned := `<style>html,body{margin:0} #target{position:relative;top:-7em;font-size:20px} #next{font-size:20px}</style>` +
		`<div id="target">target</div><div id="next">next</div>`

	baseDoc, _, baseLayout := cascade(t, base)
	positionedDoc, _, positionedLayout := cascade(t, positioned)
	baseTarget := boxFor(t, baseLayout, baseDoc, "#target")
	positionedTarget := boxFor(t, positionedLayout, positionedDoc, "#target")
	baseNext := boxFor(t, baseLayout, baseDoc, "#next")
	positionedNext := boxFor(t, positionedLayout, positionedDoc, "#next")
	if positionedTarget.Y != baseTarget.Y-140 {
		t.Errorf("target y = %v, want normal y %v - 7em (140px)", positionedTarget.Y, baseTarget.Y)
	}
	if positionedNext.Y != baseNext.Y {
		t.Errorf("next sibling y = %v, want unchanged flow y %v", positionedNext.Y, baseNext.Y)
	}
}

func TestRelativeLeftMovesBoxWithoutAffectingFlow(t *testing.T) {
	base := `<style>html,body{margin:0} #target{font-size:10px} #next{font-size:10px}</style>` +
		`<div id="target">target</div><div id="next">next</div>`
	positioned := `<style>html,body{margin:0} #target{position:relative;left:2em;font-size:10px} #next{font-size:10px}</style>` +
		`<div id="target">target</div><div id="next">next</div>`

	baseDoc, _, baseLayout := cascade(t, base)
	positionedDoc, _, positionedLayout := cascade(t, positioned)
	baseTarget := boxFor(t, baseLayout, baseDoc, "#target")
	positionedTarget := boxFor(t, positionedLayout, positionedDoc, "#target")
	baseNext := boxFor(t, baseLayout, baseDoc, "#next")
	positionedNext := boxFor(t, positionedLayout, positionedDoc, "#next")
	if positionedTarget.X != baseTarget.X+20 {
		t.Errorf("target x = %v, want normal x %v + 2em (20px)", positionedTarget.X, baseTarget.X)
	}
	if positionedNext.Y != baseNext.Y || positionedNext.X != baseNext.X {
		t.Errorf("next sibling origin = (%v,%v), want unchanged flow origin (%v,%v)",
			positionedNext.X, positionedNext.Y, baseNext.X, baseNext.Y)
	}
}

func TestRelativeOpposingInsetsUseTopAndDirectionPrecedence(t *testing.T) {
	base := `<style>html,body{margin:0} #target{font-size:16px} #next{font-size:16px}</style>` +
		`<div id="target">target</div><div id="next">next</div>`
	for _, tc := range []struct {
		direction string
		wantX     float64
	}{
		{direction: "ltr", wantX: 8},
		{direction: "rtl", wantX: -3},
	} {
		t.Run(tc.direction, func(t *testing.T) {
			positioned := `<style>html,body{margin:0} #target{font-size:16px;position:relative;` +
				`direction:` + tc.direction + `;left:8px;right:3px;top:5px;bottom:2px} #next{font-size:16px}</style>` +
				`<div id="target">target</div><div id="next">next</div>`
			baseDoc, _, baseLayout := cascade(t, base)
			positionedDoc, _, positionedLayout := cascade(t, positioned)
			baseTarget := boxFor(t, baseLayout, baseDoc, "#target")
			positionedTarget := boxFor(t, positionedLayout, positionedDoc, "#target")
			baseNext := boxFor(t, baseLayout, baseDoc, "#next")
			positionedNext := boxFor(t, positionedLayout, positionedDoc, "#next")
			if positionedTarget.X != baseTarget.X+tc.wantX || positionedTarget.Y != baseTarget.Y+5 {
				t.Errorf("target origin = (%v,%v), want normal (%v,%v) + (%v,5) per opposing inset precedence",
					positionedTarget.X, positionedTarget.Y, baseTarget.X, baseTarget.Y, tc.wantX)
			}
			if positionedNext.X != baseNext.X || positionedNext.Y != baseNext.Y {
				t.Errorf("next sibling moved to (%v,%v), want normal flow origin (%v,%v)",
					positionedNext.X, positionedNext.Y, baseNext.X, baseNext.Y)
			}
		})
	}
}

// CSS 2.1 §10.4: max-width constrains the used content width; auto inline
// margins divide the remaining space and center the border box.
func TestMaxWidthWithAutoMarginsCentersBlock(t *testing.T) {
	html := `<style>html,body{margin:0} #card{max-width:950px;margin-left:auto;margin-right:auto}</style>` +
		`<div id="card">card</div>`
	doc, _, page := cascade(t, html)
	card := boxFor(t, page, doc, "#card")
	if card.W != 950 || card.X != 25 {
		t.Errorf("card geometry = x:%v w:%v, want centered x:25 w:950 in 1000px", card.X, card.W)
	}
}

func TestMaxWidthBorderBoxIncludesPaddingWhenCentered(t *testing.T) {
	// Codeberg applies box-sizing:border-box globally. The card's padding
	// therefore fits inside its 950px max-width rather than expanding it.
	html := `<style>*{box-sizing:border-box} html,body{margin:0}` +
		`#card{display:flex;position:relative;top:-7em;max-width:950px;` +
		`padding:2.5em 2em 2em;margin-left:auto;margin-right:auto}</style>` +
		`<div id="card"><div>card one</div><div>card two</div></div>`
	doc, _, page := cascade(t, html)
	card := boxFor(t, page, doc, "#card")
	if card.W != 950 || card.X != 25 {
		t.Errorf("border-box card geometry = x:%v w:%v, want centered x:25 w:950", card.X, card.W)
	}
}
