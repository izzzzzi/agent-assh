package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

const (
	protocolVersion = "2024-11-05"
	serverName      = "assh-mcp"
	serverVersion   = "1.0.0"
)

type Server struct {
	in       *bufio.Reader
	out      io.Writer
	mu       sync.Mutex
	nextID   int
	tools    []Tool
	handlers map[string]ToolHandler
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type ToolHandler func(args map[string]any) (any, error)

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      *int      `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewServer() *Server {
	s := &Server{
		in:       bufio.NewReader(os.Stdin),
		out:      os.Stdout,
		handlers: make(map[string]ToolHandler),
	}
	s.registerTools()
	return s
}

func (s *Server) registerTools() {
	s.tools = []Tool{
		{
			Name:        "ssh_connect",
			Description: "Establish SSH connection to a remote host and open a persistent tmux session",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"host":         {Type: "string", Description: "SSH hostname or IP"},
					"user":         {Type: "string", Description: "SSH username"},
					"port":         {Type: "integer", Description: "SSH port (default: 22)"},
					"identity":     {Type: "string", Description: "SSH identity file path"},
					"password_env": {Type: "string", Description: "Environment variable containing password"},
					"session_name": {Type: "string", Description: "Session label"},
				},
				Required: []string{"host", "user"},
			},
		},
		{
			Name:        "ssh_exec",
			Description: "Execute a command in a tmux session",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"sid":     {Type: "string", Description: "Session ID from ssh_connect"},
					"command": {Type: "string", Description: "Command to execute"},
					"timeout": {Type: "integer", Description: "Timeout in seconds (default: 300)"},
				},
				Required: []string{"sid", "command"},
			},
		},
		{
			Name:        "ssh_read",
			Description: "Read output from a tmux session command",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"sid":    {Type: "string", Description: "Session ID"},
					"seq":    {Type: "integer", Description: "Command sequence number"},
					"stream": {Type: "string", Description: "stdout or stderr"},
					"limit":  {Type: "integer", Description: "Lines to return (default: 50)"},
					"offset": {Type: "integer", Description: "Line offset (default: 0)"},
				},
				Required: []string{"sid", "seq"},
			},
		},
		{
			Name:        "ssh_close",
			Description: "Close a tmux session",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"sid": {Type: "string", Description: "Session ID"},
				},
				Required: []string{"sid"},
			},
		},
		{
			Name:        "ssh_scan",
			Description: "Scan remote host: OS, kernel, CPU, memory, disk, IP",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"host":     {Type: "string", Description: "SSH hostname or IP"},
					"user":     {Type: "string", Description: "SSH username"},
					"identity": {Type: "string", Description: "SSH identity file"},
				},
				Required: []string{"host"},
			},
		},
	}

	for _, t := range s.tools {
		s.handlers[t.Name] = nil // populated by caller via RegisterHandler
	}
}

func (s *Server) RegisterHandler(name string, handler ToolHandler) {
	s.handlers[name] = handler
}

func (s *Server) Run() error {
	for {
		line, err := s.in.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		resp := s.handle(&req)
		if resp != nil {
			s.writeResponse(resp)
		}
	}
}

func (s *Server) handle(req *jsonRPCRequest) *jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	case "notifications/initialized":
		return nil // no response for notifications
	default:
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: "Method not found: " + req.Method},
		}
	}
}

func (s *Server) handleInitialize(req *jsonRPCRequest) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": protocolVersion,
			"serverInfo": map[string]string{
				"name":    serverName,
				"version": serverVersion,
			},
			"capabilities": map[string]any{
				"tools": map[string]bool{},
			},
		},
	}
}

func (s *Server) handleToolsList(req *jsonRPCRequest) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"tools": s.tools,
		},
	}
}

func (s *Server) handleToolsCall(req *jsonRPCRequest) *jsonRPCResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32602, Message: "Invalid params"},
		}
	}

	handler, ok := s.handlers[params.Name]
	if !ok || handler == nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32602, Message: "Unknown tool: " + params.Name},
		}
	}

	var args map[string]any
	if err := json.Unmarshal(params.Arguments, &args); err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32602, Message: "Invalid arguments"},
		}
	}

	result, err := handler(args)
	if err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": fmt.Sprintf("Error: %v", err)},
				},
				"isError": true,
			},
		}
	}

	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": toJSON(result)},
			},
		},
	}
}

func (s *Server) writeResponse(resp *jsonRPCResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(resp)
	_, _ = s.out.Write(append(data, '\n'))
}

func toJSON(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}
