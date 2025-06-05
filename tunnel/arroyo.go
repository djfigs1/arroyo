package tunnel

import (
	"encoding/json"
	"fmt"

	"github.com/pion/webrtc/v4"
)

var clientConfig = webrtc.Configuration{
	ICEServers: []webrtc.ICEServer{
		{
			URLs: []string{"stun:stun.l.google.com:19302"},
		},
	},
}

type ArroyoInvitation struct {
	token      string
	conn       *webrtc.PeerConnection
	resultChan chan *Arroyo
}

func NewArroyoInvitation() *ArroyoInvitation {
	conn, _ := webrtc.NewPeerConnection(clientConfig)
	resultChan := make(chan *Arroyo)

	conn.CreateDataChannel("data", nil)

	offer, err := conn.CreateOffer(nil)
	if err != nil {
		panic(err)
	}

	err = conn.SetLocalDescription(offer)
	if err != nil {
		panic(err)
	}

	// Wait until ICE gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(conn)
	<-gatherComplete

	offerBytes, err := json.Marshal(conn.LocalDescription())
	if err != nil {
		panic(err)
	}

	offerToken := string(offerBytes)

	conn.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateConnected {
			arroyo := newArroyo(conn, true)
			resultChan <- arroyo
		}
	})

	return &ArroyoInvitation{
		conn:       conn,
		token:      offerToken,
		resultChan: resultChan,
	}
}

func (i *ArroyoInvitation) InvitationToken() string {
	return i.token
}

func (i *ArroyoInvitation) Connect(responseToken string) *Arroyo {
	var answer webrtc.SessionDescription
	err := json.Unmarshal([]byte(responseToken), &answer)
	if err != nil {
		panic(err)
	}

	err = i.conn.SetRemoteDescription(answer)
	if err != nil {
		panic(err)
	}

	client := <-i.resultChan

	return client
}

type ArroyoResponse struct {
	responseToken string
	conn          *webrtc.PeerConnection
	resultChan    chan *Arroyo
}

func NewArroyoResponse(invitationToken string) *ArroyoResponse {
	conn, _ := webrtc.NewPeerConnection(clientConfig)
	resultChan := make(chan *Arroyo)

	conn.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateConnected {
			arroyo := newArroyo(conn, false)
			resultChan <- arroyo
		}
	})

	var offer webrtc.SessionDescription
	err := json.Unmarshal([]byte(invitationToken), &offer)
	if err != nil {
		panic(err)
	}

	err = conn.SetRemoteDescription(offer)
	if err != nil {
		panic(err)
	}

	answer, err := conn.CreateAnswer(nil)
	if err != nil {
		panic(err)
	}

	gatherComplete := webrtc.GatheringCompletePromise(conn)
	err = conn.SetLocalDescription(answer)
	if err != nil {
		panic(err)
	}

	<-gatherComplete

	answerJSON, err := json.Marshal(conn.LocalDescription())
	if err != nil {
		panic(err)
	}

	responseToken := string(answerJSON)

	return &ArroyoResponse{
		conn:          conn,
		responseToken: responseToken,
		resultChan:    resultChan,
	}
}

func (r *ArroyoResponse) ResponseToken() string {
	return r.responseToken
}

func (r *ArroyoResponse) Connect() *Arroyo {
	return <-r.resultChan
}

type Arroyo struct {
	conn   *webrtc.PeerConnection
	Tunnel *UDPTunnel
}

func newArroyo(conn *webrtc.PeerConnection, isOffering bool) *Arroyo {
	ready := make(chan bool)
	arroyo := &Arroyo{
		conn: conn,
	}

	conn.OnDataChannel(func(dc *webrtc.DataChannel) {
		fmt.Println("Got data channel! It's called " + dc.Label())
		if dc.Label() == "UDP" {
			arroyo.Tunnel = NewUDPTunnel(dc)
			ready <- true
		}
	})

	if isOffering {
		dc, _ := conn.CreateDataChannel("UDP", nil)
		arroyo.Tunnel = NewUDPTunnel(dc)
	} else {
		<-ready
	}

	return arroyo
}
