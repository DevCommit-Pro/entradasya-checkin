package checkin

import "time"

type Input struct {
	Code    string `json:"code"`
	EventID string `json:"eventId"`
}
type Result struct {
	Result          string     `json:"result"`
	Reason          *string    `json:"reason"`
	Code            string     `json:"code"`
	HolderName      *string    `json:"holderName"`
	SectorName      *string    `json:"sectorName"`
	TicketEventName *string    `json:"ticketEventName"`
	FirstEntryAt    *time.Time `json:"firstEntryAt"`
	CheckedAt       time.Time  `json:"checkedAt"`
}
type Ticket struct {
	ID         string     `json:"id"`
	Code       string     `json:"code"`
	EventID    string     `json:"eventId"`
	EventName  string     `json:"eventName"`
	HolderName string     `json:"holderName"`
	SectorName string     `json:"sectorName"`
	Status     string     `json:"status"`
	UsedAt     *time.Time `json:"usedAt"`
}
type EventContext struct {
	EventID     string `json:"eventId"`
	OrganizerID string `json:"organizerId"`
	Status      string `json:"status"`
}
type UseInput struct {
	EventID     string `json:"eventId"`
	ScanID      string `json:"scanId"`
	DoormanID   string `json:"doormanId"`
	DoormanName string `json:"doormanName"`
}
type Usage struct {
	ScanID      string    `json:"scanId"`
	TicketID    string    `json:"ticketId"`
	Code        string    `json:"code"`
	EventID     string    `json:"eventId"`
	UsedAt      time.Time `json:"usedAt"`
	DoormanID   string    `json:"doormanId"`
	DoormanName string    `json:"doormanName"`
	HolderName  string    `json:"holderName"`
	SectorName  string    `json:"sectorName"`
}
type UseResult struct {
	Ticket Ticket `json:"ticket"`
	Usage  Usage  `json:"usage"`
}
type Counts struct {
	EventID string `json:"eventId"`
	Issued  int    `json:"issued"`
	Used    int    `json:"used"`
}
type Record struct {
	Code        string    `json:"code"`
	HolderName  string    `json:"holderName"`
	SectorName  string    `json:"sectorName"`
	At          time.Time `json:"at"`
	DoormanName string    `json:"doormanName"`
}
type Stats struct {
	EventID string   `json:"eventId"`
	Entered int      `json:"entered"`
	Issued  int      `json:"issued"`
	Recent  []Record `json:"recent"`
}
