package datasource

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"xproxy/dao"

	mysqldriver "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sync/singleflight"
)

// sshTunnel 持有一个到跳板机的 ssh 连接, 数据库连接经它转发。
type sshTunnel struct {
	client    *ssh.Client
	signature string         // ssh 配置指纹, 变更时重建
	netName   string         // 注册到 mysql 驱动的自定义网络名
	rec       *dao.DsnRecord // 保留配置, 供断线后自动重连
	stop      chan struct{}  // 关闭以停止 keepalive
	dialSem   chan struct{}  // 限制单 client 上并发开转发通道数 (见 sshMaxConcurrentDials)
}

// sshMaxConcurrentDials 限制同一个 ssh client 上"同时开转发通道"的并发数。
// 引擎并发查询时, mysql 连接池会瞬间在同一隧道上开多个 direct-tcpip 通道,
// 突发请求可能被跳板机 sshd (MaxSessions/MaxStartups) 判定为异常而断开整条连接,
// 表现为 "unexpected packet in response to channel open: <nil>"。
// 这里把"开通道"动作限流; 通道建立后即释放, 不影响已建立连接的并发查询。
const sshMaxConcurrentDials = 2

var (
	tunMu        sync.Mutex
	tunnels      = map[string]*sshTunnel{}
	tunnelBuilds singleflight.Group
)

// sshSignature 由所有影响连接的字段拼成, 用于判断隧道是否需要重建。
func sshSignature(r *dao.DsnRecord) string {
	return strings.Join([]string{
		r.SSHHost, fmt.Sprint(r.SSHPort), r.SSHUser, r.SSHAuth,
		r.SSHPassword, r.SSHKey, r.SSHKeyPassphrase,
	}, "|")
}

// sshAuthMethods 依据认证方式构造 ssh 鉴权。
func sshAuthMethods(r *dao.DsnRecord) ([]ssh.AuthMethod, error) {
	switch strings.ToLower(strings.TrimSpace(r.SSHAuth)) {
	case "key":
		key := strings.TrimSpace(r.SSHKey)
		if key == "" {
			return nil, fmt.Errorf("ssh 认证方式为 key, 但未提供私钥")
		}
		var (
			signer ssh.Signer
			err    error
		)
		if pass := strings.TrimSpace(r.SSHKeyPassphrase); pass != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(key), []byte(pass))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(key))
		}
		if err != nil {
			return nil, fmt.Errorf("解析 ssh 私钥失败: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	default: // password
		return []ssh.AuthMethod{ssh.Password(r.SSHPassword)}, nil
	}
}

// dialSSH 建立到跳板机的 ssh 连接。
func dialSSH(r *dao.DsnRecord) (*ssh.Client, error) {
	auths, err := sshAuthMethods(r)
	if err != nil {
		return nil, err
	}
	port := r.SSHPort
	if port == 0 {
		port = 22
	}
	cfg := &ssh.ClientConfig{
		User:            strings.TrimSpace(r.SSHUser),
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // BI 内部工具, 跳板机由用户自行管理
		Timeout:         15 * time.Second,
	}
	addr := net.JoinHostPort(strings.TrimSpace(r.SSHHost), fmt.Sprint(port))
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("连接 ssh 跳板机 %s 失败: %w", addr, err)
	}
	return client, nil
}

// keepAlive 周期性向跳板机发送 keepalive 请求, 防止空闲连接被服务端断开。
// 请求失败说明连接已断, 退出循环 (后续拨号会触发自动重连)。
func keepAlive(client *ssh.Client, stop <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				return
			}
		}
	}
}

// ensureTunnel 确保某数据源的 ssh 隧道已就绪, 并返回注册给 mysql 驱动的自定义网络名。
// 隧道按数据源名缓存; ssh 配置变更时自动重建。
func ensureTunnel(name string, r *dao.DsnRecord) (string, error) {
	sig := sshSignature(r)

	if netName, ok := currentTunnelNetName(name, sig); ok {
		return netName, nil
	}

	for {
		v, err, _ := tunnelBuilds.Do(name, func() (any, error) {
			if netName, ok := currentTunnelNetName(name, sig); ok {
				return netName, nil
			}

			// 配置变化或首次建立: 先拨号 (不持锁, 避免阻塞其它数据源), 再入表。
			client, err := dialSSH(r)
			if err != nil {
				return "", err
			}

			netName := "ssh+" + name
			recCopy := *r
			t := &sshTunnel{
				client:    client,
				signature: sig,
				netName:   netName,
				rec:       &recCopy,
				stop:      make(chan struct{}),
				dialSem:   make(chan struct{}, sshMaxConcurrentDials),
			}

			tunMu.Lock()
			if old, ok := tunnels[name]; ok {
				if old.signature == sig {
					tunMu.Unlock()
					t.closeLocked()
					return old.netName, nil
				}
				old.closeLocked()
				delete(tunnels, name)
			}
			tunnels[name] = t
			tunMu.Unlock()

			go keepAlive(client, t.stop)
			registerTunnelDialer(name, netName)
			return netName, nil
		})
		if err != nil {
			return "", err
		}
		netName := v.(string)
		if cur, ok := currentTunnelNetName(name, sig); ok && cur == netName {
			return netName, nil
		}
	}
}

