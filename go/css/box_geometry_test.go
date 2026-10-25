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

func TestAbsolutePositionAndInsetsAreMappedFromLibcss(t *testing.T) {
	s := styleOf(t,
		`<style>.box{position:absolute;top:-3em;left:-2em;right:10%;bottom:1rem;font-size:20px}</style>`+
			`<div class="box">x</div>`, ".box")
	if !s.PositionAbsolute {
		t.Fatal("computed position is not absolute")
	}
	if s.AbsoluteTop == nil || !near(s.AbsoluteTop.Value, -60) || s.AbsoluteTop.Percent {
		t.Errorf("computed top = %+v, want -60px from -3em at 20px", s.AbsoluteTop)
	}
	if s.AbsoluteLeft == nil || !near(s.AbsoluteLeft.Value, -40) || s.AbsoluteLeft.Percent {
		t.Errorf("computed left = %+v, want -40px from -2em at 20px", s.AbsoluteLeft)
	}
	if s.AbsoluteRight == nil || !near(s.AbsoluteRight.Value, 10) || !s.AbsoluteRight.Percent {
		t.Errorf("computed right = %+v, want 10%%", s.AbsoluteRight)
	}
	if s.AbsoluteBottom == nil || !near(s.AbsoluteBottom.Value, 16) || s.AbsoluteBottom.Percent {
		t.Errorf("computed bottom = %+v, want 16px from 1rem", s.AbsoluteBottom)
	}
}
