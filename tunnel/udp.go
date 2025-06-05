package tunnel

import (
	"fmt"
	"net"

	"github.com/djfigs1/arroyo/protocol"
	"google.golang.org/protobuf/proto"

	"github.com/pion/webrtc/v4"
)

type UDPTunnel struct {
	dataChannel             *webrtc.DataChannel
	forwarderMap            map[int]*net.UDPConn
	remoteClientConnections map[uint32]*net.UDPConn
	localClients            map[uint32]*UDPClient
	localClientIds          map[string]uint32
	clientCounter           uint32
}

type UDPClient struct {
	ClientId   uint32
	Connection *net.UDPConn
	Addr       *net.UDPAddr
}

func NewUDPTunnel(channel *webrtc.DataChannel) *UDPTunnel {
	tunnel := &UDPTunnel{
		dataChannel:             channel,
		forwarderMap:            make(map[int]*net.UDPConn),
		remoteClientConnections: make(map[uint32]*net.UDPConn),
		localClients:            make(map[uint32]*UDPClient),
		localClientIds:          make(map[string]uint32, 0),
		clientCounter:           0,
	}

	channel.OnMessage(tunnel.onDataChannelMessage)
	channel.OnClose(tunnel.Stop)

	return tunnel
}

func (u *UDPTunnel) ForwardToRemote(localPort uint16, remoteAddr string, remotePort uint16) {
	go u.createAndServeUdpForwarder(int(localPort), remoteAddr, remotePort)
}

func (u *UDPTunnel) onDataChannelMessage(msg webrtc.DataChannelMessage) {
	var pkt protocol.UDPTunnelPacket
	if err := proto.Unmarshal(msg.Data, &pkt); err != nil {
		panic(err)
	}

	switch payload := pkt.Payload.(type) {
	case *protocol.UDPTunnelPacket_NewClient:
		u.createUdpClientHandler(payload.NewClient.ClientId, payload.NewClient.RemoteAddr, int(payload.NewClient.RemotePort))
	case *protocol.UDPTunnelPacket_Data:
		u.deliverClientPacket(payload.Data.ClientId, payload.Data.IsResponse, payload.Data.Data)
	default:
		fmt.Println("Unknown payload")
	}

}

func (u *UDPTunnel) createUdpClientHandler(clientId uint32, destinationAddr string, destinationPort int) {
	ipAddr, err := net.ResolveIPAddr("ip", destinationAddr)
	if err != nil {
		panic(err)
	}

	serverAddr := net.UDPAddr{
		IP:   ipAddr.IP,
		Port: int(destinationPort),
	}

	conn, err := net.DialUDP("udp", nil, &serverAddr)
	if err != nil {
		panic(err)
	}

	u.remoteClientConnections[clientId] = conn

	fmt.Printf("Got new client for %d heading to %s:%d\n", clientId, destinationAddr, destinationPort)

	go u.serveClient(clientId, conn)
}

func (u *UDPTunnel) serveClient(clientId uint32, conn *net.UDPConn) {
	defer conn.Close()

	pktBuf := make([]byte, 65536)
	for {
		select {
		default:
			n, _, err := conn.ReadFromUDP(pktBuf)
			if err != nil {
				fmt.Println("Couldn't read from UDP", err)
				break
			}

			data := pktBuf[:n]
			u.returnClientPacket(clientId, data)
		}
	}
}

func (f *UDPTunnel) createAndServeUdpForwarder(listeningPort int, remoteAddr string, remotePort uint16) {
	serverAddr := net.UDPAddr{
		Port: listeningPort,
	}

	conn, err := net.ListenUDP("udp", &serverAddr)
	if err != nil {
		panic(err)
	}

	f.forwarderMap[listeningPort] = conn

	pktBuf := make([]byte, 65536)

	fmt.Println("listening")

	for {
		select {
		default:
			n, clientAddr, err := conn.ReadFromUDP(pktBuf)
			if err != nil {
				continue
			}

			data := pktBuf[:n]
			f.handleIncomingUDPPacket(conn, clientAddr, remoteAddr, remotePort, data)
		}
	}
}

func (u *UDPTunnel) createClient(conn *net.UDPConn, clientStr string, clientAddr *net.UDPAddr, remoteAddr string, remotePort uint16) *UDPClient {
	newClientId := u.clientCounter
	u.clientCounter++

	client := &UDPClient{
		ClientId:   newClientId,
		Connection: conn,
		Addr:       clientAddr,
	}

	u.localClientIds[clientStr] = newClientId
	u.localClients[newClientId] = client

	pkt := &protocol.UDPTunnelPacket{
		Payload: &protocol.UDPTunnelPacket_NewClient{
			NewClient: &protocol.UDPTunnelNewClient{
				ClientId:   newClientId,
				RemoteAddr: remoteAddr,
				RemotePort: uint32(remotePort),
			},
		},
	}

	pktData, err := proto.Marshal(pkt)

	if err != nil {
		panic(err)
	}

	u.dataChannel.Send(pktData)

	return client
}

func (u *UDPTunnel) handleIncomingUDPPacket(conn *net.UDPConn, clientAddr *net.UDPAddr, remoteAddr string, remotePort uint16, data []byte) {
	clientStr := clientAddr.String()
	clientId, foundClient := u.localClientIds[clientStr]
	var client *UDPClient
	if !foundClient {
		client = u.createClient(conn, clientStr, clientAddr, remoteAddr, remotePort)
	} else {
		client = u.localClients[clientId]
	}

	pkt := &protocol.UDPTunnelPacket{
		Payload: &protocol.UDPTunnelPacket_Data{
			Data: &protocol.UDPTunnelData{
				ClientId:   client.ClientId,
				Data:       data,
				IsResponse: false,
			},
		},
	}

	pktBytes, err := proto.Marshal(pkt)

	if err != nil {
		panic(err)
	}

	u.dataChannel.Send(pktBytes)
}

func (u *UDPTunnel) deliverClientPacket(clientId uint32, is_response bool, data []byte) {
	if is_response {
		client, ok := u.localClients[clientId]
		if ok {
			client.Connection.WriteToUDP(data, client.Addr)
		} else {
			fmt.Println("Unknown client to return to")
		}
	} else {
		conn, ok := u.remoteClientConnections[clientId]
		if ok {
			conn.Write(data)
		} else {
			fmt.Println("Unknown client to deliver to")
		}
	}
}

func (u *UDPTunnel) returnClientPacket(clientId uint32, data []byte) {
	pkt := &protocol.UDPTunnelPacket{
		Payload: &protocol.UDPTunnelPacket_Data{
			Data: &protocol.UDPTunnelData{
				ClientId:   clientId,
				Data:       data,
				IsResponse: true,
			},
		},
	}

	pktBytes, err := proto.Marshal(pkt)

	if err != nil {
		panic(err)
	}

	u.dataChannel.Send(pktBytes)
}

func (u *UDPTunnel) Stop() {
	fmt.Println("Stopping UDP Tunnel")
}
