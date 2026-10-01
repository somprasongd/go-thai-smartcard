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

// Options is the wire form of the card reading options, so a client can ask
// for a different set of applets while the agent runs.
//
// It mirrors smc.Options instead of using it, because pkg/server must not
// import pkg/smc: the transport layer should not depend on the card logic, and
// a rename on the card side would otherwise silently change the wire format.
type Options struct {
	ShowFaceImage bool `json:"show_face_image"`
	ShowNhsoData  bool `json:"show_nhso"`
	ShowLaserData bool `json:"show_laser"`
}

// Command is a client request on the control channel, for example
// {"action":"set-options","options":{...}} or {"action":"get-options"}.
//
// Options is a pointer so get-options can travel without the client inventing
// values it does not mean. A set-options without options is rejected by the
// agent rather than being read as "everything off".
type Command struct {
	Action  string   `json:"action"`
	Options *Options `json:"options,omitempty"`
	// Reader names the reader to watch, for the set-reader action. Empty means
	// every attached reader, which is also how the agent starts.
	Reader string `json:"reader,omitempty"`
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
	// RemoteControl reports whether set-options and set-reader are permitted,
	// so a client can disable its switches instead of offering one that does
	// nothing.
	RemoteControl bool `json:"remote_control"`
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
