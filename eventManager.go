package hassigo

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type EventManager struct {
	subscriptions map[string]subscription
	chSubUpdate   chan subscriptionChanged
	connected     bool
	mux           sync.RWMutex
}

type subscription struct {
	entityID   string
	attributes map[string][]chan Event
}

type Event struct {
	EventType string       `json:"event_type"`
	Data      EventData    `json:"data"`
	Origin    string       `json:"origin"`
	TimeFired time.Time    `json:"time_fired"`
	Context   EventContext `json:"context"`
}

type EventData struct {
	EntityID  string `json:"entity_id,omitempty"`
	Attribute string
	OldState  EventState `json:"old_state"`
	NewState  EventState `json:"new_state"`
}

type EventState struct {
	EntityID    string                 `json:"entity_id"`
	State       string                 `json:"state"`
	Attributes  map[string]interface{} `json:"attributes"`
	LastChanged time.Time              `json:"last_changed"`
	LastUpdate  time.Time              `json:"last_update"`
	Context     EventContext           `json:"context"`
}

type EventContext struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id"`
	UserID   string `json:"user_id"`
}

type subscribeMessage struct {
	ID        int    `json:"id"`
	Type      string `json:"type"`
	EventType string `json:"event_type"`
}

func (em *EventManager) start(ctx context.Context, URI string, token string, insecureSkipVerify bool) error {

	type basicMessage struct {
		Type string `json:"type,omitempty"`
	}

	c := em.connect(ctx, URI, token, insecureSkipVerify)

	go func() {

		defer c.Close()
		for {
			_, message, err := c.ReadMessage()

			if err != nil {
				log.Printf("Websocket error reading: %v", err)
				log.Printf("Reconnecting")

				c = em.connect(ctx, URI, token, insecureSkipVerify)
				em.resubscribe()

				continue
			}

			messageEnvelope := struct {
				Type string `json:"type,omitempty"`
			}{}

			err = json.Unmarshal(message, &messageEnvelope)

			if err != nil {
				log.Printf("Unable to unmarshall message type: %v", err)
				fmt.Println(string(message))
				return
			}

			//log.Printf("Message type: %s", messageEnvelope.Type)

			switch messageEnvelope.Type {
			case "auth_required":

				authMessage := struct {
					Type        string `json:"type,omitempty"`
					AccessToken string `json:"access_token,omitempty"`
				}{
					Type:        "auth",
					AccessToken: token,
				}

				err = em.Write(c, authMessage)

				if err != nil {
					log.Printf("Could not send auth: %v", err)
					return
				}

			case "auth_ok":
				log.Print("Websocket authentication complete")

				//Testing
				subMessage := subscribeMessage{
					ID:        19,
					Type:      "subscribe_events",
					EventType: "state_changed",
				}

				err := em.Write(c, subMessage)

				if err != nil {
					log.Printf("Could not subscribe to events: %v", err)
					return
				}

			case "auth_invalid":
				log.Print("Websocket authentication failed")
				return

			case "result":
				/*result := {
					ID int
					Type string
					Success bool
				}{}*/

				//log.Print(string(message))

			case "event":

				eventMessage := struct {
					ID    int    `json:"id,omitempty"`
					Type  string `json:"type,omitempty"`
					Event Event  `json:"event,omitempty"`
				}{}

				err := json.Unmarshal(message, &eventMessage)

				if err != nil {
					log.Printf("Unable to unmarshall event: %v", err)
				}

				err = em.evaluateEvent(eventMessage.Event)

				if err != nil {
					log.Printf("Could not evaluate event: %v", err)
				}
			}

		}
	}()

	em.subscriptions = make(map[string]subscription)
	em.chSubUpdate = make(chan subscriptionChanged, 1000)
	go em.manageSubscriptions(ctx)

	return nil

}

func (em *EventManager) connect(ctx context.Context, URI string, token string, insecureSkipVerify bool) *websocket.Conn {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			u := url.URL{Scheme: "ws", Host: URI, Path: "/api/websocket"}
			log.Printf("connecting to %s", u.String())

			c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)

			if err != nil {
				log.Printf("websocket dial error: %v", err)
				time.Sleep(2 * time.Second)
				continue
			}

			return c
		}

	}
}

