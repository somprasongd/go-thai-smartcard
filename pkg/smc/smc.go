package smc

import (
	"errors"
	"log"

	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/util"
)

type Options struct {
	ShowFaceImage bool
	ShowNhsoData  bool
	Broadcast     chan model.Message
}

type smartCard struct {
	Options
}

func NewSmartCard(opt *Options) *smartCard {
	if opt == nil {
		opt = &Options{
			ShowFaceImage: true,
			ShowNhsoData:  false,
			Broadcast:     nil,
		}
	}
	return &smartCard{
		Options: *opt,
	}
}

func (s *smartCard) StartDemon() error {
	// Establish a context
	ctx, err := util.EstablishContext()
	if err != nil {
		return err
	}
	defer util.ReleaseContext(ctx)

	// List available readers
	readers, err := util.ListReaders(ctx)
	if err != nil {
		return err
	}

	log.Printf("Available %d readers:\n", len(readers))
	for i, reader := range readers {
		log.Printf("[%d] %s\n", i, reader)
	}

	if len(readers) == 0 {
		return errors.New("not available readers")
	}

	rs := util.InitReaderStates(readers)
	for {
		log.Println("Waiting for a Card Inserted")
		index, err := util.WaitUntilCardPresent(ctx, rs)
		if err != nil {
			log.Printf("waiting card error %s", err.Error())
			continue
		}

		// Connect to card
		reader := readers[index]
		log.Printf("Connecting to card with %s", reader)
		card, err := util.ConnectCard(ctx, reader)
		if err != nil {
			log.Printf("connecting card error %s", err.Error())
			util.DisconnectCard(card)
			continue
		}

		if s.Broadcast != nil {
			message := model.Message{
				Event: "smc-inserted",
				Payload: map[string]string{
					"message": "Connected to " + reader,
				},
			}
			s.Broadcast <- message
		}

		status, err := card.Status()
		if err != nil {
			log.Printf("get card status error %s", err.Error())
			util.DisconnectCard(card)
			if s.Broadcast != nil {
				message := model.Message{
					Event: "smc-error",
					Payload: map[string]string{
						"message": err.Error(),
					},
				}
				s.Broadcast <- message
				continue
			}
		}

		// log.Printf("\treader: %s\n\tstate: %x\n\tactive protocol: %x\n\tatr: % x\n",
		// 	status.Reader, status.State, status.ActiveProtocol, status.Atr)

		cmd := util.GetResponseCommand(status.Atr)

		data := model.Data{}

		personalReader := NewPersonalReader(card, cmd)
		personalReader.Select()
		data.Personal = personalReader.Read(s.ShowFaceImage)

		if s.ShowNhsoData {
			nhsoReader := NewNhsoReader(card, cmd)
			nhsoReader.Select()
			data.Nhso = nhsoReader.Read()
		}

		// resp, _ := json.Marshal(data)
		// log.Println(string(resp))

		if s.Broadcast != nil {
			message := model.Message{
				Event:   "smc-data",
				Payload: data,
			}
			s.Broadcast <- message
		}

		log.Println("Waiting for a Card Removed")
		util.WaitUntilCardRemove(ctx, rs)
		util.DisconnectCard(card)

		if s.Broadcast != nil {
			message := model.Message{
				Event: "smc-removed",
				Payload: map[string]string{
					"message": "Disonnected from " + reader,
				},
			}
			s.Broadcast <- message
		}
	}
}
