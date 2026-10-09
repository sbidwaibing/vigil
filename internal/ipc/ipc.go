package ipc

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/sukrutbidwai/vigil/pkg/types"
)

// Handler processes incoming IPC commands.
type Handler func(req types.IPCRequest) types.IPCResponse

// Server listens on localhost TCP for CLI commands.
type Server struct {
	listener net.Listener
	handler  Handler
	done     chan struct{}
}

// NewServer binds to localhost:port.
func NewServer(port int, handler Handler) (*Server, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("ipc listen on %s: %w", addr, err)
	}
	return &Server{
		listener: ln,
		handler:  handler,
		done:     make(chan struct{}),
	}, nil
}

// Serve accepts connections until Stop is called.
func (s *Server) Serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				continue
			}
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	decoder := json.NewDecoder(conn)
	var req types.IPCRequest
	if err := decoder.Decode(&req); err != nil {
		resp := types.IPCResponse{OK: false, Message: "invalid request"}
		json.NewEncoder(conn).Encode(resp)
		return
	}
	resp := s.handler(req)
	json.NewEncoder(conn).Encode(resp)
}

// Stop closes the listener.
func (s *Server) Stop() {
	close(s.done)
	s.listener.Close()
}

// --- Client side (used by CLI commands) ---

// SendCommand sends an IPC request to a running daemon.
func SendCommand(port int, req types.IPCRequest) (*types.IPCResponse, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("can't connect to daemon on %s — is it running?", addr)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, err
	}
	var resp types.IPCResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
