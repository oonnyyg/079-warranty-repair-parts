package warranty

import (
	"fmt"
	"sort"
	"strconv"
)

// Request is one line of input. Unknown fields are ignored; Op selects the
// operation.
type Request struct {
	Op              string         `json:"op"`
	ModelID         string         `json:"model_id"`
	WarrantyDays    int            `json:"warranty_days"`
	FaultCategory   string         `json:"fault_category"`
	FaultCategories []string       `json:"fault_categories"`
	DeviceID        string         `json:"device_id"`
	PurchaseDate    string         `json:"purchase_date"`
	PartID          string         `json:"part_id"`
	PartLines       []PartQuantity `json:"parts"`
	Quantity        int            `json:"quantity"`
	TicketID        string         `json:"ticket_id"`
	Fault           string         `json:"fault"`
	ApplyDate       string         `json:"apply_date"`
}

// Response is one line of output.
type Response struct {
	OK     bool        `json:"ok"`
	Error  string      `json:"error,omitempty"`
	Result interface{} `json:"result,omitempty"`
}

// Dispatch executes one request, persisting after successful mutations.
func (s *Store) Dispatch(req Request) Response {
	var err error
	switch req.Op {
	case "register_model":
		err = s.registerModel(req)
	case "register_device":
		err = s.registerDevice(req)
	case "receive_part":
		err = s.receivePart(req)
	case "apply_repair":
		return s.applyRepair(req)
	case "approve_ticket":
		err = s.approveTicket(req)
	case "cancel_ticket":
		err = s.transitionTicket(req.TicketID, StatusCancelled)
	case "complete_ticket":
		err = s.transitionTicket(req.TicketID, StatusCompleted)
	case "query_ticket":
		return s.queryTicket(req)
	case "query_part":
		return s.queryPart(req)
	case "check_eligibility":
		return s.checkEligibility(req)
	default:
		err = fmt.Errorf("unknown op %q", req.Op)
	}
	if err != nil {
		return fail(err)
	}
	if err := s.save(); err != nil {
		return fail(fmt.Errorf("persist state: %w", err))
	}
	return Response{OK: true}
}

func fail(err error) Response {
	return Response{OK: false, Error: err.Error()}
}

func (s *Store) registerModel(req Request) error {
	if req.ModelID == "" {
		return fmt.Errorf("model_id is required")
	}
	if _, exists := s.state.Models[req.ModelID]; exists {
		return fmt.Errorf("model %q already registered", req.ModelID)
	}
	if req.WarrantyDays <= 0 {
		return fmt.Errorf("warranty_days must be a positive integer")
	}
	if len(req.FaultCategories) == 0 {
		return fmt.Errorf("fault_categories must contain at least one category")
	}
	seen := map[string]bool{}
	for _, c := range req.FaultCategories {
		if c == "" {
			return fmt.Errorf("fault category must not be empty")
		}
		if seen[c] {
			return fmt.Errorf("duplicate fault category %q", c)
		}
		seen[c] = true
	}
	s.state.Models[req.ModelID] = Model{
		WarrantyDays:    req.WarrantyDays,
		FaultCategories: append([]string(nil), req.FaultCategories...),
	}
	return nil
}

func (s *Store) registerDevice(req Request) error {
	if req.DeviceID == "" {
		return fmt.Errorf("device_id is required")
	}
	if _, exists := s.state.Devices[req.DeviceID]; exists {
		return fmt.Errorf("device %q already registered", req.DeviceID)
	}
	model, ok := s.state.Models[req.ModelID]
	if !ok {
		return fmt.Errorf("unknown model %q", req.ModelID)
	}
	if _, err := parseDate(req.PurchaseDate); err != nil {
		return err
	}
	// Freeze the warranty rules at registration time.
	s.state.Devices[req.DeviceID] = Device{
		ModelID:         req.ModelID,
		PurchaseDate:    req.PurchaseDate,
		WarrantyDays:    model.WarrantyDays,
		FaultCategories: append([]string(nil), model.FaultCategories...),
	}
	return nil
}

func (s *Store) receivePart(req Request) error {
	if req.PartID == "" {
		return fmt.Errorf("part_id is required")
	}
	if req.Quantity <= 0 {
		return fmt.Errorf("quantity must be a positive integer")
	}
	p := s.state.Parts[req.PartID]
	p.Available += req.Quantity
	s.state.Parts[req.PartID] = p
	return nil
}

