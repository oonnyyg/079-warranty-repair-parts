package warranty

import (
	"encoding/json"
)

type request struct {
	Op         string            `json:"op"`
	Model      string            `json:"model"`
	Device     string            `json:"device"`
	Category   string            `json:"category"`
	Date       string            `json:"date"`
	Part       string            `json:"part"`
	Quantity   int               `json:"quantity"`
	Ticket     string            `json:"ticket"`
	Days       int               `json:"warranty_days"`
	Categories []string          `json:"covered_categories"`
	Parts      []PartReservation `json:"parts"`
}

// HandleLine processes one JSON request line and returns a JSON response.
// Mutating operations persist the store before a success is reported.
func (s *Store) HandleLine(line []byte) []byte {
	resp := s.dispatch(line)
	out, err := json.Marshal(resp)
	if err != nil {
		out = []byte(`{"ok":false,"error":"internal encoding error"}`)
	}
	return out
}

func (s *Store) dispatch(line []byte) map[string]any {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return fail("invalid JSON: " + err.Error())
	}
	switch req.Op {
	case "register_model":
		return s.mutate(s.RegisterModel(req.Model, req.Days, req.Categories))
	case "register_device":
		return s.mutate(s.RegisterDevice(req.Device, req.Model, req.Date))
	case "stock_part":
		return s.mutate(s.StockPart(req.Part, req.Quantity))
	case "request_repair":
		t, err := s.RequestRepair(req.Device, req.Category, req.Date)
		if err != nil {
			return fail(err.Error())
		}
		if err := s.Save(); err != nil {
			return fail("save failed: " + err.Error())
		}
		return okResp(map[string]any{"ticket": t.ID, "status": t.Status})
	case "check_eligibility":
		eligible, reasons := s.CheckEligibility(req.Device, req.Category, req.Date)
		return okResp(map[string]any{"eligible": eligible, "reasons": reasons})
	case "approve":
		return s.mutate(s.Approve(req.Ticket, req.Parts))
	case "complete":
		return s.mutate(s.Complete(req.Ticket))
	case "cancel":
		return s.mutate(s.Cancel(req.Ticket))
	case "query_ticket":
		t, ok := s.Tickets[req.Ticket]
		if !ok {
			return fail("unknown ticket " + req.Ticket)
		}
		return okResp(map[string]any{"ticket": t})
	case "query_part":
		avail, reserved, consumed, err := s.PartInfo(req.Part)
		if err != nil {
			return fail(err.Error())
		}
		return okResp(map[string]any{
			"part": req.Part, "available": avail,
			"reserved": reserved, "consumed": consumed,
		})
	case "list_tickets":
		return okResp(map[string]any{"tickets": s.sortedTickets()})
	default:
		return fail("unknown op " + req.Op)
	}
}

func (s *Store) mutate(err error) map[string]any {
	if err != nil {
		return fail(err.Error())
	}
	if err := s.Save(); err != nil {
		return fail("save failed: " + err.Error())
	}
	return okResp(nil)
}

func okResp(extra map[string]any) map[string]any {
	m := map[string]any{"ok": true}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func fail(msg string) map[string]any {
	if msg == "" {
		msg = "error"
	}
	return map[string]any{"ok": false, "error": msg}
}
