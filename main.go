package main

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/google/uuid"
	"github.com/rivo/tview"
)

type port = uint16

type Manager struct {
	RunningServers map[uuid.UUID]*Server
	UsedPorts      map[port]uuid.UUID
	UpdateChan     chan UIUpdate
}

func NewManager(updateChan chan UIUpdate) Manager {
	return Manager{
		RunningServers: make(map[uuid.UUID]*Server),
		UsedPorts:      make(map[port]uuid.UUID),
		UpdateChan:     updateChan,
	}
}

func (m *Manager) SpawnServer(_port *uint16) (*uuid.UUID, error) {
	port := func() uint16 {
		if _port == nil {
			for {
				p := uint16(rand.UintN(2 << 16))
				_, ok := m.UsedPorts[p]
				if !ok {
					return p
				}
			}
		}

		return *_port
	}()

	_, ok := m.UsedPorts[port]
	if ok {
		return nil, fmt.Errorf("port is already in use")
	}

	server := NewServer(port, m.UpdateChan)

	m.RunningServers[server.ID] = &server
	m.UsedPorts[port] = server.ID

	go func() {
		server.Start()

		delete(m.RunningServers, server.ID)
		delete(m.UsedPorts, port)
	}()

	return &server.ID, nil
}

func (m *Manager) List() []*Server {
	return slices.Collect(maps.Values(m.RunningServers))
}

func (m *Manager) StopServer(id uuid.UUID) error {
	server, ok := m.RunningServers[id]
	if !ok {
		return fmt.Errorf("no server with id: %v", id)
	}

	return server.Stop()
}

type UIUpdateType = uint8

const (
	AddServer UIUpdateType = iota
	RemoveServer
	UpdateServer
)

type UIUpdate struct {
	Type         UIUpdateType
	ServerID     ServerID
	ServerPort   port
	ServerStatus ServerStatus
}

type ServerDetails struct {
	ID     ServerID
	Port   port
	Status StatusCode
}

func (ui *UI) Redraw() {
	statusColor := func(status StatusCode) tcell.Color {
		switch status {
		case OK:
			return tcell.ColorGreen
		case STARTING:
			return tcell.ColorYellowGreen
		case SHUTTINGDOWN:
			return tcell.ColorYellow
		case DOWN:
			return tcell.ColorDarkRed
		}

		return tcell.ColorPink
	}

	statusText := func(status StatusCode) string {
		switch status {
		case OK:
			return "UP"
		case STARTING:
			return "STARTING"
		case SHUTTINGDOWN:
			return "SHUTTING DOWN"
		case DOWN:
			return "DOWN"
		}

		return "INVALID"
	}

	ui.table.Clear()

	ui.table.SetBorder(true)

	ui.table.SetCell(0, 1, tview.NewTableCell("port ").SetAlign(tview.AlignLeft))
	ui.table.SetCell(0, 2, tview.NewTableCell("status       ").SetAlign(tview.AlignLeft))

	for i, server := range ui.serverDetails {
		color := statusColor(server.Status)
		statusText := statusText(server.Status)

		ui.table.SetCell(i+1, 1, tview.NewTableCell(fmt.Sprintf("%d", server.Port)).SetBackgroundColor(color))
		ui.table.SetCell(i+1, 2, tview.NewTableCell(statusText).SetBackgroundColor(color))
	}
}

func (ui *UI) GetServerDetails(id ServerID) (*ServerDetails, int) {
	for i, server := range ui.serverDetails {
		if server.ID == id {
			return server, i
		}
	}

	return nil, 0
}

type UI struct {
	serverDetails []*ServerDetails
	table         *tview.Table
}

func main() {
	app := tview.NewApplication()
	table := tview.NewTable()

	ui := &UI{
		serverDetails: []*ServerDetails{},
		table:         table,
	}

	updateChan := make(chan UIUpdate)

	go func() {
		for {
			select {
			case update := <-updateChan:
				app.QueueUpdateDraw(func() {
					switch update.Type {
					case AddServer:
						ui.serverDetails = append(ui.serverDetails, &ServerDetails{
							ID:     update.ServerID,
							Port:   update.ServerPort,
							Status: update.ServerStatus.Code,
						})
					case RemoveServer:
						server, i := ui.GetServerDetails(update.ServerID)
						if server == nil {
							// log error that we could not find the server
							return
						}

						ui.serverDetails = append(ui.serverDetails[:i], ui.serverDetails[i+1:]...)
					case UpdateServer:
						server, _ := ui.GetServerDetails(update.ServerID)
						if server == nil {
							// log error that we could not find the server
							return
						}
						server.Status = update.ServerStatus.Code
					}

					ui.Redraw()
				})
			}
		}
	}()

	m := NewManager(updateChan)
	cli := tview.NewTextArea().SetPlaceholder("enter commands here").SetPlaceholderStyle(tcell.StyleDefault.Foreground(tcell.ColorGrey))
	cli.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event != nil && event.Key() == tcell.KeyEnter {
			handler := func() {
				t := cli.GetText()
				tokens := strings.Split(t, " ")
				if len(tokens) == 0 {
					return
				}

				cmd := tokens[0]

				args := tokens[1:]
				switch cmd {
				case "start":
					if len(args) != 1 {
						// print to a log that there is an invalid amount of args
						return
					}

					_port, err := strconv.ParseUint(args[0], 10, 16)
					if err != nil {
						// print to a log the error
						return
					}

					port := uint16(_port)

					_, err = m.SpawnServer(&port)
					if err != nil {
						// print to a log
						return
					}

				case "stop":
					if len(args) != 1 {
						// print to a log that there is an invalid amount of args
						return
					}

					p := args[0]
					port, err := strconv.ParseUint(p, 10, 16)
					if err != nil {
						// the argument was not a uuid or a port
						return
					}

					var uid *uuid.UUID = nil

					for k, v := range m.RunningServers {
						if v.Port == uint16(port) {
							uid = &k
							break
						}
					}

					if uid == nil {
						// log there is no server on that port
						return
					}

					err = m.StopServer(*uid)
					if err != nil {
						// log that there was an error stopping the server
						return
					}
				case "exit":
					app.Stop()
				}
			}

			return func() *tcell.EventKey {
				handler()
				cli.SetText("", true)
				return nil
			}()
		}
		return event
	})
	flex := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(table, 0, 1, false).AddItem(cli, 1, 1, false)

	ui.Redraw()

	if err := app.SetRoot(flex, true).SetFocus(cli).Run(); err != nil {
		panic(err)
	}
}
