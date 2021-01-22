package websocket

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/System-Glitch/goyave/v3"
	"github.com/System-Glitch/goyave/v3/config"

	ws "github.com/gorilla/websocket"
)

// TODO test websocket.go

var (

	// ErrCloseFrameSent returned by writing operations if a close message
	// has already been sent.
	ErrCloseFrameSent = errors.New("close frame sent, cannot write anymore")

	// ErrCloseFrameReceived returned by reading operations if a close message
	// has already been received.
	ErrCloseFrameReceived = errors.New("close frame received, cannot read anymore")

	// ErrCloseTimeout returned during the close handshake if the client took
	// too long to respond with
	ErrCloseTimeout = errors.New("close handshake timed out")

	// TODO document behavior when close frame sent or received
)

// HandlerFunc is a handler for websocket connections.
// The request parameter contains the original upgraded HTTP request.
//
// To keep connection alive, these handlers should run an infinite for loop
// and check for close errors. The connection is closed when the handler
// returns. Therefore, if the latter is using goroutines, it should use a
// sync.WaitGroup to wait for them to terminate before returning.
//
// Instead of closing the connection yourself, just make your handler return
// and the close handshake will be performed automatically.
//
// The following HandlerFunc is an example of an "echo" feature using websockets:
//
//  func(c *websocket.Conn, request *goyave.Request) error {
//  	for {
//  		mt, message, err := c.ReadMessage()
//  		if err != nil {
//  			if websocket.IsCloseError(err) {
//  				return nil
//  			}
//  			return fmt.Errorf("read: %v", err)
//  		}
//  		goyave.Logger.Printf("recv: %s", message)
//  		err = c.WriteMessage(mt, message)
//  		if err != nil {
//  			return fmt.Errorf("write: %v", err)
//  		}
//  	}
//  }
type HandlerFunc func(*Conn, *goyave.Request) error

// UpgradeErrorHandler is a specific Handler type for connection upgrade errors.
// These handlers are called when an error occurs while the protocol switching.
type UpgradeErrorHandler func(response *goyave.Response, request *goyave.Request, status int, reason error)

func defaultUpgradeErrorHandler(response *goyave.Response, request *goyave.Request, status int, reason error) {
	text := http.StatusText(status)
	if config.GetBool("app.debug") && reason != nil {
		text = reason.Error()
	}
	message := map[string]string{
		"error": text,
	}
	response.JSON(status, message)
}

// ErrorHandler is a specific Handler type for handling errors returned by HandlerFunc.
type ErrorHandler func(request *goyave.Request, err error)

// Conn represents a WebSocket connection.
type Conn struct {
	*ws.Conn
	waitClose     chan struct{}
	receivedClose bool
	sentClose     bool
}

func newConn(c *ws.Conn) *Conn {
	conn := &Conn{
		Conn:      c,
		waitClose: make(chan struct{}, 1),
	}
	c.SetCloseHandler(conn.closeHandler)
	return conn
}

// TODO handle pings and pongs

func (c *Conn) closeHandler(code int, text string) error {
	if c.receivedClose {
		return ErrCloseFrameReceived
	}
	c.receivedClose = true
	if c.sentClose { // TODO check concurrency
		c.waitClose <- struct{}{}
	}
	return nil
}

// NextWriter returns a writer for the next message to send. The writer's Close
// method flushes the complete message to the network.
//
// There can be at most one open writer on a connection. NextWriter closes the
// previous writer if the application has not already done so.
//
// All message types (TextMessage, BinaryMessage, CloseMessage, PingMessage and
// PongMessage) are supported.
func (c *Conn) NextWriter(messageType int) (io.WriteCloser, error) { // FIXME doesn't work, need to override all read and write functions
	if c.sentClose {
		return nil, ErrCloseFrameSent
	}
	if messageType == ws.CloseMessage {
		c.sentClose = true
	}
	return c.Conn.NextWriter(messageType)
}

// shutdownNormal performs the closing handshake as specified by
// RFC 6455 Section 1.4. Sends status code 1000 (normal closure) and
// message "Server closed connection".
func (c *Conn) shutdownNormal() error {
	return c.shutdown(ws.CloseNormalClosure, "Server closed connection")
}

// shutdownOnError performs the closing handshake as specified by
// RFC 6455 Section 1.4 because a server error occurred.
// Sends status code 1011 (internal server error) and
// message "Internal server error". If debug is enabled,
// the message is set to the given error's message.
func (c *Conn) shutdownOnError(err error) error {
	message := "Internal server error"
	if config.GetBool("app.debug") {
		message = err.Error()
	}
	return c.shutdown(ws.CloseInternalServerErr, message)
}