func (em *EventManager) resubscribe() error {
	return nil
}

func (em *EventManager) Connected() bool {
	em.mux.RLock()
	defer em.mux.RUnlock()
	return em.connected
}

func (em *EventManager) evaluateEvent(event Event) error {
	oldState := event.Data.OldState
	newState := event.Data.NewState

	if sub, ok := em.subscriptions[event.Data.EntityID]; ok {

		for attr, listeners := range sub.attributes {
			switch attr {
			case "":
				if oldState.State != newState.State {
					event.Data.Attribute = ""
					notifyListeners(event, listeners)
				}
			case "*":
				if oldState.State != newState.State {
					event.Data.Attribute = ""
					notifyListeners(event, listeners)
				}

				for oldKey, oldValue := range oldState.Attributes {
					if newValue, ok := newState.Attributes[oldKey]; ok {
						if oldValue != newValue {
							event.Data.Attribute = oldKey
							notifyListeners(event, listeners)

						}
					}
				}
			default:
				if old, ok := oldState.Attributes[attr]; ok {
					if new, ok := newState.Attributes[attr]; ok {
						if old != new {
							event.Data.Attribute = attr
							notifyListeners(event, listeners)

						}
					}
				}
			}

		}

	}

	return nil

}

func notifyListeners(event Event, listeners []chan Event) error {
	for _, ch := range listeners {
		select {
		case ch <- event:
		default:
			return fmt.Errorf("Could not write event to channel")
		}
	}
	return nil
}

type subscriptionChanged struct {
	subscribe bool
	entityID  string
	attribute string
	ch        chan Event
}

func (em *EventManager) manageSubscriptions(ctx context.Context) {

	log.Printf("Starting Event Subscription Manager")

	for {
		select {
		case <-ctx.Done():
			return
		case subChange := <-em.chSubUpdate:
			log.Printf("Processing subscription change")

			if sub, ok := em.subscriptions[subChange.entityID]; ok {
				if subscribers, ok := sub.attributes[subChange.attribute]; ok {
					subFound := false

					for i, subscriber := range subscribers {
						if subChange.ch == subscriber {
							if !subChange.subscribe {

								subscribers[i] = subscribers[len(subscribers)-1] // Copy last element to index i.
								subscribers[len(subscribers)-1] = nil            // Erase last element (write zero value).
								subscribers = subscribers[:len(subscribers)-1]

								fmt.Println("HERE1")
								fmt.Println(len(subscribers))
								if len(subscribers) == 0 {
									fmt.Println("HERE2")
									delete(sub.attributes, subChange.attribute)
								}
							}
							subFound = true
						}
					}

					if !subFound {
						sub.attributes[subChange.attribute] = append(sub.attributes[subChange.attribute], subChange.ch)
					}
				} else {
					// Add the atribute, and add the channel as the only member
					sub.attributes[subChange.attribute] = []chan Event{subChange.ch}
				}

				em.subscriptions[subChange.entityID] = sub

			} else {
				subscribers := make(map[string][]chan Event)

				subscribers[subChange.attribute] = []chan Event{subChange.ch}

				em.subscriptions[subChange.entityID] = subscription{
					entityID:   subChange.entityID,
					attributes: subscribers,
				}
			}
			//log.Printf("%+v", em.subscriptions)
		}
	}
}

func (em *EventManager) Subscribe(entityID string, attribute string, ch chan Event) {
	em.chSubUpdate <- subscriptionChanged{
		subscribe: true,
		entityID:  entityID,
		attribute: attribute,
		ch:        ch,
	}
}

func (em *EventManager) Unsubscribe(entityID string, attribute string, ch chan Event) {
	em.chSubUpdate <- subscriptionChanged{
		subscribe: false,
		entityID:  entityID,
		attribute: attribute,
		ch:        ch,
	}
}

func (em *EventManager) Write(c *websocket.Conn, message interface{}) error {

	messageJSON, err := json.Marshal(message)

	if err != nil {
		log.Printf("Unable to marshall message: %v", err)
	}

	err = c.WriteMessage(websocket.TextMessage, messageJSON)

	if err != nil {
		return fmt.Errorf("Could not write to websocket: %v", err)
	}

	return nil

}
