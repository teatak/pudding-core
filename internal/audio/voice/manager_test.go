package voice

import "testing"

func TestManagerBindsAndReleasesSessionOwnership(t *testing.T) {
	m := NewManager()

	bindings, err := m.BindInput("sess_a", true)
	if err != nil {
		t.Fatal(err)
	}
	if bindings.InputOwner != "sess_a" || bindings.OutputOwner != "" {
		t.Fatalf("unexpected input binding: %+v", bindings)
	}

	bindings, err = m.BindOutput("sess_a", true)
	if err != nil {
		t.Fatal(err)
	}
	if bindings.OutputOwner != "sess_a" {
		t.Fatalf("unexpected output binding: %+v", bindings)
	}

	bindings, err = m.BindInput("sess_b", true)
	if err != nil {
		t.Fatal(err)
	}
	if bindings.InputOwner != "sess_b" || bindings.OutputOwner != "sess_a" {
		t.Fatalf("unexpected replacement binding: %+v", bindings)
	}

	bindings, err = m.BindInput("sess_a", false)
	if err != nil {
		t.Fatal(err)
	}
	if bindings.InputOwner != "sess_b" {
		t.Fatalf("non-owner should not clear input: %+v", bindings)
	}

	bindings = m.ReleaseSession("sess_a")
	if bindings.InputOwner != "sess_b" || bindings.OutputOwner != "" {
		t.Fatalf("release should clear only matching slots: %+v", bindings)
	}

	bindings = m.ReleaseSession("sess_b")
	if bindings.InputOwner != "" || bindings.OutputOwner != "" {
		t.Fatalf("release should clear final slot: %+v", bindings)
	}
}

func TestManagerRejectsEmptySessionID(t *testing.T) {
	m := NewManager()
	if _, err := m.BindInput(" ", true); err != ErrSessionRequired {
		t.Fatalf("BindInput err = %v, want %v", err, ErrSessionRequired)
	}
	if _, err := m.BindOutput("", true); err != ErrSessionRequired {
		t.Fatalf("BindOutput err = %v, want %v", err, ErrSessionRequired)
	}
}
