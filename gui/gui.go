package gui

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/djfigs1/arroyo/tunnel"
)

type Mode int

const (
	ModeOffer Mode = iota
	ModeRecipient
)

type ArroyoApp struct {
	fyneApp    fyne.App
	window     fyne.Window
	mode       Mode
	state      string
	arroyo     *tunnel.Arroyo
	invitation *tunnel.ArroyoInvitation
	response   *tunnel.ArroyoResponse
	statusLbl  *widget.Label
	localPort  *widget.Entry
	remoteAddr *widget.Entry
	remotePort *widget.Entry
	startBtn   *widget.Button
	stopBtn    *widget.Button
	statusTab  *container.Scroll
	stateCh    chan string
}

func Run() error {
	fyneApp := app.New()
	window := fyneApp.NewWindow("Arroyo")
	window.Resize(fyne.NewSize(500, 450))

	app := &ArroyoApp{
		fyneApp: fyneApp,
		window:  window,
		state:   "disconnected",
		stateCh: make(chan string, 1),
	}

	app.buildUI()
	app.window.SetOnClosed(func() {
		fyneApp.Quit()
	})

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		fyneApp.Quit()
	}()

	// Handle state updates on the main thread
	go func() {
		for state := range app.stateCh {
			fyne.Do(func() {
				app.state = state
				app.updateUI()
			})
		}
	}()

	app.window.ShowAndRun()

	return nil
}

func (app *ArroyoApp) setState(state string) {
	app.stateCh <- state
}

func (app *ArroyoApp) buildUI() {
	// Mode radio group
	modeGroup := widget.NewRadioGroup([]string{"Offer", "Recipient"}, func(s string) {
		if app.state != "disconnected" && app.state != "error" {
			return
		}
		if s == "Offer" {
			app.mode = ModeOffer
		} else {
			app.mode = ModeRecipient
		}
		app.resetState()
		app.setState("disconnected")
	})
	modeGroup.Horizontal = true

	// Status
	app.statusLbl = widget.NewLabel("Status: Disconnected")

	// Settings entries
	app.localPort = widget.NewEntry()
	app.localPort.SetText("18890")
	app.localPort.SetPlaceHolder("Local port")

	app.remoteAddr = widget.NewEntry()
	app.remoteAddr.SetText("127.0.0.1")
	app.remoteAddr.SetPlaceHolder("Remote address")

	app.remotePort = widget.NewEntry()
	app.remotePort.SetText("8890")
	app.remotePort.SetPlaceHolder("Remote port")

	settingsForm := widget.NewForm(
		widget.NewFormItem("Local Port", app.localPort),
		widget.NewFormItem("Remote Address", app.remoteAddr),
		widget.NewFormItem("Remote Port", app.remotePort),
	)

	// Content area
	contentArea := widget.NewLabel("Select a mode and press Start.")
	app.statusTab = container.NewScroll(contentArea)

	tabContainer := container.NewAppTabs(
		container.NewTabItem("Status", app.statusTab),
		container.NewTabItem("Settings", container.NewBorder(nil, nil, nil, nil, container.NewScroll(settingsForm))),
	)

	// Buttons
	app.startBtn = widget.NewButton("Start", func() {
		app.onStart()
	})
	app.startBtn.Importance = widget.HighImportance

	app.stopBtn = widget.NewButton("Stop", func() {
		app.onStop()
	})
	app.stopBtn.Disable()

	// Assemble
	header := container.NewVBox(modeGroup, app.statusLbl)
	actions := container.NewHBox(app.startBtn, app.stopBtn)
	content := container.NewBorder(header, actions, nil, nil, tabContainer)

	app.window.SetContent(content)
}

