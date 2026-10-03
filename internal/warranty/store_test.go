package warranty

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func seed(t *testing.T, s *Store) {
	t.Helper()
	if err := s.RegisterModel("M1", 365, []string{"screen", "battery"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterDevice("D1", "M1", "2026-01-01"); err != nil {
		t.Fatal(err)
	}
}

func TestWarrantyBoundary(t *testing.T) {
	s := newStore(t)
	seed(t, s)
	cases := []struct {
		date     string
		eligible bool
	}{
		{"2025-12-31", false}, // before purchase
		{"2026-01-01", true},  // purchase day
		{"2026-12-31", true},  // last covered day
		{"2027-01-01", false}, // expiry day: not covered
		{"2027-06-01", false},
	}
	for _, c := range cases {
		ok, reasons := s.CheckEligibility("D1", "screen", c.date)
		if ok != c.eligible {
			t.Errorf("date %s: eligible=%v, want %v (reasons %v)", c.date, ok, c.eligible, reasons)
		}
	}
}

func TestEligibilityReasons(t *testing.T) {
	s := newStore(t)
	seed(t, s)
	ok, reasons := s.CheckEligibility("D1", "camera", "2025-01-01")
	if ok || len(reasons) != 2 {
		t.Fatalf("want 2 reasons, got ok=%v reasons=%v", ok, reasons)
	}
	ok, reasons = s.CheckEligibility("NOPE", "screen", "2026-01-01")
	if ok || len(reasons) != 1 {
		t.Fatalf("unknown device: ok=%v reasons=%v", ok, reasons)
	}
	if _, err := s.RequestRepair("D1", "camera", "2026-02-01"); err == nil {
		t.Fatal("uncovered category request should fail")
	}
}

func TestOneOpenTicketPerDevice(t *testing.T) {
	s := newStore(t)
	seed(t, s)
	if _, err := s.RequestRepair("D1", "screen", "2026-02-01"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestRepair("D1", "battery", "2026-02-02"); err == nil {
		t.Fatal("second open ticket should be rejected")
	}
	if err := s.Cancel("T000001"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestRepair("D1", "battery", "2026-02-02"); err != nil {
		t.Fatalf("after cancel a new ticket should be allowed: %v", err)
	}
}

func TestApproveAtomicity(t *testing.T) {
	s := newStore(t)
	seed(t, s)
	if err := s.StockPart("screen", 2); err != nil {
		t.Fatal(err)
	}
	if err := s.StockPart("battery", 1); err != nil {
		t.Fatal(err)
	}
	tk, err := s.RequestRepair("D1", "screen", "2026-02-01")
	if err != nil {
		t.Fatal(err)
	}
	err = s.Approve(tk.ID, []PartReservation{
		{Part: "screen", Quantity: 2},
		{Part: "battery", Quantity: 5},
	})
	if err == nil {
		t.Fatal("approve should fail on insufficient battery stock")
	}
	avail, reserved, _, _ := s.PartInfo("screen")
	if avail != 2 || reserved != 0 {
		t.Fatalf("failed approve must not reserve: avail=%d reserved=%d", avail, reserved)
	}
	if s.Tickets[tk.ID].Status != StatusPending {
		t.Fatalf("ticket should stay pending, got %s", s.Tickets[tk.ID].Status)
	}
}

func TestApproveValidation(t *testing.T) {
	s := newStore(t)
	seed(t, s)
	tk, _ := s.RequestRepair("D1", "screen", "2026-02-01")
	if err := s.Approve(tk.ID, nil); err != nil {
		t.Fatalf("empty parts list should be allowed: %v", err)
	}
	if err := s.Approve(tk.ID, nil); err == nil {
		t.Fatal("re-approving an approved ticket should fail")
	}
	tk2, _ := s.RequestRepair("D1", "battery", "2026-02-02")
	_ = tk2
}

func TestApproveDuplicatePart(t *testing.T) {
	s := newStore(t)
	seed(t, s)
	s.StockPart("screen", 10)
	tk, _ := s.RequestRepair("D1", "screen", "2026-02-01")
	err := s.Approve(tk.ID, []PartReservation{
		{Part: "screen", Quantity: 1},
		{Part: "screen", Quantity: 2},
	})
	if err == nil {
		t.Fatal("duplicate part should be rejected")
	}
}

func TestCompleteConsumesAndCancelReleases(t *testing.T) {
	s := newStore(t)
	seed(t, s)
	s.StockPart("screen", 4)
	tk, _ := s.RequestRepair("D1", "screen", "2026-02-01")
	if err := s.Approve(tk.ID, []PartReservation{{Part: "screen", Quantity: 3}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(tk.ID); err != nil {
		t.Fatal(err)
	}
	avail, reserved, consumed, _ := s.PartInfo("screen")
	if avail != 1 || reserved != 0 || consumed != 3 {
		t.Fatalf("after complete: avail=%d reserved=%d consumed=%d", avail, reserved, consumed)
	}
	if err := s.Complete(tk.ID); err == nil {
		t.Fatal("double complete should fail")
	}
	if err := s.Cancel(tk.ID); err == nil {
		t.Fatal("cancel after complete should fail")
	}
	// Stocking more must not rewrite usage counters.
	s.StockPart("screen", 10)
	avail, reserved, consumed, _ = s.PartInfo("screen")
	if avail != 11 || reserved != 0 || consumed != 3 {
		t.Fatalf("restock: avail=%d reserved=%d consumed=%d", avail, reserved, consumed)
	}
}

func TestCancelApprovedReleasesParts(t *testing.T) {
	s := newStore(t)
	seed(t, s)
	s.StockPart("screen", 4)
	tk, _ := s.RequestRepair("D1", "screen", "2026-02-01")
	s.Approve(tk.ID, []PartReservation{{Part: "screen", Quantity: 3}})
	if err := s.Cancel(tk.ID); err != nil {
		t.Fatal(err)
	}
	avail, reserved, consumed, _ := s.PartInfo("screen")
	if avail != 4 || reserved != 0 || consumed != 0 {
		t.Fatalf("after cancel: avail=%d reserved=%d consumed=%d", avail, reserved, consumed)
	}
	if err := s.Cancel(tk.ID); err == nil {
		t.Fatal("double cancel should fail")
	}
}

func TestPersistenceAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	seed(t, s)
	s.StockPart("screen", 4)
	tk, _ := s.RequestRepair("D1", "screen", "2026-02-01")
	s.Approve(tk.ID, []PartReservation{{Part: "screen", Quantity: 3}})
	s.Cancel(tk.ID)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	s2, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	avail, reserved, consumed, err := s2.PartInfo("screen")
	if err != nil || avail != 4 || reserved != 0 || consumed != 0 {
		t.Fatalf("after restart: avail=%d reserved=%d consumed=%d err=%v", avail, reserved, consumed, err)
	}
	got := s2.Tickets[tk.ID]
	if got == nil || got.Status != StatusCancelled || len(got.History) != 3 {
		t.Fatalf("ticket not restored: %+v", got)
	}
	// Ticket IDs keep increasing after restart.
	tk2, err := s2.RequestRepair("D1", "battery", "2026-03-01")
	if err != nil {
		t.Fatal(err)
	}
	if tk2.ID == tk.ID {
		t.Fatal("ticket ID reused after restart")
	}
	if _, err := Load(filepath.Join(dir, "state.json")); err == nil {
		// Loading a file path as dir should fail or be empty; just ensure no panic.
		t.Log("note: loading file path as dir did not error")
	}
}

func TestHandleLineErrorsAndContinues(t *testing.T) {
	s := newStore(t)
	resp := s.HandleLine([]byte(`not json`))
	var r map[string]any
	if err := json.Unmarshal(resp, &r); err != nil {
		t.Fatal(err)
	}
	if r["ok"] != false {
		t.Fatalf("invalid JSON should fail: %s", resp)
	}
	resp = s.HandleLine([]byte(`{"op":"bogus"}`))
	json.Unmarshal(resp, &r)
	if r["ok"] != false {
		t.Fatalf("unknown op should fail: %s", resp)
	}
	resp = s.HandleLine([]byte(`{"op":"register_model","model":"M","warranty_days":10,"covered_categories":["x"]}`))
	json.Unmarshal(resp, &r)
	if r["ok"] != true {
		t.Fatalf("valid request after errors should succeed: %s", resp)
	}
}

func TestStockValidation(t *testing.T) {
	s := newStore(t)
	if err := s.StockPart("p", 0); err == nil {
		t.Fatal("zero quantity should fail")
	}
	if err := s.StockPart("p", -3); err == nil {
		t.Fatal("negative quantity should fail")
	}
	if err := s.RegisterModel("bad", 0, []string{"x"}); err == nil {
		t.Fatal("zero warranty days should fail")
	}
	if err := s.RegisterDevice("D", "missing", "2026-01-01"); err == nil {
		t.Fatal("unknown model should fail")
	}
	if err := s.RegisterModel("M", 1, []string{"x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterDevice("D", "M", "2026-13-01"); err == nil {
		t.Fatal("invalid date should fail")
	}
}
