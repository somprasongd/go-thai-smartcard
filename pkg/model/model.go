package model

import (
	"fmt"
	"strconv"
)

type Data struct {
	Personal *Personal `json:"personal"`
	Card     *Card     `json:"card,omitempty"`
	Nhso     *Nhso     `json:"nhso,omitempty"`
	// Reader names the reader this card came from.
	//
	// With several readers attached, smc-inserted only says a card arrived and
	// says nothing about which one produced the data that follows it. Two cards
	// inserted close together leave a client unable to tell them apart, so the
	// data carries its own reader. It is additive: a client reading
	// data.personal still works without it.
	Reader string `json:"reader,omitempty"`
}

type Message struct {
	Event   string `json:"event"`
	Payload any    `json:"payload,omitempty"`
}

// Command is a client request on the control channel, for example
// {"action":"get-status"}.
//
// The card socket is read-only: settings change through /settings or the tray,
// never over the socket, and the sockets carry no derived copy of the config —
// a client that wants to know what the agent reads reads /api/settings. An
// action the agent does not know — including the removed get-options,
// set-options and set-reader — is answered with an smc-error naming the
// unknown action.
type Command struct {
	Action    string `json:"action"`
	RequestID string `json:"request_id,omitempty"`
	// Reply stays in-process; transports bind it to the requesting connection.
	Reply *CommandReply `json:"-"`
}

// Daemon states, as a client sees them.
const (
	// StateWaiting means no card is in and the agent is watching for one.
	StateWaiting = "waiting"
	// StateReading means a card is in and is being read right now.
	StateReading = "reading"
	// StateCardPresent means a card is in, has been read, and is waiting to be
	// taken out. This is the state a client may ask for another read in.
	StateCardPresent = "card-present"
)

// Status is what the agent is doing right now.
//
// It answers three questions a client cannot work out for itself: which readers
// are attached, which one the agent is watching, and whether it is in a state
// where asking for another read makes sense. Readers and selection are broadcast
// together because they change together.
type Status struct {
	Readers  []string `json:"readers"`
	Selected string   `json:"selected"`
	State    string   `json:"state"`
	Health   string   `json:"health,omitempty"`
}

type FormatedDate string

func NewFormatedDate(raw string) FormatedDate {
	if len(raw) != 8 {
		return ""
	}
	thaiYear := raw[:4]
	year, err := strconv.Atoi(thaiYear)
	if err != nil {
		return ""
	}

	return FormatedDate(
		fmt.Sprintf("%v-%s-%s", year-543, raw[4:6], raw[6:]),
	)
}

// CommandResult contains no card data and goes only to the requesting client.
type CommandResult struct {
	RequestID string `json:"request_id"`
	Action    string `json:"action"`
	Status    string `json:"status"`
	Code      string `json:"code,omitempty"`
}

// CommandReply is an in-process response sink; a pointer keeps Command comparable.
type CommandReply struct{ Send func(status, code string) }
