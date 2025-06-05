package main

import (
	"fmt"

	"github.com/djfigs1/arroyo/tunnel"

	"github.com/charmbracelet/huh"
)

func main() {
	var doOffer bool
	huh.NewSelect[bool]().
		Title("Pick a mode.").
		Options(
			huh.NewOption("Offer", true),
			huh.NewOption("Recipient", false),
		).
		Value(&doOffer).
		Run()

	var arroyo *tunnel.Arroyo
	if doOffer {
		invitation := tunnel.NewArroyoInvitation()

		fmt.Println(invitation.InvitationToken())

		var responseToken string
		huh.NewText().Title("Enter Response Token").Value(&responseToken).Run()
		arroyo = invitation.Connect(responseToken)

		fmt.Println("Connected!")
	} else {
		var invitationToken string

		huh.NewText().Title("Enter Invitation Token").Value(&invitationToken).Run()
		response := tunnel.NewArroyoResponse(invitationToken)
		fmt.Println(response.ResponseToken())
		arroyo = response.Connect()
		fmt.Println("Connected!")
	}

	if !doOffer {
		arroyo.Tunnel.ForwardToRemote(18890, "127.0.0.1", 8890)
		// arroyo.Tunnel.ForwardToRemote(18001, "127.0.0.1", 8001)
	}

	select {}
}
