package timeline

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/pkg/errors"

	"golang.org/x/net/websocket"
)

var rtmStartURL = "https://slack.com/api/rtm.start"

var slackAPIEndpoint = "https://slack.com/api/"

var origin = "http://localhost"

type rtmStartResponse struct {
	OK    bool   `json:"ok"`
	URL   string `json:"url"`
	Error string `json:"error"`
}

type deletedEvent struct {
	ChannelID string       `json:"channel"`
	Message   slackMessage `json:"previous_message"`
}

type slackMessage struct {
	Raw       string `json:"-"`
	Type      string `json:"type"`
	UserID    string `json:"user"`
	Text      string `json:"text"`
	ChannelID string `json:"channel"`
	TimeStamp string `json:"ts"`
	SubType   string `json:"subtype"`
}

func (m *slackMessage) ToKey() string {
	return fmt.Sprintf("%s-%s", m.ChannelID, m.TimeStamp)
}

type userListResponse struct {
	OK    bool   `json:"ok"`
	User  user   `json:"user"`
	Error string `json:"error"`
}

type user struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Profile profile `json:"profile"`
}

type profile struct {
	ImageURL string `json:"image_48"`
}

type slackClient struct {
	Token string
}

func (cli *slackClient) connectToRTM() (*websocket.Conn, error) {
	v := url.Values{
		"token": {cli.Token},
	}
	resp, e := http.Get(rtmStartURL + "?" + v.Encode())
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	byteArray, e := ioutil.ReadAll(resp.Body)
	if e != nil {
		return nil, e
	}
	res := rtmStartResponse{}
	e = json.Unmarshal(byteArray, &res)
	if e != nil {
		return nil, e
	}
	if !res.OK {
		return nil, fmt.Errorf(res.Error)
	}
	ws, e := websocket.Dial(res.URL, "", origin)
	if e != nil {
		return nil, e
	}
	return ws, nil
}

func receive(ws *websocket.Conn) ([]byte, error) {
	var msg = make([]byte, 4096)
	n, e := ws.Read(msg)
	if e != nil {
		return nil, e
	}
	return msg[:n], nil
}

func (cli *slackClient) polling(
	messageChan, deletedMessageChan chan *slackMessage,
	warnChan, errorChan chan error,
) {
	ws, e := cli.connectToRTM()
	if e != nil {
		errorChan <- e
		return
	}
	defer ws.Close()
	prev := []byte{}
	for {
		received, e := receive(ws)
		if e != nil {
			errorChan <- e
		}
		msg := append(prev, received...)
		message := slackMessage{
			Raw: string(msg),
		}
		err := json.Unmarshal(msg, &message)

		if err != nil {
			warnChan <- errors.Wrap(err, fmt.Sprintf("failed to unmarshal. json: '%s'", msg))
			prev = msg
			continue
		}
		prev = []byte{}

		switch message.SubType {
		case "":
			messageChan <- &message
		case "message_deleted":
			d := deletedEvent{}
			e := json.Unmarshal([]byte(msg), &d)
			if e != nil {
				warnChan <- errors.Wrap(err, fmt.Sprintf("failed to unmarshal to deleted event. json: '%s'", msg))
				continue
			}
			d.Message.ChannelID = d.ChannelID
			deletedMessageChan <- &d.Message
		}
	}
}

func (cli *slackClient) postMessage(channelID, text, userName, iconURL string) ([]byte, error) {
	res, e := http.PostForm(slackAPIEndpoint+"chat.postMessage", url.Values{
		"token":      {cli.Token},
		"channel":    {channelID},
		"text":       {text},
		"username":   {userName},
		"as_user":    {"false"},
		"icon_url":   {iconURL},
		"link_names": {"0"},
	})
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	byteArray, e := ioutil.ReadAll(res.Body)
	if e != nil {
		return nil, e
	}
	return byteArray, nil
}

func (cli *slackClient) getUser(userID string) (*user, error) {
	res, e := http.PostForm(slackAPIEndpoint+"users.info", url.Values{
		"token": {cli.Token},
		"user":  {userID},
	})
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	b, e := ioutil.ReadAll(res.Body)
	if e != nil {
		return nil, e
	}
	r := userListResponse{}
	e = json.Unmarshal(b, &r)
	if e != nil {
		return nil, e
	}
	if !r.OK {
		return nil, fmt.Errorf(r.Error)
	}
	u := r.User
	return &u, nil
}

func (cli *slackClient) deleteMessage(ts, channel string) ([]byte, error) {
	res, e := http.PostForm(slackAPIEndpoint+"chat.delete", url.Values{
		"token":   {cli.Token},
		"ts":      {ts},
		"channel": {channel},
	})
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	byteArray, e := ioutil.ReadAll(res.Body)
	if e != nil {
		return nil, e
	}
	return byteArray, nil
}
