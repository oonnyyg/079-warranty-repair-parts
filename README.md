# Warranty Repair Parts

A local warranty repair work-order tool written in Go (standard library only).
It reads JSON requests line by line from stdin, answers each with one JSON
response line, and persists all state under a directory so data survives
restarts.

## Run

```sh
go run ./cmd/warranty --state <state-dir>
```

State is stored in `<state-dir>/state.json` and reloaded on the next start.
Every input line is an independent request; an invalid line returns
`{"ok":false,"error":"..."}` and processing continues with the next line.

## Commands

All requests are single-line JSON objects with an `op` field.

- `register_model` — `{"op":"register_model","model":"M1","warranty_days":365,"covered_categories":["screen","battery"]}`
  Registers a model. `warranty_days` must be a positive integer.
- `register_device` — `{"op":"register_device","device":"D1","model":"M1","date":"2026-01-01"}`
  Registers a device purchased on `date`; the model's warranty rules are
  frozen onto the device at this moment.
- `stock_part` — `{"op":"stock_part","part":"screen","quantity":5}`
  Adds a positive integer quantity to inventory. Never changes existing
  reserved/consumed counters.
- `check_eligibility` — `{"op":"check_eligibility","device":"D1","category":"screen","date":"2026-06-01"}`
  Returns `{"ok":true,"eligible":bool,"reasons":[...]}` listing every
  concrete reason the repair is not covered.
- `request_repair` — `{"op":"request_repair","device":"D1","category":"screen","date":"2026-06-01"}`
  Creates a `pending_approval` ticket (`T000001`, ...) after the same
  eligibility check. A device may have only one open (pending or approved)
  ticket.
- `approve` — `{"op":"approve","ticket":"T000001","parts":[{"part":"screen","quantity":2}]}`
  Only pending tickets. `parts` may be empty (repair without parts), each
  quantity must be a positive integer, and a part may not appear twice.
  Reservations are atomic: if any part lacks stock, nothing is reserved and
  the ticket stays pending.
- `complete` — `{"op":"complete","ticket":"T000001"}`
  Only approved tickets. Reserved parts become consumed.
- `cancel` — `{"op":"cancel","ticket":"T000001"}`
  Pending or approved tickets. Reserved parts are fully released.
  Finished (completed/cancelled) tickets reject further operations.
- `query_ticket` — `{"op":"query_ticket","ticket":"T000001"}`
  Returns the ticket with its full status-change history and the parts
  involved in each transition.
- `query_part` — `{"op":"query_part","part":"screen"}`
  Returns `available`, `reserved` and cumulative `consumed` quantities.
- `list_tickets` — `{"op":"list_tickets"}` returns all tickets sorted by ID.

## Warranty boundary

Dates use `YYYY-MM-DD`. A repair requested on date `R` for a device purchased
on `P` with `N` warranty days is eligible when `P <= R < P + N days`.
The expiry day `P + N` itself is **not** covered. Requests before the
purchase date, unknown devices/models/parts, and uncovered categories are
rejected; ineligible tickets cannot be created and therefore never reach
approval.

## Tests

```sh
go test ./...
```

The suite covers the warranty date boundary, eligibility reasons, the
one-open-ticket rule, atomic approval on insufficient stock, duplicate
parts, complete/cancel part flows, restocking without rewriting usage
counters, and querying released stock after a restart.