func (app *ArroyoApp) updateUI() {
	var content fyne.CanvasObject

	switch app.state {
	case "disconnected":
		content = widget.NewLabel("Select a mode and press Start.")

	case "generating":
		content = widget.NewLabel("Generating token...")

	case "waiting-invitation":
		token := ""
		if app.invitation != nil {
			token = app.invitation.InvitationToken()
		}

		copyBtn := widget.NewButton("Copy Token", func() {
			if token != "" {
				app.fyneApp.Clipboard().SetContent(token)
			}
		})
		if token == "" {
			copyBtn.Disable()
		}

		showBtn := widget.NewButton("Show Token", func() {
			if token != "" {
				dialog.ShowInformation("Invitation Token", token, app.window)
			}
		})

		responseEntry := widget.NewEntry()
		responseEntry.SetPlaceHolder("Paste response token here")

		connectBtn := widget.NewButton("Connect", func() {
			if app.invitation == nil {
				return
			}
			tok := responseEntry.Text
			if tok == "" {
				dialog.ShowError(fmt.Errorf("response token is required"), app.window)
				return
			}
			app.setState("connecting")
			// Connect in background, then update state on main thread
			go func() {
				arroyo := app.invitation.Connect(tok)
				app.arroyo = arroyo
				app.setState("connected")
			}()
		})

		content = container.NewVBox(
			widget.NewLabel("Share this invitation token with the recipient:"),
			showBtn,
			container.NewHBox(copyBtn),
			widget.NewSeparator(),
			widget.NewLabel("Paste the response token below:"),
			responseEntry,
			connectBtn,
		)

	case "waiting-response":
		tok := ""
		if app.response != nil {
			tok = app.response.ResponseToken()
		}

		responseLbl := widget.NewLabel(tok)
		copyBtn := widget.NewButton("Copy Token", func() {
			if tok != "" {
				app.fyneApp.Clipboard().SetContent(tok)
			}
		})
		if tok == "" {
			copyBtn.Disable()
		}

		showBtn := widget.NewButton("Show Token", func() {
			if tok != "" {
				dialog.ShowInformation("Response Token", tok, app.window)
			}
		})

		content = container.NewVBox(
			widget.NewLabel("Share this response token with the offerer:"),
			responseLbl,
			showBtn,
			container.NewHBox(copyBtn),
			widget.NewLabel("Waiting for connection..."),
		)

	case "connecting":
		content = widget.NewLabel("Connecting...")

	case "connected":
		content = widget.NewLabel("Connected! Press Start to begin tunneling.")

	case "tunneling":
		content = widget.NewLabel("Tunneling in progress...")

	case "error":
		content = widget.NewLabel("An error occurred. Press Stop to reset.")
	}

	app.statusTab.Content = content
	app.statusTab.Refresh()

	// Enable/disable buttons based on state
	switch app.state {
	case "disconnected", "waiting-response":
		app.startBtn.Enable()
		app.stopBtn.Disable()
	case "waiting-invitation":
		app.startBtn.Disable()
		app.stopBtn.Disable()
	case "connected":
		app.startBtn.Enable()
		app.stopBtn.Enable()
	case "tunneling":
		app.startBtn.Disable()
		app.stopBtn.Enable()
	case "connecting", "generating":
		app.startBtn.Disable()
		app.stopBtn.Disable()
	case "error":
		app.startBtn.Enable()
		app.stopBtn.Disable()
	}

	// Update status label
	switch app.state {
	case "disconnected":
		app.statusLbl.SetText("Status: Disconnected")
	case "generating":
		app.statusLbl.SetText("Status: Generating token...")
	case "waiting-invitation":
		app.statusLbl.SetText("Status: Waiting for response token")
	case "waiting-response":
		app.statusLbl.SetText("Status: Waiting for connection")
	case "connecting":
		app.statusLbl.SetText("Status: Connecting...")
	case "connected":
		app.statusLbl.SetText("Status: Connected")
	case "tunneling":
		app.statusLbl.SetText("Status: Tunneling")
	case "error":
		app.statusLbl.SetText("Status: Error")
	}
}

func (app *ArroyoApp) resetState() {
	app.state = "disconnected"
	app.arroyo = nil
	app.invitation = nil
	app.response = nil
}

func (app *ArroyoApp) onStart() {
	switch app.state {
	case "disconnected":
		if app.mode == ModeOffer {
			app.setState("generating")
			go func() {
				app.invitation = tunnel.NewArroyoInvitation()
				app.setState("waiting-invitation")
			}()
		} else {
			dialog.ShowEntryDialog("Invitation Token", "Paste the invitation token from the offerer:", func(token string) {
				if token == "" {
					return
				}
				app.setState("generating")
				go func() {
					app.response = tunnel.NewArroyoResponse(token)
					app.setState("waiting-response")
				}()
			}, app.window)
		}

	case "connected":
		app.startTunnel()
	}
}

func (app *ArroyoApp) startTunnel() {
	if app.arroyo == nil {
		return
	}

	localPortStr := app.localPort.Text
	remoteAddr := app.remoteAddr.Text
	remotePortStr := app.remotePort.Text

	if localPortStr == "" || remoteAddr == "" || remotePortStr == "" {
		dialog.ShowError(fmt.Errorf("all fields are required"), app.window)
		return
	}

	localPort, err := strconv.ParseUint(localPortStr, 10, 16)
	if err != nil {
		dialog.ShowError(err, app.window)
		return
	}

	remotePort, err := strconv.ParseUint(remotePortStr, 10, 16)
	if err != nil {
		dialog.ShowError(err, app.window)
		return
	}

	app.arroyo.Tunnel.ForwardToRemote(uint16(localPort), remoteAddr, uint16(remotePort))
	app.setState("tunneling")
}

func (app *ArroyoApp) onStop() {
	app.resetState()
	app.setState("disconnected")
}