func (c *Conn) shutdown(code int, message string) error {
	if !c.sentClose {
		m := ws.FormatCloseMessage(code, message)
		err := c.WriteControl(ws.CloseMessage, m, time.Now().Add(time.Second))
		if err != nil {
			goyave.ErrLogger.Println(err) // TODO better shutdown error handling
		}
	}

	if !c.receivedClose {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // TODO timeout
		defer cancel()
		// FIXME would always timeout if the handler already returned because
		// nothing is reading anymore
		select {
		case <-ctx.Done():
			goyave.ErrLogger.Println(ErrCloseTimeout)
		case <-c.waitClose:
			close(c.waitClose)
		}
	}

	// TODO properly shutdown before goyave.Start returns?
	return c.Close()
}

// Upgrader is responsible for the upgrade of HTTP connections to
// websocket connections.
type Upgrader struct {
	// UpgradeErrorHandler specifies the function for generating HTTP error responses.
	//
	// The default UpgradeErrorHandler returns a JSON response containing the status text
	// corresponding to the status code returned. If debugging is enabled, the reason error
	// message is returned instead.
	//
	//  {"error": "message"}
	UpgradeErrorHandler UpgradeErrorHandler

	// ErrorHandler specifies the function handling errors returned by HandlerFunc.
	// If nil, the error is written to "goyave.ErrLogger".
	ErrorHandler ErrorHandler

	// CheckOrigin returns true if the request Origin header is acceptable. If
	// CheckOrigin is nil, then a safe default is used: return false if the
	// Origin request header is present and the origin host is not equal to
	// request Host header.
	//
	// A CheckOrigin function should carefully validate the request origin to
	// prevent cross-site request forgery.
	CheckOrigin func(r *goyave.Request) bool

	// Headers function generating headers to be sent with the protocol switching response.
	Headers func(request *goyave.Request) http.Header

	// Settings the parameters for upgrading the connection. "Error" and "CheckOrigin" are
	// ignored: this the Goyave upgrader's "ErrorHandler" and "CheckOrigin".
	Settings ws.Upgrader
}

func (u *Upgrader) makeUpgrader(request *goyave.Request) *ws.Upgrader {
	upgradeErrorHandler := u.UpgradeErrorHandler
	if upgradeErrorHandler == nil {
		upgradeErrorHandler = defaultUpgradeErrorHandler
	}
	a := adapter{
		upgradeErrorHandler: upgradeErrorHandler,
		checkOrigin:         u.CheckOrigin,
		request:             request,
	}

	upgrader := u.Settings
	upgrader.Error = a.onError
	upgrader.CheckOrigin = a.getCheckOriginFunc()
	return &upgrader
}

// Handler create an HTTP handler upgrading the HTTP connection before passing it
// to the given HandlerFunc.
//
// HTTP response's status is set to "101 Switching Protocols".
//
// The connection is closed automatically after the HandlerFunc returns, using the
// closing handshake defined by RFC 6455 Section 1.4 if possible. If the HandlerFunc
// returns an error, the Upgrader's error handler will be executed and the close frame
// sent to the client will have status code 1011 (internal server error) and
// "Internal server error" as message. If debug is enabled, the message will be set to the
// one of the error returned by the HandlerFunc.
// Otherwise the close frame will have status code 1000 (normal closure) and
// "Server closed connection" as a message.
//
// Bear in mind that the recovery middleware doesn't work on websocket connections,
// as we are not in an HTTP context anymore.
func (u *Upgrader) Handler(handler HandlerFunc) goyave.Handler {
	return func(response *goyave.Response, request *goyave.Request) {
		var headers http.Header
		if u.Headers != nil {
			headers = u.Headers(request)
		}

		c, err := u.makeUpgrader(request).Upgrade(response, request.Request(), headers)
		if err != nil {
			return
		}
		response.Status(http.StatusSwitchingProtocols)
		// TODO recovery?
		conn := newConn(c)
		err = handler(conn, request)
		if err != nil {
			if u.ErrorHandler != nil {
				u.ErrorHandler(request, err)
			} else {
				goyave.ErrLogger.Println(err)
			}
			conn.shutdownOnError(err)
		} else {
			conn.shutdownNormal()
		}
	}
}

type adapter struct {
	upgradeErrorHandler UpgradeErrorHandler
	checkOrigin         func(r *goyave.Request) bool
	request             *goyave.Request
}

func (a *adapter) onError(w http.ResponseWriter, r *http.Request, status int, reason error) {
	if status == http.StatusInternalServerError {
		panic(reason)
	}
	w.Header().Set("Sec-Websocket-Version", "13")
	a.upgradeErrorHandler(w.(*goyave.Response), a.request, status, reason)
}

func (a *adapter) getCheckOriginFunc() func(r *http.Request) bool {
	if a.checkOrigin != nil {
		return func(r *http.Request) bool {
			return a.checkOrigin(a.request)
		}
	}

	return nil
}

// IsCloseError returns true if the error is one of the following close errors:
// CloseNormalClosure (1000), CloseGoingAway (1001) or CloseNoStatusReceived (1005)
func IsCloseError(err error) bool {
	return ws.IsCloseError(err,
		ws.CloseNormalClosure,
		ws.CloseGoingAway,
		ws.CloseNoStatusReceived,
	)
}
