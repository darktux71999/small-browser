package css

import "testing"

// Computed-value evidence for the about-card investigation: the declaration
// must survive the rewrite and libcss must expose it before layout applies it.
func TestMaxWidthAndRelativePositionAreMappedFromLibcss(t *testing.T) {
	s := styleOf(t,
		`<style>.card{box-sizing:border-box;max-width:950px;margin-left:auto;margin-right:auto;position:relative;top:-7em;left:2em;font-size:20px}</style>`+
			`<div class="card">card</div>`, ".card")
	if s.MaxWidth == nil || !near(*s.MaxWidth, 950) {
		t.Errorf("computed max-width = %v, want 950px", s.MaxWidth)
	}
	if !s.BorderBox {
		t.Error("computed box-sizing is not border-box")
	}
	if !s.MarginAuto[1] || !s.MarginAuto[3] {
		t.Errorf("auto inline margins = %+v, want right and left auto", s.MarginAuto)
	}
	if !s.PositionRelative {
		t.Error("computed position is not relative")
	}
	if s.RelativeTop == nil || !near(*s.RelativeTop, -140) {
		t.Errorf("computed top = %v, want -140px from -7em at 20px", s.RelativeTop)
	}
	if s.RelativeLeft == nil || !near(*s.RelativeLeft, 40) {
		t.Errorf("computed left = %v, want 40px from 2em at 20px", s.RelativeLeft)
	}
}

func TestStaticPositionKeepsRelativeInsetsInactive(t *testing.T) {
	s := styleOf(t,
		`<style>.box{position:static;top:-7em;left:2em;font-size:20px}</style><div class="box">x</div>`,
		".box")
	if s.PositionRelative {
		t.Error("static position mapped as relative")
	}
	if s.RelativeTop != nil || s.RelativeLeft != nil {
		t.Errorf("static insets should not be mapped as relative offsets: top=%v left=%v", s.RelativeTop, s.RelativeLeft)
	}
}