func (s *Store) applyRepair(req Request) Response {
	if req.DeviceID == "" {
		return fail(fmt.Errorf("device_id is required"))
	}
	device, ok := s.state.Devices[req.DeviceID]
	if !ok {
		return fail(fmt.Errorf("unknown device %q", req.DeviceID))
	}
	if req.Fault == "" {
		return fail(fmt.Errorf("fault category is required"))
	}
	if _, err := parseDate(req.ApplyDate); err != nil {
		return fail(err)
	}
	reasons := eligibilityReasons(device, req.Fault, req.ApplyDate)
	if len(reasons) > 0 {
		return Response{OK: false, Error: "repair not eligible", Result: map[string]interface{}{
			"eligible": false,
			"reasons":  reasons,
		}}
	}
	for _, t := range s.state.Tickets {
		if t.DeviceID == req.DeviceID && !isEnded(t.Status) {
			return fail(fmt.Errorf("device %q already has open ticket %q", req.DeviceID, t.ID))
		}
	}
	id := "T" + strconv.Itoa(s.state.NextSeq)
	s.state.NextSeq++
	ticket := &Ticket{
		ID:        id,
		DeviceID:  req.DeviceID,
		Fault:     req.Fault,
		ApplyDate: req.ApplyDate,
		Status:    StatusPending,
		History: []TicketEvent{{
			Action: "applied",
			Date:   req.ApplyDate,
		}},
	}
	s.state.Tickets[id] = ticket
	if err := s.save(); err != nil {
		return fail(fmt.Errorf("persist state: %w", err))
	}
	return Response{OK: true, Result: map[string]interface{}{
		"ticket_id": id,
		"status":    StatusPending,
	}}
}

func isEnded(status string) bool {
	return status == StatusCancelled || status == StatusCompleted
}

func validatePartLines(lines []PartQuantity) (map[string]int, error) {
	out := map[string]int{}
	seen := map[string]bool{}
	for _, line := range lines {
		if line.PartID == "" {
			return nil, fmt.Errorf("part_id is required")
		}
		if seen[line.PartID] {
			return nil, fmt.Errorf("part %q listed more than once", line.PartID)
		}
		seen[line.PartID] = true
		if line.Quantity <= 0 {
			return nil, fmt.Errorf("quantity for part %q must be a positive integer", line.PartID)
		}
		out[line.PartID] = line.Quantity
	}
	return out, nil
}

func (s *Store) approveTicket(req Request) error {
	ticket, ok := s.state.Tickets[req.TicketID]
	if !ok {
		return fmt.Errorf("unknown ticket %q", req.TicketID)
	}
	if ticket.Status != StatusPending {
		return fmt.Errorf("ticket %q is %s, only pending tickets can be approved", req.TicketID, ticket.Status)
	}
	need, err := validatePartLines(req.PartLines)
	if err != nil {
		return err
	}
	// All parts must exist with sufficient stock; check before mutating.
	ids := make([]string, 0, len(need))
	for id := range need {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		part, ok := s.state.Parts[id]
		if !ok {
			return fmt.Errorf("unknown part %q", id)
		}
		if part.Available < need[id] {
			return fmt.Errorf("insufficient stock for part %q: available %d, need %d", id, part.Available, need[id])
		}
	}
	// Reserve atomically.
	for _, id := range ids {
		part := s.state.Parts[id]
		part.Available -= need[id]
		part.Reserved += need[id]
		s.state.Parts[id] = part
	}
	ticket.Status = StatusApproved
	ticket.PartLines = append([]PartQuantity(nil), req.PartLines...)
	ticket.History = append(ticket.History, TicketEvent{
		Action: "approved",
		Date:   req.ApplyDate,
		Parts:  append([]PartQuantity(nil), req.PartLines...),
	})
	return nil
}

func (s *Store) transitionTicket(id, target string) error {
	ticket, ok := s.state.Tickets[id]
	if !ok {
		return fmt.Errorf("unknown ticket %q", id)
	}
	if isEnded(ticket.Status) {
		return fmt.Errorf("ticket %q is already %s", id, ticket.Status)
	}
	switch target {
	case StatusCancelled:
		// Pending or approved tickets may be cancelled. Approval reservations
		// are released back to available stock.
		if ticket.Status == StatusApproved {
			for _, line := range ticket.PartLines {
				part := s.state.Parts[line.PartID]
				part.Reserved -= line.Quantity
				part.Available += line.Quantity
				s.state.Parts[line.PartID] = part
			}
		}
	case StatusCompleted:
		if ticket.Status != StatusApproved {
			return fmt.Errorf("ticket %q is %s, only approved tickets can be completed", id, ticket.Status)
		}
		for _, line := range ticket.PartLines {
			part := s.state.Parts[line.PartID]
			part.Reserved -= line.Quantity
			part.Consumed += line.Quantity
			s.state.Parts[line.PartID] = part
		}
	default:
		return fmt.Errorf("unsupported target status %q", target)
	}
	ticket.Status = target
	ticket.History = append(ticket.History, TicketEvent{
		Action: target,
		Date:   "",
		Parts:  append([]PartQuantity(nil), ticket.PartLines...),
	})
	return nil
}
