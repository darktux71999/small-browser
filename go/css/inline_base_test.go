package css

import (
	"testing"
)

// Inline style="" declarations belong to the document, so a relative url()
// in them resolves against the document base URL, the same base a
// stylesheet's own references resolve against (CSS 2.1 §7.2 relative URIs:
// the base is the document). Before this rule the inline sheet was named
// browser-lite://inline/N, so every relative reference failed.
func TestInlineStyleURLResolvesAgainstDocumentBase(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{
			name: "http document base keeps its directory",
			base: "https://example.com/dir/page.html",
			want: "https://example.com/dir/bg.png",
		},
		{
			name: "directory base with trailing slash",
			base: "https://example.com/dir/",
			want: "https://example.com/dir/bg.png",
		},
		{
			name: "dot segments in the document path normalise",
			base: "https://example.com/a/b/../page.html",
			want: "https://example.com/a/bg.png",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := parseFakeHTML(`<div id="d" style="background-image: url(bg.png)">x</div>`)
			styles, _, err := ComputeDocument(d, nil, testViewport, tt.base)
			if err != nil {
				t.Fatalf("ComputeDocument: %v", err)
			}
			s := styles[mustSelect(t, d, "#d")]
			if !s.HasBackgroundImage {
				t.Fatalf("inline background-image was dropped; want %q", tt.want)
			}
			if s.BackgroundImage != tt.want {
				t.Fatalf("BackgroundImage = %q, want %q", s.BackgroundImage, tt.want)
			}
		})
	}
}
