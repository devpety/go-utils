package redis

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// initializationWatch 监听初始化完成或取消。
type initializationWatch struct {
	stop   func() bool
	action func()
}

// initialization 管理构造期间的取消回调，成功交付时同时解除拨号及连接关闭回调。
type initialization struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	watches map[*initializationWatch]struct{}
}

// newInitialization 创建初始化上下文。
func newInitialization(ctx context.Context) *initialization {
	ctx, cancel := context.WithCancel(ctx)
	return &initialization{ctx: ctx, cancel: cancel, watches: make(map[*initializationWatch]struct{})}
}

// watch 监听初始化完成或取消。
func (init *initialization) watch(action func()) (func(), error) {
	init.mu.Lock()
	if init.ctx == nil {
		init.mu.Unlock()
		return func() {}, nil
	}
	if err := init.ctx.Err(); err != nil {
		init.mu.Unlock()
		action()
		return func() {}, err
	}
	watch := &initializationWatch{action: action, stop: context.AfterFunc(init.ctx, action)}
	init.watches[watch] = struct{}{}
	init.mu.Unlock()
	return func() {
		init.mu.Lock()
		delete(init.watches, watch)
		watch.stop()
		init.mu.Unlock()
	}, nil
}

// commit 提交初始化上下文，解除所有监听回调。
func (init *initialization) commit() error {
	init.mu.Lock()
	defer init.mu.Unlock()
	if err := init.ctx.Err(); err != nil {
		return err
	}
	for watch := range init.watches {
		if !watch.stop() {
			return init.ctx.Err()
		}
	}
	if err := init.ctx.Err(); err != nil {
		return err
	}
	init.watches = nil
	init.ctx = nil
	init.cancel()
	init.cancel = nil
	return nil
}

// abort 取消初始化上下文，执行所有监听回调。
func (init *initialization) abort() {
	init.mu.Lock()
	init.cancel()
	actions := make([]func(), 0, len(init.watches))
	for watch := range init.watches {
		watch.stop()
		actions = append(actions, watch.action)
	}
	init.watches = nil
	init.mu.Unlock()
	// commit 与取消竞争时，已解除但尚未交付的回调也必须执行清理。
	for _, action := range actions {
		action()
	}
}

// dialer 拨号器，监听初始化完成或取消。
func (init *initialization) dialer(base func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		dialCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		release, err := init.watch(cancel)
		if err != nil {
			return nil, err
		}
		connection, err := base(dialCtx, network, address)
		release()
		if err != nil {
			return nil, err
		}
		if err := dialCtx.Err(); err != nil {
			return nil, errors.Join(err, connection.Close())
		}
		_, err = init.watch(func() {
			// 关闭物理连接以中断已进入认证或探活的阻塞读写，连接池负责最终回收。
			_ = connection.Close()
		})
		if err != nil {
			return nil, err
		}
		return connection, nil
	}
}

// defaultDialer 默认拨号器，监听初始化完成或取消。
func defaultDialer(options *redis.Options) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: options.DialTimeout, KeepAlive: 5 * time.Minute}
		if options.TLSConfig == nil {
			return dialer.DialContext(ctx, network, address)
		}
		return (&tls.Dialer{NetDialer: dialer, Config: options.TLSConfig}).DialContext(ctx, network, address)
	}
}
