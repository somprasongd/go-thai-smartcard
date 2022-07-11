package smc

import (
	"errors"
	"log"
	"time"

	"github.com/ebfe/scard"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/util"
)

type SmartCardConfig struct {
	ShowFaceImage bool
	ShowNhsoData  bool
}

type smartCard struct {
	SmartCardConfig
}

func NewSmartCard(cfg *SmartCardConfig) *smartCard {
	if cfg == nil {
		cfg = &SmartCardConfig{
			ShowFaceImage: true,
			ShowNhsoData:  false,
		}
	}
	return &smartCard{
		SmartCardConfig: *cfg,
	}
}

func (s *smartCard) ListReaders() ([]string, error) {
	// Establish a context
	ctx, err := util.EstablishContext()
	if err != nil {
		return nil, err
	}
	defer util.ReleaseContext(ctx)

	// List available readers
	return util.ListReaders(ctx)
}

func (s *smartCard) Read(readerName *string) (*model.Data, error) {
	readers := []string{}

	if readerName == nil {
		r, err := s.ListReaders()
		if err != nil {
			return nil, err
		}
		readers = r
	} else {
		readers = append(readers, *readerName)
	}

	if len(readers) == 0 {
		return nil, errors.New("not available readers")
	}

	// Establish a context
	ctx, err := util.EstablishContext()
	if err != nil {
		return nil, err
	}
	defer util.ReleaseContext(ctx)

	rs := util.InitReaderStates(readers)

	log.Println("Waiting for a Card Inserted")
	index, err := util.WaitUntilCardPresent(ctx, rs)
	if err != nil {
		return nil, err
	}

	reader := readers[index]
	card, data, err := s.readCard(ctx, reader)
	defer util.DisconnectCard(card)

	if err != nil {
		return nil, err
	}

	return data, nil
}

func (s *smartCard) readCard(ctx *scard.Context, reader string) (*scard.Card, *model.Data, error) {
	log.Printf("Connecting to card with %s", reader)
	card, err := util.ConnectCard(ctx, reader)
	if err != nil {
		log.Printf("connecting card error %s", err.Error())
		return card, nil, err
	}

	status, err := card.Status()
	if err != nil {
		log.Printf("get card status error %s", err.Error())
		return card, nil, err
	}

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
	return card, &data, nil
}

func (s *smartCard) StartDaemon(broadcast chan model.Message) error {
	// Establish a context
	ctx, err := util.EstablishContext()
	if err != nil {
		return err
	}
	defer util.ReleaseContext(ctx)

	chWaitReaders := make(chan []string)
	go func(chWaitReaders chan []string) {
		for {
			// List available readers
			readers, err := util.ListReaders(ctx)
			if err != nil {
				if broadcast != nil {
					message := model.Message{
						Event: "smc-error",
						Payload: map[string]string{
							"message": err.Error(),
						},
					}
					broadcast <- message
				}
				log.Println("Cannot find a smart card reader, Wait 2 seconds")
				time.Sleep(2 * time.Second)
				continue
			}

			log.Printf("Available %d readers:\n", len(readers))
			for i, reader := range readers {
				log.Printf("[%d] %s\n", i, reader)
			}

			if len(readers) == 0 {
				if broadcast != nil {
					message := model.Message{
						Event: "smc-error",
						Payload: map[string]string{
							"message": "not available readers",
						},
					}
					broadcast <- message
				}
				log.Println("Cannot find a smart card reader, Wait 2 seconds")
				time.Sleep(2 * time.Second)
				continue
			}

			chWaitReaders <- readers
			break
		}
	}(chWaitReaders)
	readers := <-chWaitReaders

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

		if broadcast != nil {
			message := model.Message{
				Event: "smc-inserted",
				Payload: map[string]string{
					"message": "Connected to " + reader,
				},
			}
			broadcast <- message
		}

		card, data, err := s.readCard(ctx, reader)

		if err != nil {
			util.DisconnectCard(card)
			if broadcast != nil {
				message := model.Message{
					Event: "smc-error",
					Payload: map[string]string{
						"message": err.Error(),
					},
				}
				broadcast <- message
			}
			continue
		}

		if broadcast != nil {
			message := model.Message{
				Event:   "smc-data",
				Payload: data,
			}
			broadcast <- message
		}

		log.Println("Waiting for a Card Removed")
		util.WaitUntilCardRemove(ctx, rs)

		if broadcast != nil {
			message := model.Message{
				Event: "smc-removed",
				Payload: map[string]string{
					"message": "Disonnected from " + reader,
				},
			}
			broadcast <- message
		}

		util.DisconnectCard(card)
	}
}
