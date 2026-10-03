package warranty

// Model is a product model. Warranty rules are captured on the model and
// copied onto each device at registration time.
type Model struct {
	WarrantyDays    int      `json:"warranty_days"`
	FaultCategories []string `json:"fault_categories"`
}

// Device stores the warranty rules as they were when the device was
// registered, so later model changes cannot affect existing devices.
type Device struct {
	ModelID         string   `json:"model_id"`
	PurchaseDate    string   `json:"purchase_date"`
	WarrantyDays    int      `json:"warranty_days"`
	FaultCategories []string `json:"fault_categories"`
}

// Part tracks stock in three independent counters. Inbound stock only ever
// increases Available; past consumption is never rewritten.
type Part struct {
	Available int `json:"available"`
	Reserved  int `json:"reserved"`
	Consumed  int `json:"consumed"`
}

// PartQuantity is a part id paired with a positive quantity.
type PartQuantity struct {
	PartID   string `json:"part_id"`
	Quantity int    `json:"quantity"`
}

// Ticket statuses.
const (
	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusCancelled = "cancelled"
	StatusCompleted = "completed"
)

// Ticket is a warranty repair work order.
type Ticket struct {
	ID        string         `json:"id"`
	DeviceID  string         `json:"device_id"`
	Fault     string         `json:"fault"`
	ApplyDate string         `json:"apply_date"`
	Status    string         `json:"status"`
	PartLines []PartQuantity `json:"part_lines,omitempty"`
	History   []TicketEvent  `json:"history"`
}

// TicketEvent records one status change together with where the parts went.
type TicketEvent struct {
	Action string         `json:"action"`
	Date   string         `json:"date"`
	Parts  []PartQuantity `json:"parts,omitempty"`
}

// State is everything persisted to the state directory.
type State struct {
	Models  map[string]Model   `json:"models"`
	Devices map[string]Device  `json:"devices"`
	Parts   map[string]Part    `json:"parts"`
	Tickets map[string]*Ticket `json:"tickets"`
	NextSeq int                `json:"next_ticket_seq"`
}

func newState() *State {
	return &State{
		Models:  map[string]Model{},
		Devices: map[string]Device{},
		Parts:   map[string]Part{},
		Tickets: map[string]*Ticket{},
		NextSeq: 1,
	}
}