func currentTunnelNetName(name, sig string) (string, bool) {
	tunMu.Lock()
	defer tunMu.Unlock()
	if t, ok := tunnels[name]; ok && t.signature == sig {
		return t.netName, true
	}
	return "", false
}

// registeredDialers 记录已注册过 DialContext 的网络名, 避免重复注册。
var registeredDialers sync.Map

// registerTunnelDialer 为某数据源注册 mysql 驱动的自定义网络拨号逻辑 (仅注册一次)。
// 拨号时若发现 ssh client 已失效, 自动重连后重试一次。
func registerTunnelDialer(name, netName string) {
	if _, loaded := registeredDialers.LoadOrStore(netName, true); loaded {
		return
	}
	mysqldriver.RegisterDialContext(netName, func(ctx context.Context, addr string) (net.Conn, error) {
		conn, err := dialViaTunnelContext(ctx, name, addr)
		if err == nil {
			return conn, nil
		}
		// ssh client 可能已断开, 尝试重连后再拨一次
		if _, rerr := reconnectTunnel(name); rerr != nil {
			return nil, fmt.Errorf("ssh 隧道重连失败: %w (原始错误: %v)", rerr, err)
		}
		return dialViaTunnelContext(ctx, name, addr)
	})
}

// dialViaTunnel 经当前 ssh client 转发到目标 addr。
func dialViaTunnel(name, addr string) (net.Conn, error) {
	return dialViaTunnelContext(context.Background(), name, addr)
}

func dialViaTunnelContext(ctx context.Context, name, addr string) (net.Conn, error) {
	tunMu.Lock()
	t := tunnels[name]
	tunMu.Unlock()
	if t == nil {
		return nil, fmt.Errorf("ssh 隧道 %s 未就绪", name)
	}
	// 限流"开转发通道": 仅在 client.Dial 期间占用信号量, 通道建好即释放。
	// 避免引擎并发查询时突发开多个通道把跳板机连接打断。
	if t.dialSem != nil {
		select {
		case t.dialSem <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		defer func() { <-t.dialSem }()
	}
	conn, err := t.client.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	// ssh 通道连接不支持 SetDeadline (返回 error), 会被 mysql 驱动判为坏连接
	// 进而报 "unexpected EOF"; 用 noDeadlineConn 把 deadline 调用变为 no-op。
	return noDeadlineConn{conn}, nil
}

// reconnectTunnel 重建某数据源的 ssh 连接 (用相同配置), 用于断线自愈。
func reconnectTunnel(name string) (*sshTunnel, error) {
	tunMu.Lock()
	old := tunnels[name]
	tunMu.Unlock()
	if old == nil || old.rec == nil {
		return nil, fmt.Errorf("ssh 隧道 %s 不存在, 无法重连", name)
	}

	client, err := dialSSH(old.rec) // 不持锁拨号
	if err != nil {
		return nil, err
	}

	tunMu.Lock()
	defer tunMu.Unlock()
	// 期间可能已有其它请求完成重连, 复用之即可。
	if cur, ok := tunnels[name]; ok && cur != old {
		_ = client.Close()
		return cur, nil
	}
	if old != nil {
		old.closeLocked()
	}
	recCopy := *old.rec
	t := &sshTunnel{client: client, signature: old.signature, netName: old.netName, rec: &recCopy, stop: make(chan struct{}), dialSem: make(chan struct{}, sshMaxConcurrentDials)}
	tunnels[name] = t
	go keepAlive(client, t.stop)
	return t, nil
}

// rewriteMySQLNet 把 mysql DSN 的网络类型改为自定义的 ssh 网络名,
// 使数据库连接经 ssh 隧道建立。
func rewriteMySQLNet(dsn, netName string) (string, error) {
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("解析 mysql 连接串失败: %w", err)
	}
	cfg.Net = netName
	return cfg.FormatDSN(), nil
}

// closeLocked 停止 keepalive 并关闭 ssh client (调用方需持有 tunMu)。
func (t *sshTunnel) closeLocked() {
	if t.stop != nil {
		close(t.stop)
		t.stop = nil
	}
	if t.client != nil {
		_ = t.client.Close()
	}
}

// closeTunnel 关闭并移除某数据源的 ssh 隧道 (用于配置删除/变更)。
func closeTunnel(name string) {
	tunMu.Lock()
	defer tunMu.Unlock()
	if t, ok := tunnels[name]; ok {
		t.closeLocked()
		delete(tunnels, name)
	}
}

// noDeadlineConn 包装 ssh 通道连接, 把不被支持的 SetDeadline 系列调用变为 no-op。
// ssh 的 chanConn.SetDeadline 会返回 "deadline not supported" 错误, 而 mysql 驱动
// 在握手/读写时会调用它, 收到 error 后即认定连接损坏 (unexpected EOF)。
type noDeadlineConn struct {
	net.Conn
}

func (noDeadlineConn) SetDeadline(time.Time) error      { return nil }
func (noDeadlineConn) SetReadDeadline(time.Time) error  { return nil }
func (noDeadlineConn) SetWriteDeadline(time.Time) error { return nil }
