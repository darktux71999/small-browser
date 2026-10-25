package css

import "testing"

// Unit pins for the row-composition cascade readers (meta1-codeberg M3):
// the @media gate, minmax() tracks and place-self mapping. End-to-end
// geometry lives in go/layout/rowcompose_test.go; these pin the parsing
// edges the fixtures do not reach.
func TestMediaQueryMatchesViewport(t *testing.T) {
	cases := []struct {
		name    string
		prelude string
		width   float64
		height  float64
		want    bool
	}{
		{"max-width matches below", "@media (max-width: 739px)", 700, 600, true},
		{"max-width misses above", "@media (max-width: 739px)", 1000, 600, false},
		{"min-width matches above", "@media screen and (min-width: 740px)", 1000, 600, true},
		{"min-width misses below", "@media screen and (min-width: 740px)", 700, 600, false},
		{"and-chain needs both", "@media screen and (min-width: 740px) and (max-width: 991px)", 1000, 600, false},
		{"and-chain holds inside", "@media screen and (min-width: 740px) and (max-width: 991px)", 800, 600, true},
		{"fractional bound", "@media (max-width: 767.98px)", 767.98, 600, true},
		{"fractional bound above", "@media (max-width: 767.98px)", 768, 600, false},
		{"comma list matches either", "@media (max-width: 739px), (min-width: 992px)", 1000, 600, true},
		{"print never matches screen", "@media print", 1000, 600, false},
		{"unknown feature does not match", "@media (prefers-color-scheme: dark)", 1000, 600, false},
		{"negated unknown matches", "@media not (prefers-color-scheme: dark)", 1000, 600, true},
		{"max-height reads height", "@media (max-height: 575px)", 1000, 500, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mediaMatchesViewport(tc.prelude, tc.width, tc.height); got != tc.want {
				t.Errorf("mediaMatchesViewport(%q, %.0f, %.0f) = %v, want %v",
					tc.prelude, tc.width, tc.height, got, tc.want)
			}
		})
	}
}

func TestMinmaxTrackParsing(t *testing.T) {
	tracks := parseGridTracks("repeat(5,minmax(177px,240px))")
	if len(tracks) != 5 {
		t.Fatalf("repeat(5,minmax) expands to 5 tracks, got %+v", tracks)
	}
	for i, track := range tracks {
		if track.Kind != TrackMinMax || track.MinAuto || track.MaxAuto ||
			!near(track.Min, 177.0) || !near(track.Max, 240.0) {
			t.Fatalf("track %d clamps 177..240, got %+v", i, track)
		}
	}
	auto := parseGridTracks("minmax(auto,240px)")
	if len(auto) != 1 || auto[0].Kind != TrackMinMax || !auto[0].MinAuto || near(auto[0].Max, 0) {
		t.Fatalf("auto min stays open, got %+v", auto)
	}
	degraded := parseGridTracks("minmax(100px,1fr)")
	if len(degraded) != 1 || degraded[0].Kind != TrackAuto {
		t.Fatalf("fr extremum degrades to auto, got %+v", degraded)
	}
}

func TestPlaceSelfParsing(t *testing.T) {
	align, justify := parsePlaceSelf("center right")
	if align != GridSelfCenter || justify != GridSelfEnd {
		t.Fatalf("place-self:center right = (%v,%v)", align, justify)
	}
	single, singleJust := parsePlaceSelf("center")
	if single != GridSelfCenter || singleJust != GridSelfCenter {
		t.Fatalf("place-self:center sets both, got (%v,%v)", single, singleJust)
	}
	if got := parseSelfAlign("left"); got != GridSelfStart {
		t.Fatalf("left maps to start, got %v", got)
	}
	if got := parseSelfAlign("bogus"); got != GridSelfAuto {
		t.Fatalf("unknown degrades to auto, got %v", got)
	}
}
