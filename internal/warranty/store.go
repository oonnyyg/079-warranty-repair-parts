package warranty

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const dateLayout = "2006-01-02"

// Store holds in-memory state backed by a single JSON file in the state
// directory. Every mutating request is followed by a save so a restart after
// a successful response keeps the data.
type Store struct {
	state    *State
	filePath string
}

// OpenStore loads state from dir, creating it (and an empty state) on first
// use.
func OpenStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("state directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	s := &Store{
		state:    newState(),
		filePath: filepath.Join(dir, "warranty-state.json"),
	}
	data, err := os.ReadFile(s.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state file: %w", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, s.state); err != nil {
			return nil, fmt.Errorf("parse state file: %w", err)
		}
		if s.state.Models == nil {
			s.state = newState()
		}
	}
	return s, nil
}

func (s *Store) save() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.filePath)
}

func parseDate(s string) (time.Time, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("date %q must use YYYY-MM-DD", s)
	}
	return t, nil
}

// eligibilityReasons returns the human-readable reasons a device does not
// cover a fault on the given application date. An empty slice means the
// request is eligible. The last day covered is purchase + days - 1; the
// expiry day itself is NOT covered.
func eligibilityReasons(d Device, fault, applyDate string) []string {
	var reasons []string
	purchase, err := parseDate(d.PurchaseDate)
	if err != nil {
		reasons = append(reasons, err.Error())
		return reasons
	}
	apply, err := parseDate(applyDate)
	if err != nil {
		reasons = append(reasons, err.Error())
		return reasons
	}
	start := purchase
	end := purchase.AddDate(0, 0, d.WarrantyDays-1)
	if apply.Before(start) {
		reasons = append(reasons, "application date is before purchase date")
	}
	if apply.After(end) {
		reasons = append(reasons, fmt.Sprintf("warranty expired on or before %s", end.Format(dateLayout)))
	}
	if !contains(d.FaultCategories, fault) {
		reasons = append(reasons, fmt.Sprintf("fault category %q is not covered by warranty", fault))
	}
	return reasons
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
