package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type response struct {
	value any
	err   error
}
type outboundMessage struct {
	data []byte
	sent chan error
}

type Client struct {
	config       ServerConfig
	cwd          string
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	outgoing     chan outboundMessage
	stopped      chan struct{}
	mu           sync.Mutex
	pending      map[int]chan response
	nextID       int
	closed       bool
	stderr       string
	listeners    map[int]func(string, any)
	nextListener int
	capabilities map[string]any
	exited       chan struct{}
	disposeOnce  sync.Once
	disposed     chan struct{}
	unregister   func()
}

func FileURI(path string) string {
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" && strings.HasPrefix(path, "//") {
		host, rest, _ := strings.Cut(strings.TrimPrefix(path, "//"), "/")
		return (&url.URL{Scheme: "file", Host: host, Path: "/" + rest}).String()
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func NewClient(config ServerConfig, cwd string, registerCleanup func(func()) func()) (*Client, error) {
	cmd := exec.Command(config.Command, config.Args...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	for key, value := range config.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	prepareProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return nil, err
	}
	c := &Client{config: config, cwd: cwd, cmd: cmd, stdin: stdin, pending: map[int]chan response{}, nextID: 1, listeners: map[int]func(string, any){}, exited: make(chan struct{}), disposed: make(chan struct{}), unregister: func() {}, outgoing: make(chan outboundMessage, 64), stopped: make(chan struct{})}
	if registerCleanup != nil {
		c.unregister = registerCleanup(c.kill)
	}
	go c.writeMessages()
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		b := make([]byte, 1024)
		for {
			n, err := stderr.Read(b)
			if n > 0 {
				c.mu.Lock()
				c.stderr += string(b[:n])
				if len(c.stderr) > MaxStderrChars {
					c.stderr = c.stderr[len(c.stderr)-MaxStderrChars:]
				}
				c.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		if err := c.receive(stdout); err != nil {
			c.fail(fmt.Errorf("Invalid LSP response: %w", err))
			c.kill()
		}
	}()
	go func() {
		<-readDone
		<-stderrDone
		_ = cmd.Wait()
		c.mu.Lock()
		detail := c.stderr
		c.mu.Unlock()
		if detail != "" {
			detail = ": " + detail
		}
		c.fail(fmt.Errorf("LSP server '%s' exited%s", config.Name, detail))
		c.unregister()
		close(c.exited)
	}()
	return c, nil
}
func (c *Client) kill() {
	select {
	case <-c.exited:
		return
	default:
		killProcess(c.cmd)
	}
}
func (c *Client) IsClosed() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.closed }
func (c *Client) Capabilities() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.capabilities
}
func (c *Client) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.stopped)
	}
	for id, p := range c.pending {
		p <- response{err: err}
		delete(c.pending, id)
	}
}
func (c *Client) writeMessages() {
	for {
		select {
		case <-c.stopped:
			return
		case message := <-c.outgoing:
			_, err := c.stdin.Write(message.data)
			message.sent <- err
			if err != nil {
				c.fail(err)
				c.kill()
				return
			}
		}
	}
}
func (c *Client) queue(message map[string]any) (<-chan error, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("LSP server '%s' is not running", c.config.Name)
	}
	message["jsonrpc"] = "2.0"
	data, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	framed := append([]byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))), data...)
	sent := make(chan error, 1)
	select {
	case <-c.stopped:
		return nil, fmt.Errorf("LSP server '%s' is not running", c.config.Name)
	case c.outgoing <- outboundMessage{data: framed, sent: sent}:
		return sent, nil
	default:
		err := errors.New("LSP outgoing queue is full")
		c.fail(err)
		c.kill()
		return nil, err
	}
}
func (c *Client) send(message map[string]any) error { _, err := c.queue(message); return err }

