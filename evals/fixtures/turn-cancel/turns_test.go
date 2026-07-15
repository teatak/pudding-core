package turncancel

import "testing"

func TestCancelOnlyChangesRunningTurn(t *testing.T) {
	turns := map[string]Status{"run": Running, "done": Completed, "failed": Failed}
	if !Cancel(turns, "run") || turns["run"] != Cancelled {
		t.Fatalf("running turn = %q", turns["run"])
	}
	if !Cancel(turns, "done") || turns["done"] != Completed {
		t.Fatalf("completed turn changed: %q", turns["done"])
	}
	if !Cancel(turns, "failed") || turns["failed"] != Failed {
		t.Fatalf("failed turn changed: %q", turns["failed"])
	}
	if Cancel(turns, "missing") {
		t.Fatal("missing turn should return false")
	}
}
