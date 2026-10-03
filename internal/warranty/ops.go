package warranty

import (
	"errors"
	"fmt"
)

// RegisterModel adds a new model with warranty rules.
func (s *Store) RegisterModel(name string, days int, categories []string) error {
	if name == "" {
		return errors.New("model name is required")
	}
	if _, ok := s.Models[name]; ok {
		return fmt.Errorf("model %q already registered", name)
	}
	if days <= 0 {
		return errors.New("warranty_days must be a positive integer")
	}
	if len(categories) == 0 {
		return errors.New("covered_categories must not be empty")
	}
	seen := map[string]bool{}
	for _, c := range categories {
		if c == "" {
			return errors.New("covered_categories must not contain empty names")
		}
		if seen[c] {
			return fmt.Errorf("duplicate category %q", c)
		}
		seen[c] = true
	}
	s.Models[name] = ModelRules{WarrantyDays: days, CoveredCategories: append([]string(nil), categories...)}
	return nil
}

// RegisterDevice adds a device, freezing the model's current rules.
func (s *Store) RegisterDevice(id, model, purchaseDate string) error {
	if id == "" {
		return errors.New("device id is required")
	}
	if _, ok := s.Devices[id]; ok {
		return fmt.Errorf("device %q already registered", id)
	}
	m, ok := s.Models[model]
	if !ok {
		return fmt.Errorf("unknown model %q", model)
	}
	if _, err := parseDate(purchaseDate); err != nil {
		return err
	}
	s.Devices[id] = Device{
		Model:             model,
		PurchaseDate:      purchaseDate,
		WarrantyDays:      m.WarrantyDays,
		CoveredCategories: append([]string(nil), m.CoveredCategories...),
	}
	return nil
}

// StockPart adds quantity to a part's inventory without touching usage.
func (s *Store) StockPart(part string, qty int) error {
	if part == "" {
		return errors.New("part name is required")
	}
	if qty <= 0 {
		return errors.New("quantity must be a positive integer")
	}
	st := s.Parts[part]
	if st == nil {
		st = &PartStock{}
		s.Parts[part] = st
	}
	st.Total += qty
	return nil
}

// CheckEligibility validates a repair request, returning all failure reasons.
func (s *Store) CheckEligibility(deviceID, category, requestDate string) (bool, []string) {
	var reasons []string
	dev, ok := s.Devices[deviceID]
	if !ok {
		return false, []string{fmt.Sprintf("unknown device %q", deviceID)}
	}
	if !contains(dev.CoveredCategories, category) {
		reasons = append(reasons, fmt.Sprintf("category %q is not covered by model %q", category, dev.Model))
	}
	req, err := parseDate(requestDate)
	if err != nil {
		reasons = append(reasons, err.Error())
	} else {
		purchase, _ := parseDate(dev.PurchaseDate)
		expiry := purchase.AddDate(0, 0, dev.WarrantyDays)
		if req.Before(purchase) {
			reasons = append(reasons, "request date is before purchase date")
		} else if !req.Before(expiry) {
			reasons = append(reasons, fmt.Sprintf("warranty expired on %s", expiry.Format(dateLayout)))
		}
	}
	if s.openTicketOf(deviceID) != nil {
		reasons = append(reasons, fmt.Sprintf("device %q already has an open ticket", deviceID))
	}
	return len(reasons) == 0, reasons
}

func (s *Store) openTicketOf(deviceID string) *Ticket {
	for _, t := range s.Tickets {
		if t.Device == deviceID && (t.Status == StatusPending || t.Status == StatusApproved) {
			return t
		}
	}
	return nil
}

// RequestRepair creates a pending ticket after an eligibility check.
func (s *Store) RequestRepair(deviceID, category, requestDate string) (*Ticket, error) {
	ok, reasons := s.CheckEligibility(deviceID, category, requestDate)
	if !ok {
		return nil, fmt.Errorf("repair not eligible: %v", reasons)
	}
	t := &Ticket{
		ID:       fmt.Sprintf("T%06d", s.nextID),
		Device:   deviceID,
		Category: category,
		Request:  requestDate,
		Status:   StatusPending,
	}
	s.nextID++
	t.History = append(t.History, HistoryEntry{Status: StatusPending})
	s.Tickets[t.ID] = t
	return t, nil
}

// Approve reserves parts atomically and marks a pending ticket approved.
func (s *Store) Approve(ticketID string, parts []PartReservation) error {
	t, ok := s.Tickets[ticketID]
	if !ok {
		return fmt.Errorf("unknown ticket %q", ticketID)
	}
	if t.Status != StatusPending {
		return fmt.Errorf("ticket %q is %s, only pending tickets can be approved", ticketID, t.Status)
	}
	seen := map[string]bool{}
	for _, p := range parts {
		if p.Part == "" {
			return errors.New("part name is required")
		}
		if p.Quantity <= 0 {
			return fmt.Errorf("quantity for part %q must be a positive integer", p.Part)
		}
		if seen[p.Part] {
			return fmt.Errorf("duplicate part %q", p.Part)
		}
		seen[p.Part] = true
		st := s.Parts[p.Part]
		avail := 0
		if st != nil {
			avail = st.Total - st.Reserved - st.Consumed
		}
		if avail < p.Quantity {
			return fmt.Errorf("insufficient stock for part %q: need %d, available %d", p.Part, p.Quantity, avail)
		}
	}
	for _, p := range parts {
		s.Parts[p.Part].Reserved += p.Quantity
	}
	t.Parts = append([]PartReservation(nil), parts...)
	t.Status = StatusApproved
	t.History = append(t.History, HistoryEntry{Status: StatusApproved, Parts: t.Parts})
	return nil
}

// Complete consumes reserved parts and closes an approved ticket.
func (s *Store) Complete(ticketID string) error {
	t, ok := s.Tickets[ticketID]
	if !ok {
		return fmt.Errorf("unknown ticket %q", ticketID)
	}
	if t.Status != StatusApproved {
		return fmt.Errorf("ticket %q is %s, only approved tickets can be completed", ticketID, t.Status)
	}
	for _, p := range t.Parts {
		st := s.Parts[p.Part]
		st.Reserved -= p.Quantity
		st.Consumed += p.Quantity
	}
	t.Status = StatusCompleted
	t.History = append(t.History, HistoryEntry{Status: StatusCompleted, Parts: t.Parts})
	return nil
}

// Cancel releases any reserved parts and closes the ticket.
func (s *Store) Cancel(ticketID string) error {
	t, ok := s.Tickets[ticketID]
	if !ok {
		return fmt.Errorf("unknown ticket %q", ticketID)
	}
	if t.Status != StatusPending && t.Status != StatusApproved {
		return fmt.Errorf("ticket %q is %s, only pending or approved tickets can be cancelled", ticketID, t.Status)
	}
	if t.Status == StatusApproved {
		for _, p := range t.Parts {
			s.Parts[p.Part].Reserved -= p.Quantity
		}
	}
	t.Status = StatusCancelled
	t.History = append(t.History, HistoryEntry{Status: StatusCancelled, Parts: t.Parts})
	return nil
}

// PartInfo reports available, reserved and consumed quantities.
func (s *Store) PartInfo(part string) (avail, reserved, consumed int, err error) {
	st, ok := s.Parts[part]
	if !ok {
		return 0, 0, 0, fmt.Errorf("unknown part %q", part)
	}
	return st.Total - st.Reserved - st.Consumed, st.Reserved, st.Consumed, nil
}
