// Package http 提供HTTP服务器和客户端功能
// 功能特性：
//   - HTTP服务器管理（启动、停止、TLS支持）
//   - 服务发现端点注册
//   - 网络配置（TCP/UDP）
//   - 地址自动提取
package http

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync"

	"github.com/devpety/go-utils/v2/netx"
	"github.com/devpety/go-utils/v2/server"
)

// ErrServerAlreadyStarted 表示同一实例正在提供服务，不能重复启动。
var ErrServerAlreadyStarted = errors.New("HTTP server has already started")

var _ server.Server = (*Server)(nil)
var _ server.Endpointer = (*Server)(nil)

type ServerOption func(*Server)

// WithServerNetwork with server network.
func WithServerNetwork(network string) ServerOption {
	return func(s *Server) {
		s.network = network
	}
}

// WithServerAddress with server address.
func WithServerAddress(addr string) ServerOption {
	return func(s *Server) {
		s.address = addr
	}
}

// WithServerEndpoint with server endpoint.
func WithServerEndpoint(endpoint *url.URL) ServerOption {
	return func(o *Server) {
		o.endpoint = endpoint
	}
}

// WithServerTLSConfig with TLS config.
func WithServerTLSConfig(c *tls.Config) ServerOption {
	return func(o *Server) {
		o.tlsConf = c
	}
}

type Server struct {
	*http.Server

	mu       sync.Mutex
	started  bool
	stopped  bool
	network  string
	address  string
	lis      net.Listener
	endpoint *url.URL
	tlsConf  *tls.Config
}

// NewServer 使用传入的服务器配置；显式选项覆盖监听地址和 TLS 配置。
func NewServer(hs *http.Server, opts ...ServerOption) *Server {
	if hs == nil {
		hs = &http.Server{}
	}
	srv := &Server{Server: hs, network: "tcp", tlsConf: hs.TLSConfig}
	for _, opt := range opts {
		opt(srv)
	}
	if srv.address == "" {
		srv.address = srv.Server.Addr
	}
	// 监听方式与注册端点必须使用同一份有效 TLS 配置。
	srv.Server.TLSConfig = srv.tlsConf
	return srv
}

// ensureListener 在持有 mu 时创建监听器，独立于对外公布的 Endpoint。
func (s *Server) ensureListener() (net.Listener, error) {
	if s.stopped {
		return nil, http.ErrServerClosed
	}
	if s.lis != nil {
		return s.lis, nil
	}
	listener, err := net.Listen(s.network, s.address)
	if err != nil {
		return nil, err
	}
	s.lis = listener
	return listener, nil
}

// Endpoint 返回对外公布的地址；自动地址需要先绑定端口，以正确解析端口 0。
// 显式 Endpoint 只提供注册信息，不替代 Start 创建实际监听器。
func (s *Server) Endpoint() (*url.URL, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.endpoint == nil {
		listener, err := s.ensureListener()
		if err != nil {
			return nil, err
		}
		address, err := netx.ExtractAddress(s.address, listener)
		if err != nil {
			if s.started {
				return nil, err
			}
			s.lis = nil
			return nil, errors.Join(err, closeListener(listener))
		}
		s.endpoint = server.NewEndpoint("http", address, s.tlsConf != nil)
	}
	endpoint := *s.endpoint
	return &endpoint, nil
}

// Start 提供服务；同一实例只运行一个生命周期，停止后不会重新打开端口。
// 显式停止视为正常退出；启动失败时关闭已创建但尚未被 net/http 接管的监听器。
func (s *Server) Start(ctx context.Context) (err error) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	if s.started {
		s.mu.Unlock()
		return ErrServerAlreadyStarted
	}
	if err := ctx.Err(); err != nil {
		s.stopped = true
		listener := s.lis
		s.mu.Unlock()
		return errors.Join(err, s.Server.Close(), closeListener(listener))
	}
	listener, err := s.ensureListener()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.started = true
	s.BaseContext = func(net.Listener) context.Context { return ctx }
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.stopped = true
		s.mu.Unlock()
		err = errors.Join(err, closeListener(listener))
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.tlsConf != nil {
		err = s.ServeTLS(listener, "", "")
	} else {
		err = s.Serve(listener)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Stop 优雅排空请求，同时回收 Endpoint 可能提前创建的监听器。
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.stopped = true
	listener := s.lis
	s.mu.Unlock()
	return errors.Join(s.Server.Shutdown(ctx), closeListener(listener))
}

// Close 立即关闭连接及尚未交付给 Serve 的监听器。
func (s *Server) Close() error {
	s.mu.Lock()
	s.stopped = true
	listener := s.lis
	s.mu.Unlock()
	return errors.Join(s.Server.Close(), closeListener(listener))
}

// closeListener 关闭监听器，忽略已关闭错误。
func closeListener(listener net.Listener) error {
	if listener == nil {
		return nil
	}
	err := listener.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
