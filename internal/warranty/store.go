package warranty

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const dateLayout = "2006-01-02"

// ModelRules describes a registered product model.
type ModelRules struct {
	WarrantyDays      int      `json:"warranty_days"`
	CoveredCategories []string `json:"covered_categories"`
}

// Device is a registered device; warranty rules are frozen at registration.
type Device struct {
	Model             string   `json:"model"`
	PurchaseDate      string   `json:"purchase_date"`
	WarrantyDays      int      `json:"warranty_days"`
	CoveredCategories []string `json:"covered_categories"`
}

// PartStock tracks a spare part's quantities.
type PartStock struct {
	Total    int `json:"total"`
	Reserved int `json:"reserved"`
	Consumed int `json:"consumed"`
}

// PartReservation records parts reserved by an approved ticket.
type PartReservation struct {
	Part     string `json:"part"`
	Quantity int    `json:"quantity"`
}

// HistoryEntry records a ticket status change.
type HistoryEntry struct {
	Status string            `json:"status"`
	Parts  []PartReservation `json:"parts,omitempty"`
}

// Ticket is a repair work order.
type Ticket struct {
	ID       string            `json:"id"`
	Device   string            `json:"device"`
	Category string            `json:"category"`
	Request  string            `json:"request_date"`
	Status   string            `json:"status"`
	Parts    []PartReservation `json:"parts,omitempty"`
	History  []HistoryEntry    `json:"history"`
}

const (
	StatusPending   = "pending_approval"
	StatusApproved  = "approved"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
)

// Store holds all persistent state.
type Store struct {
	Models  map[string]ModelRules `json:"models"`
	Devices map[string]Device     `json:"devices"`
	Parts   map[string]*PartStock `json:"parts"`
	Tickets map[string]*Ticket    `json:"tickets"`

	nextID int
	path   string
}

// Load opens (or creates) the state file inside dir.
func Load(dir string) (*Store, error) {
	s := &Store{
		Models:  map[string]ModelRules{},
		Devices: map[string]Device{},
		Parts:   map[string]*PartStock{},
		Tickets: map[string]*Ticket{},
		nextID:  1,
		path:    filepath.Join(dir, "state.json"),
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var disk struct {
		Models  map[string]ModelRules `json:"models"`
		Devices map[string]Device     `json:"devices"`
		Parts   map[string]*PartStock `json:"parts"`
		Tickets map[string]*Ticket    `json:"tickets"`
		NextID  int                   `json:"next_id"`
	}
	if err := json.Unmarshal(data, &disk); err != nil {
		return nil, fmt.Errorf("corrupt state file: %w", err)
	}
	if disk.Models != nil {
		s.Models = disk.Models
	}
	if disk.Devices != nil {
		s.Devices = disk.Devices
	}
	if disk.Parts != nil {
		s.Parts = disk.Parts
	}
	if disk.Tickets != nil {
		s.Tickets = disk.Tickets
	}
	if disk.NextID > 0 {
		s.nextID = disk.NextID
	}
	return s, nil
}

// Save persists the store atomically.
func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	disk := struct {
		Models  map[string]ModelRules `json:"models"`
		Devices map[string]Device     `json:"devices"`
		Parts   map[string]*PartStock `json:"parts"`
		Tickets map[string]*Ticket    `json:"tickets"`
		NextID  int                   `json:"next_id"`
	}{s.Models, s.Devices, s.Parts, s.Tickets, s.nextID}
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func parseDate(v string) (time.Time, error) {
	t, err := time.Parse(dateLayout, v)
	if err != nil {
		return t, fmt.Errorf("invalid date %q, want YYYY-MM-DD", v)
	}
	return t, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// sortedTickets returns tickets ordered by ID for deterministic output.
func (s *Store) sortedTickets() []*Ticket {
	out := make([]*Ticket, 0, len(s.Tickets))
	for _, t := range s.Tickets {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
