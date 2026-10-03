package warranty

import "testing"

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustOK(t *testing.T, resp Response) {
	t.Helper()
	if !resp.OK {
		t.Fatalf("expected ok, got error %q", resp.Error)
	}
}

func mustFail(t *testing.T, resp Response, want string) {
	t.Helper()
	if resp.OK {
		t.Fatalf("expected failure containing %q, got ok", want)
	}
	if want != "" && !containsStr(resp.Error, want) {
		t.Fatalf("error %q does not contain %q", resp.Error, want)
	}
}

func failReasons(resp Response) []string {
	if resp.Result == nil {
		return nil
	}
	r, ok := resp.Result.(map[string]interface{})
	if !ok || r["reasons"] == nil {
		return nil
	}
	out := []string{}
	for _, v := range r["reasons"].([]string) {
		out = append(out, v)
	}
	return out
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func seedBasics(t *testing.T, s *Store) {
	t.Helper()
	mustOK(t, s.Dispatch(Request{
		Op: "register_model", ModelID: "M1", WarrantyDays: 30,
		FaultCategories: []string{"screen", "battery"},
	}))
	mustOK(t, s.Dispatch(Request{
		Op: "register_device", DeviceID: "D1", ModelID: "M1", PurchaseDate: "2026-01-01",
	}))
	mustOK(t, s.Dispatch(Request{Op: "receive_part", PartID: "P1", Quantity: 5}))
}

func TestWarrantyBoundary(t *testing.T) {
	s := newTestStore(t)
	seedBasics(t, s)

	// Last covered day: purchase + 29 = 2026-01-30.
	resp := s.Dispatch(Request{Op: "apply_repair", DeviceID: "D1", Fault: "screen", ApplyDate: "2026-01-30"})
	mustOK(t, resp)

	// A device can only have one open ticket.
	resp = s.Dispatch(Request{Op: "apply_repair", DeviceID: "D1", Fault: "battery", ApplyDate: "2026-01-20"})
	mustFail(t, resp, "already has open ticket")

	mustOK(t, s.Dispatch(Request{Op: "cancel_ticket", TicketID: "T1"}))

	// Expiry day itself (purchase + 30) is not covered.
	resp = s.Dispatch(Request{Op: "apply_repair", DeviceID: "D1", Fault: "screen", ApplyDate: "2026-01-31"})
	mustFail(t, resp, "not eligible")

	// Before purchase date is rejected.
	resp = s.Dispatch(Request{Op: "apply_repair", DeviceID: "D1", Fault: "screen", ApplyDate: "2025-12-31"})
	mustFail(t, resp, "not eligible")
	if joined := joinReasons(failReasons(resp)); !containsStr(joined, "before purchase date") {
		t.Fatalf("missing before-purchase reason, got %v", failReasons(resp))
	}

	// Uncovered fault category is rejected even within the date window.
	resp = s.Dispatch(Request{Op: "apply_repair", DeviceID: "D1", Fault: "keyboard", ApplyDate: "2026-01-15"})
	mustFail(t, resp, "not eligible")
	if joined := joinReasons(failReasons(resp)); !containsStr(joined, "not covered") {
		t.Fatalf("missing uncovered-fault reason, got %v", failReasons(resp))
	}
}

func joinReasons(rs []string) string {
	out := ""
	for _, r := range rs {
		out += r + "|"
	}
	return out
}

func TestUnknownReferencesAndValidation(t *testing.T) {
	s := newTestStore(t)
	mustFail(t, s.Dispatch(Request{
		Op: "register_model", ModelID: "M1", WarrantyDays: 0, FaultCategories: []string{"x"},
	}), "positive integer")
	mustFail(t, s.Dispatch(Request{
		Op: "register_device", DeviceID: "D1", ModelID: "NOPE", PurchaseDate: "2026-01-01",
	}), "unknown model")

	seedBasics(t, s)
	mustFail(t, s.Dispatch(Request{Op: "receive_part", PartID: "P1", Quantity: 0}), "positive integer")
	mustFail(t, s.Dispatch(Request{Op: "apply_repair", DeviceID: "NOPE", Fault: "screen", ApplyDate: "2026-01-10"}), "unknown device")
}

func TestApproveConsumesOrReleasesAtomically(t *testing.T) {
	s := newTestStore(t)
	seedBasics(t, s)
	mustOK(t, s.Dispatch(Request{Op: "receive_part", PartID: "P2", Quantity: 1}))
	mustOK(t, s.Dispatch(Request{Op: "apply_repair", DeviceID: "D1", Fault: "screen", ApplyDate: "2026-01-10"}))

	// Duplicate part lines rejected.
	mustFail(t, s.Dispatch(Request{Op: "approve_ticket", TicketID: "T1", PartLines: []PartQuantity{
		{PartID: "P1", Quantity: 1}, {PartID: "P1", Quantity: 2},
	}}), "more than once")

	// Shortage on P2 must not deduct anything (P1 stays at 5 available).
	mustFail(t, s.Dispatch(Request{Op: "approve_ticket", TicketID: "T1", PartLines: []PartQuantity{
		{PartID: "P1", Quantity: 4}, {PartID: "P2", Quantity: 2},
	}}), "insufficient stock")
	part := s.state.Parts["P1"]
	if part.Available != 5 || part.Reserved != 0 {
		t.Fatalf("stock changed after failed approval: %+v", part)
	}

	mustOK(t, s.Dispatch(Request{Op: "approve_ticket", TicketID: "T1", PartLines: []PartQuantity{
		{PartID: "P1", Quantity: 4}, {PartID: "P2", Quantity: 1},
	}}))
	part = s.state.Parts["P1"]
	if part.Available != 1 || part.Reserved != 4 {
		t.Fatalf("unexpected P1 after approve: %+v", part)
	}

	// Only pending tickets can be approved.
	mustFail(t, s.Dispatch(Request{Op: "approve_ticket", TicketID: "T1"}), "only pending")

	// Empty parts list allowed for a pending ticket.
	mustOK(t, s.Dispatch(Request{Op: "register_model", ModelID: "M2", WarrantyDays: 10, FaultCategories: []string{"screen"}}))
	mustOK(t, s.Dispatch(Request{Op: "register_device", DeviceID: "D2", ModelID: "M2", PurchaseDate: "2026-02-01"}))
	mustOK(t, s.Dispatch(Request{Op: "apply_repair", DeviceID: "D2", Fault: "screen", ApplyDate: "2026-02-01"}))
	mustOK(t, s.Dispatch(Request{Op: "approve_ticket", TicketID: "T2"}))
	mustOK(t, s.Dispatch(Request{Op: "complete_ticket", TicketID: "T2"}))

	// Cancel approved ticket releases reservations intact.
	mustOK(t, s.Dispatch(Request{Op: "cancel_ticket", TicketID: "T1"}))
	part = s.state.Parts["P1"]
	if part.Available != 5 || part.Reserved != 0 || part.Consumed != 0 {
		t.Fatalf("cancel did not release stock: %+v", part)
	}
	// Ended tickets reject repeated operations.
	mustFail(t, s.Dispatch(Request{Op: "cancel_ticket", TicketID: "T1"}), "already")
	mustFail(t, s.Dispatch(Request{Op: "complete_ticket", TicketID: "T2"}), "already")
}

func TestCompleteConsumesAndInboundKeepsUsage(t *testing.T) {
	s := newTestStore(t)
	seedBasics(t, s)
	mustOK(t, s.Dispatch(Request{Op: "apply_repair", DeviceID: "D1", Fault: "screen", ApplyDate: "2026-01-10"}))
	mustOK(t, s.Dispatch(Request{Op: "approve_ticket", TicketID: "T1", PartLines: []PartQuantity{{PartID: "P1", Quantity: 3}}}))
	mustOK(t, s.Dispatch(Request{Op: "complete_ticket", TicketID: "T1"}))

	part := s.state.Parts["P1"]
	if part.Available != 2 || part.Reserved != 0 || part.Consumed != 3 {
		t.Fatalf("unexpected P1 after complete: %+v", part)
	}
	// Further inbound only raises available; consumed history stays.
	mustOK(t, s.Dispatch(Request{Op: "receive_part", PartID: "P1", Quantity: 10}))
	part = s.state.Parts["P1"]
	if part.Available != 12 || part.Consumed != 3 {
		t.Fatalf("inbound rewrote usage: %+v", part)
	}

	resp := s.Dispatch(Request{Op: "query_part", PartID: "P1"})
	mustOK(t, resp)
	r := resp.Result.(map[string]interface{})
	if r["consumed"] != 3 || r["reserved"] != 0 || r["available"] != 12 {
		t.Fatalf("query_part mismatch: %v", r)
	}
}

func TestPersistAcrossReopenAndCancelRelease(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	seedBasics(t, s)
	mustOK(t, s.Dispatch(Request{Op: "apply_repair", DeviceID: "D1", Fault: "screen", ApplyDate: "2026-01-10"}))
	mustOK(t, s.Dispatch(Request{Op: "approve_ticket", TicketID: "T1", PartLines: []PartQuantity{{PartID: "P1", Quantity: 2}}}))

	// Reopen: reserved stock and ticket history survive.
	s2, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	part := s2.state.Parts["P1"]
	if part.Available != 3 || part.Reserved != 2 {
		t.Fatalf("state not persisted: %+v", part)
	}
	ticket := s2.state.Tickets["T1"]
	if ticket.Status != StatusApproved || len(ticket.History) != 2 {
		t.Fatalf("ticket not persisted: %+v", ticket)
	}

	// Cancel after reopen releases the reservation.
	mustOK(t, s2.Dispatch(Request{Op: "cancel_ticket", TicketID: "T1"}))
	part = s2.state.Parts["P1"]
	if part.Available != 5 || part.Reserved != 0 {
		t.Fatalf("release after reopen failed: %+v", part)
	}

	// Reopen once more and verify the release persisted and new ticket gets a
	// fresh unique id.
	s3, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	part = s3.state.Parts["P1"]
	if part.Available != 5 || part.Reserved != 0 {
		t.Fatalf("release not persisted: %+v", part)
	}
	resp := s3.Dispatch(Request{Op: "apply_repair", DeviceID: "D1", Fault: "screen", ApplyDate: "2026-01-11"})
	mustOK(t, resp)
	if resp.Result.(map[string]interface{})["ticket_id"] != "T2" {
		t.Fatalf("expected new ticket T2, got %v", resp.Result)
	}
}

func TestEligibilityQuery(t *testing.T) {
	s := newTestStore(t)
	seedBasics(t, s)
	resp := s.Dispatch(Request{Op: "check_eligibility", DeviceID: "D1", Fault: "keyboard", ApplyDate: "2026-02-01"})
	mustOK(t, resp)
	r := resp.Result.(map[string]interface{})
	if r["eligible"] != false || len(r["reasons"].([]string)) != 2 {
		t.Fatalf("expected two reasons, got %v", r)
	}
}