func (c *Client) Notify(method string, params any) error {
	message := map[string]any{"method": method}
	if params != nil {
		message["params"] = params
	}
	return c.send(message)
}
func (c *Client) Request(ctx context.Context, method string, params any, timeout time.Duration) (any, error) {
	if ctx.Err() != nil {
		return nil, errors.New(RequestInterruptedError)
	}
	if timeout == 0 {
		timeout = DefaultRequestTimeout
		if c.config.TimeoutMs != 0 {
			timeout = time.Duration(c.config.TimeoutMs) * time.Millisecond
		}
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("LSP server '%s' is not running", c.config.Name)
	}
	id := c.nextID
	c.nextID++
	pending := make(chan response, 1)
	c.pending[id] = pending
	c.mu.Unlock()
	message := map[string]any{"id": id, "method": method}
	if params != nil {
		message["params"] = params
	}
	if err := c.send(message); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var err error
	select {
	case result := <-pending:
		return result.value, result.err
	case <-ctx.Done():
		err = errors.New(RequestInterruptedError)
	case <-timer.C:
		err = fmt.Errorf("LSP %s timed out after %dms", method, timeout.Milliseconds())
	}
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
	_ = c.Notify("$/cancelRequest", map[string]any{"id": id})
	return nil, err
}
func (c *Client) receive(reader io.Reader) error {
	r := bufio.NewReader(reader)
	for {
		var header strings.Builder
		for !strings.HasSuffix(header.String(), "\r\n\r\n") {
			b, err := r.ReadByte()
			if err != nil {
				if err == io.EOF && header.Len() == 0 {
					return nil
				}
				return err
			}
			header.WriteByte(b)
			if header.Len() > MaxHeaderBytes+4 {
				return errors.New(HeaderTooLargeError)
			}
		}
		raw := strings.TrimSuffix(header.String(), "\r\n\r\n")
		if len(raw) > MaxHeaderBytes {
			return errors.New(HeaderTooLargeError)
		}
		count, length := 0, 0
		for _, line := range strings.Split(raw, "\r\n") {
			key, value, ok := strings.Cut(line, ":")
			if ok && strings.EqualFold(key, "Content-Length") {
				n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
				if err != nil {
					return errors.New(ContentLengthExpectedError)
				}
				if n > MaxMessageBytes {
					return errors.New(MessageTooLargeError)
				}
				length = int(n)
				count++
			}
		}
		if count != 1 {
			return errors.New(ContentLengthExpectedError)
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(r, data); err != nil {
			return err
		}
		var message map[string]any
		if err := json.Unmarshal(data, &message); err != nil {
			return err
		}
		if message["jsonrpc"] != "2.0" {
			return errors.New(InvalidJSONRPCError)
		}
		if method, ok := message["method"].(string); ok {
			if _, hasID := message["id"]; hasID {
				if err := c.respond(message); err != nil {
					return err
				}
			} else {
				c.mu.Lock()
				listeners := make([]func(string, any), 0, len(c.listeners))
				for _, fn := range c.listeners {
					listeners = append(listeners, fn)
				}
				c.mu.Unlock()
				for _, fn := range listeners {
					fn(method, message["params"])
				}
			}
		} else if id, ok := message["id"].(float64); ok {
			c.mu.Lock()
			pending := c.pending[int(id)]
			delete(c.pending, int(id))
			c.mu.Unlock()
			if pending != nil {
				result := response{value: message["result"]}
				if failure, ok := message["error"].(map[string]any); ok {
					msg, ok := failure["message"].(string)
					if !ok {
						msg = RequestFailedError
					}
					result.err = errors.New(msg)
				}
				pending <- result
			}
		}
	}
}
func (c *Client) respond(message map[string]any) error {
	result := map[string]any{"id": message["id"], "result": nil}
	params, _ := message["params"].(map[string]any)
	switch message["method"] {
	case "workspace/applyEdit":
		result["result"] = map[string]any{"applied": false, "failureReason": ApplyEditFailureReason}
	case "workspace/configuration":
		items, _ := params["items"].([]any)
		values := make([]any, 0, len(items))
		for _, item := range items {
			var value any = c.config.Settings
			if value == nil {
				value = map[string]any{}
			}
			if record, ok := item.(map[string]any); ok {
				if section, ok := record["section"].(string); ok {
					for _, key := range strings.Split(section, ".") {
						record, _ := value.(map[string]any)
						value = record[key]
					}
				}
			}
			values = append(values, value)
		}
		result["result"] = values
	case "workspace/workspaceFolders":
		result["result"] = []any{map[string]any{"uri": FileURI(c.cwd), "name": c.cwd}}
	case "window/showMessageRequest", "window/workDoneProgress/create":
	default:
		delete(result, "result")
		result["error"] = map[string]any{"code": -32601, "message": UnsupportedMethodError}
	}
	return c.send(result)
}
func (c *Client) OnNotification(fn func(string, any)) func() {
	c.mu.Lock()
	id := c.nextListener
	c.nextListener++
	c.listeners[id] = fn
	c.mu.Unlock()
	return func() { c.mu.Lock(); delete(c.listeners, id); c.mu.Unlock() }
}
func (c *Client) Initialize() error {
	params := map[string]any{"processId": os.Getpid(), "clientInfo": map[string]any{"name": "yukino"}, "rootUri": FileURI(c.cwd), "workspaceFolders": []any{map[string]any{"uri": FileURI(c.cwd), "name": c.cwd}}, "capabilities": map[string]any{"general": map[string]any{"positionEncodings": []string{"utf-16"}}, "workspace": map[string]any{"configuration": true, "workspaceFolders": true, "applyEdit": false}, "textDocument": map[string]any{"synchronization": map[string]any{"dynamicRegistration": false}, "publishDiagnostics": map[string]any{"versionSupport": true}, "diagnostic": map[string]any{"dynamicRegistration": false}}}}
	if c.config.InitializationOptions != nil {
		params["initializationOptions"] = c.config.InitializationOptions
	}
	value, err := c.Request(context.Background(), "initialize", params, 0)
	if err != nil {
		return err
	}
	result, ok := value.(map[string]any)
	if !ok {
		return errors.New(InvalidInitializeResponseError)
	}
	capabilities, ok := result["capabilities"].(map[string]any)
	if !ok {
		return errors.New(InvalidInitializeResponseError)
	}
	c.mu.Lock()
	c.capabilities = capabilities
	c.mu.Unlock()
	if encoding := capabilities["positionEncoding"]; encoding != nil && encoding != "" && encoding != "utf-16" {
		return errors.New(UnsupportedPositionEncodingError)
	}
	if err := c.Notify("initialized", map[string]any{}); err != nil {
		return err
	}
	if c.config.Settings != nil {
		return c.Notify("workspace/didChangeConfiguration", map[string]any{"settings": c.config.Settings})
	}
	return nil
}
func (c *Client) Dispose() error {
	c.disposeOnce.Do(func() {
		if !c.IsClosed() {
			if _, err := c.Request(context.Background(), "shutdown", nil, ShutdownTimeout); err == nil {
				if sent, err := c.queue(map[string]any{"method": "exit"}); err == nil {
					timer := time.NewTimer(KillDelay)
					select {
					case <-sent:
					case <-timer.C:
					}
					timer.Stop()
				}
			}
		}
		c.fail(errors.New(ClientDisposedError))
		c.mu.Lock()
		clear(c.listeners)
		c.mu.Unlock()
		_ = c.stdin.Close()
		timer := time.AfterFunc(KillDelay, c.kill)
		<-c.exited
		timer.Stop()
		c.unregister()
		close(c.disposed)
	})
	<-c.disposed
	return nil
}
