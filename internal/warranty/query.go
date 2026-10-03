package warranty

import "fmt"

func (s *Store) queryTicket(req Request) Response {
	ticket, ok := s.state.Tickets[req.TicketID]
	if !ok {
		return fail(fmt.Errorf("unknown ticket %q", req.TicketID))
	}
	device := s.state.Devices[ticket.DeviceID]
	reasons := eligibilityReasons(device, ticket.Fault, ticket.ApplyDate)
	return Response{OK: true, Result: map[string]interface{}{
		"ticket":     ticket,
		"eligible":   len(reasons) == 0,
		"reasons":    reasons,
		"part_lines": ticket.PartLines,
	}}
}

func (s *Store) queryPart(req Request) Response {
	part, ok := s.state.Parts[req.PartID]
	if !ok {
		return fail(fmt.Errorf("unknown part %q", req.PartID))
	}
	return Response{OK: true, Result: map[string]interface{}{
		"part_id":   req.PartID,
		"available": part.Available,
		"reserved":  part.Reserved,
		"consumed":  part.Consumed,
	}}
}

func (s *Store) checkEligibility(req Request) Response {
	device, ok := s.state.Devices[req.DeviceID]
	if !ok {
		return fail(fmt.Errorf("unknown device %q", req.DeviceID))
	}
	if _, err := parseDate(req.ApplyDate); err != nil {
		return fail(err)
	}
	reasons := eligibilityReasons(device, req.Fault, req.ApplyDate)
	return Response{OK: true, Result: map[string]interface{}{
		"device_id": req.DeviceID,
		"fault":     req.Fault,
		"date":      req.ApplyDate,
		"eligible":  len(reasons) == 0,
		"reasons":   reasons,
	}}
}
