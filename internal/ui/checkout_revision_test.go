package ui

import (
	"testing"

	"svn-tui/internal/model"
)

func testCheckoutRevisions() []model.CheckoutRevision {
	return []model.CheckoutRevision{
		{Revision: 1210, Author: "kata", Date: "2026-02-03 10:00", Msg: "Fix login redirect", Paths: []model.CheckoutPath{
			{Action: "M", Path: "/trunk/inc/auth.php"},
		}},
		{Revision: 1209, Author: "bence", Date: "2026-02-02 09:30", Msg: "Add invoice export", Paths: []model.CheckoutPath{
			{Action: "A", Path: "/trunk/inc/invoice.php"},
			{Action: "M", Path: "/trunk/action.php"},
		}},
		{Revision: 1208, Author: "bence", Date: "2026-02-01 08:15", Msg: "Bump version", Paths: []model.CheckoutPath{
			{Action: "M", Path: "/trunk/version.txt"},
		}},
	}
}

func TestFilterCheckoutRevisions(t *testing.T) {
	revisions := testCheckoutRevisions()

	tests := []struct {
		name  string
		query string
		want  []int
	}{
		{"empty query keeps everything", "  ", []int{1210, 1209, 1208}},
		{"revision number", "1209", []int{1209}},
		{"revision number with r prefix", "r1210", []int{1210}},
		{"commit message", "invoice", []int{1209}},
		{"changed path", "auth.php", []int{1210}},
		{"author", "bence", []int{1209, 1208}},
		{"terms are combined", "bence version", []int{1208}},
		{"no match", "nothing here", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterCheckoutRevisions(revisions, tt.query)
			if len(got) != len(tt.want) {
				t.Fatalf("filterCheckoutRevisions(%q) returned %d rows, want %d", tt.query, len(got), len(tt.want))
			}
			for i, rev := range got {
				if rev.Revision != tt.want[i] {
					t.Errorf("row %d is r%d, want r%d", i, rev.Revision, tt.want[i])
				}
			}
		})
	}
}

func TestCheckoutRevisionMatchedPath(t *testing.T) {
	revisions := testCheckoutRevisions()

	if got := checkoutRevisionMatchedPath(revisions[1], "invoice.php"); got != "/trunk/inc/invoice.php" {
		t.Errorf("path match = %q, want /trunk/inc/invoice.php", got)
	}
	// The message already explains the hit, so no path is worth showing.
	if got := checkoutRevisionMatchedPath(revisions[1], "invoice"); got != "" {
		t.Errorf("message match returned path %q, want empty", got)
	}
}
