package hassigo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"
)

type Hass struct {
	EventManager EventManager
	token        string
	uri          string
}

func New(URI string, token string, insecureSkipVerify bool) (*Hass, error) {
	hass := Hass{
		EventManager: EventManager{},
		token:        token,
		uri:          URI,
	}

	err := hass.EventManager.start(context.Background(), URI, token, insecureSkipVerify)

	if err != nil {
		return nil, err
	}

	return &hass, nil
}

type Entity struct {
	EntityID    string                 `json:"entity_id,omitempty"`
	State       string                 `json:"state,omitempty"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
	LastChanged time.Time              `json:"last_changed,omitempty"`
	LastUpdates time.Time              `json:"last_updates,omitempty"`
}

func (ha *Hass) GetState(entityID string) (Entity, error) {
	bearer := fmt.Sprintf("Bearer %s", ha.token)

	uri := url.URL{Scheme: "http", Host: ha.uri, Path: fmt.Sprintf("/api/states/%s", entityID)}

	req, err := http.NewRequest("GET", uri.String(), nil)
	req.Header.Add("Authorization", bearer)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return Entity{}, fmt.Errorf("error on http response: %v", err)
	}

	body, _ := ioutil.ReadAll(resp.Body)
	//log.Println(string([]byte(body)))

	entity := Entity{}

	err = json.Unmarshal(body, &entity)

	if err != nil {
		return Entity{}, fmt.Errorf("unable to unmarshal entity: %v", err)
	}

	return entity, nil

}

func (ha *Hass) CallService(domain string, service string, serviceData interface{}) error {
	bearer := fmt.Sprintf("Bearer %s", ha.token)

	uri := url.URL{Scheme: "http", Host: ha.uri, Path: fmt.Sprintf("/api/services/%s/%s", domain, service)}

	req, err := http.NewRequest("POST", uri.String(), nil)
	req.Header.Add("Authorization", bearer)
	req.Header.Add("Content-Type", "application/json")

	reqBody, err := json.Marshal(serviceData)

	if err != nil {
		return fmt.Errorf("could not marshal request body: %v", err)
	}

	req.Body = ioutil.NopCloser(bytes.NewReader(reqBody))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error on http response: %v", err)
	}

	respBody, _ := ioutil.ReadAll(resp.Body)
	//log.Println(string(respBody))

	_ = respBody

	//entity := Entity{}

	//err = json.Unmarshal(body, &entity)

	//if err != nil {
	//	return Entity{}, fmt.Errorf("unable to unmarshal entity: %v", err)
	//}

	return nil

}
