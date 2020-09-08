package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"golang.org/x/net/websocket"
)

type HttpProxy struct {
	Listen       string `json:"listen"`
	Target       string `json:"target"`
	targetUrl    *url.URL
	reverseProxy *httputil.ReverseProxy
	compilers    *CompilerGroup
}

func (h *HttpProxy) Start(compilers *CompilerGroup) error {
	// listen for connections
	listener, err := net.Listen("tcp", h.Listen)
	if err != nil {
		return err
	}

	h.targetUrl, err = url.Parse(h.Target)
	if err != nil {
		return err
	}

	fmt.Println(h.Listen, h.Target)

	// setup proxy
	h.compilers = compilers
	h.reverseProxy = httputil.NewSingleHostReverseProxy(h.targetUrl)
	http.HandleFunc("/_autogo/refresh.js", h.refreshJs)
	http.Handle("/_autogo/refresh.ws", websocket.Handler(h.refreshWebsocket))
	http.Handle("/", h)
	go http.Serve(listener, nil)

	return nil
}

func (h *HttpProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.compilers.WaitForState(Idle)

	// wait for the server to be fully started.
	for {
		conn, err := net.Dial("tcp", h.targetUrl.Host)
		if err != nil {
			time.Sleep(time.Millisecond * 50)
			//conn.Close()
		} else {
			conn.Close()
			break
		}
	}
	//w.Write([]byte("<html><head><script type=\"text/javascript\" src=\"/_autogo/refresh.js\" ></script></head><body>Kinda cool</body></html>"))

	h.reverseProxy.ServeHTTP(w, r)
}

func (h *HttpProxy) refreshJs(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`
	 	var socket = new WebSocket("ws://"+location.host+"/_autogo/refresh.ws", "reloadprotocol") 
	 	socket.onclose = function () { location.reload()}
	`))
}

func (h *HttpProxy) refreshWebsocket(ws *websocket.Conn) {
	h.compilers.WaitForState(Compiling)
	ws.Write([]byte{50})
	ws.Close()
}
