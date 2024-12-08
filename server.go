package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type StatusCode = uint8

type ServerID = uuid.UUID

const (
	OK StatusCode = iota
	STARTING
	SHUTTINGDOWN
	DOWN
)

type LogEntry struct{}

type ServerStatus struct {
	Code StatusCode

	UntilHealthCheck *uint8
}

type Server struct {
	ID         ServerID
	Port       port
	Status     ServerStatus
	server     *http.Server
	log        LogEntry
	updateChan chan UIUpdate
}

func (s *Server) ScheduleHealthCheck(seconds uint8) {
	if s.Status.UntilHealthCheck != nil {
		return
	}
	s.Status.UntilHealthCheck = &seconds
	go func() {
		for seconds > 0 {
			time.Sleep(1 * time.Second)
			seconds--
			s.updateChan <- UIUpdate{
				Type:         UpdateServer,
				ServerID:     s.ID,
				ServerPort:   s.Port,
				ServerStatus: s.Status,
			}
		}

		s.Status.UntilHealthCheck = nil
		s.updateChan <- UIUpdate{
			Type:         UpdateServer,
			ServerID:     s.ID,
			ServerPort:   s.Port,
			ServerStatus: s.Status,
		}

		res, err := http.Get(fmt.Sprintf("http://localhost:%d/health", s.Port))
		if err != nil {
			//fmt.Println("health check failed:", err)
			return
		}

		if res.StatusCode != 200 {
			//fmt.Println("health check failed:", "not OK")
		}

		s.Status.Code = OK
		s.updateChan <- UIUpdate{
			Type:         UpdateServer,
			ServerID:     s.ID,
			ServerPort:   s.Port,
			ServerStatus: s.Status,
		}
	}()
}

func (s *Server) Start() {
	s.Status.Code = STARTING
	s.updateChan <- UIUpdate{
		Type:         AddServer,
		ServerID:     s.ID,
		ServerPort:   s.Port,
		ServerStatus: s.Status,
	}
	s.ScheduleHealthCheck(3)
	err := s.server.ListenAndServe()
	if err != nil {
		//fmt.Printf("error starting server: %v\n", err)
	}
	s.Status.Code = DOWN
	s.updateChan <- UIUpdate{
		Type:         UpdateServer,
		ServerID:     s.ID,
		ServerPort:   s.Port,
		ServerStatus: s.Status,
	}

	time.Sleep(1 * time.Second)
	s.updateChan <- UIUpdate{
		Type:         RemoveServer,
		ServerID:     s.ID,
		ServerPort:   s.Port,
		ServerStatus: s.Status,
	}
}

func (s *Server) Stop() error {
	s.Status.Code = SHUTTINGDOWN
	s.updateChan <- UIUpdate{
		Type:         UpdateServer,
		ServerID:     s.ID,
		ServerPort:   s.Port,
		ServerStatus: s.Status,
	}
	return s.server.Shutdown(context.Background())
}

func NewServer(port uint16, updateChan chan UIUpdate) Server {
	addr := fmt.Sprintf(":%d", port)

	handler := http.NewServeMux()

	handler.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		io.Copy(w, r.Body)
	})

	handler.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return Server{
		ID:   uuid.New(),
		Port: port,
		Status: ServerStatus{
			Code:             DOWN,
			UntilHealthCheck: nil,
		},
		server: &http.Server{
			Addr:    addr,
			Handler: handler,
		},
		updateChan: updateChan,
	}
}
