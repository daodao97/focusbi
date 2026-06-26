package datasource

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/pem"
	"io"
	"net"
	"testing"

	"xproxy/dao"

	"golang.org/x/crypto/ssh"
)

// genHostKey 生成一个临时 ed25519 主机密钥用于测试 SSH 服务端。
func genHostKey(t *testing.T) ssh.Signer {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen ed25519: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	return signer
}

// startSSHServer 启动一个仅用于测试的最小 SSH 服务端:
// 接受密码 "secret" 或指定公钥, 支持 direct-tcpip 通道。
// 返回监听地址, 测试结束自动关闭。
func startSSHServer(t *testing.T, authorizedKey ...ssh.PublicKey) string {
	hostKey := genHostKey(t)
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if string(pass) == "secret" {
				return &ssh.Permissions{}, nil
			}
			return nil, io.EOF
		},
	}
	if len(authorizedKey) > 0 && authorizedKey[0] != nil {
		want := authorizedKey[0].Marshal()
		cfg.PublicKeyCallback = func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) == string(want) {
				return &ssh.Permissions{}, nil
			}
			return nil, io.EOF
		}
	}
	cfg.AddHostKey(hostKey)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleSSHConn(conn, cfg)
		}
	}()
	return ln.Addr().String()
}

func handleSSHConn(nConn net.Conn, cfg *ssh.ServerConfig) {
	sshConn, chans, reqs, err := ssh.NewServerConn(nConn, cfg)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)

	for newCh := range chans {
		if newCh.ChannelType() != "direct-tcpip" {
			_ = newCh.Reject(ssh.UnknownChannelType, "only direct-tcpip")
			continue
		}
		go handleDirectTCPIP(newCh)
	}
}

// directTCPIPMsg 是 direct-tcpip 通道的额外数据 (RFC 4254 §7.2)。
type directTCPIPMsg struct {
	DestAddr string
	DestPort uint32
	SrcAddr  string
	SrcPort  uint32
}

func handleDirectTCPIP(newCh ssh.NewChannel) {
	var msg directTCPIPMsg
	if err := ssh.Unmarshal(newCh.ExtraData(), &msg); err != nil {
		_ = newCh.Reject(ssh.ConnectionFailed, "bad payload")
		return
	}
	target := net.JoinHostPort(msg.DestAddr, itoa(msg.DestPort))
	remote, err := net.Dial("tcp", target)
	if err != nil {
		_ = newCh.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := newCh.Accept()
	if err != nil {
		_ = remote.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	go func() { _, _ = io.Copy(remote, ch); _ = remote.Close() }()
	go func() { _, _ = io.Copy(ch, remote); _ = ch.Close() }()
}

func itoa(p uint32) string {
	b := make([]byte, 0, 5)
	if p == 0 {
		return "0"
	}
	var tmp [10]byte
	i := len(tmp)
	for p > 0 {
		i--
		tmp[i] = byte('0' + p%10)
		p /= 10
	}
	return string(append(b, tmp[i:]...))
}

// 让 binary 包被引用 (Unmarshal 内部已处理, 这里仅保证导入不被裁剪的占位).
var _ = binary.BigEndian

// TestSSHTunnelPing 验证: 经 SSH 跳板机转发到本地 MySQL 的 PingRecord 能成功。
// 需要本地 127.0.0.1:3306 有可用 MySQL (root:root). 不可用时跳过。
func TestSSHTunnelPing(t *testing.T) {
	// 先确认本地 MySQL 直连可用, 否则跳过 (隧道目标不存在无法验证)。
	if err := PingDSN("mysql", "root:root@tcp(127.0.0.1:3306)/?timeout=3s"); err != nil {
		t.Skipf("本地 MySQL 不可用, 跳过 SSH 隧道测试: %v", err)
	}

	addr := startSSHServer(t)
	host, port, _ := net.SplitHostPort(addr)

	rec := &dao.DsnRecord{
		Driver:      "mysql",
		DSN:         "root:root@tcp(127.0.0.1:3306)/?timeout=5s",
		SSHEnabled:  true,
		SSHHost:     host,
		SSHPort:     atoiSafe(port),
		SSHUser:     "tester",
		SSHAuth:     "password",
		SSHPassword: "secret",
	}

	if err := PingRecord(rec); err != nil {
		t.Fatalf("PingRecord through SSH tunnel failed: %v", err)
	}
}

// TestSSHTunnelQuery 验证经 ssh 隧道执行【真实查询】成功。
// 这是 "unexpected EOF" 回归测试: ssh 通道连接不支持 SetDeadline,
// 若不包装为 no-op, mysql 驱动会在查询时把连接判为损坏。
func TestSSHTunnelQuery(t *testing.T) {
	if err := PingDSN("mysql", "root:root@tcp(127.0.0.1:3306)/?timeout=3s"); err != nil {
		t.Skipf("本地 MySQL 不可用, 跳过: %v", err)
	}

	addr := startSSHServer(t)
	host, port, _ := net.SplitHostPort(addr)

	netName, err := ensureTunnel("__test_query__", &dao.DsnRecord{
		SSHHost: host, SSHPort: atoiSafe(port), SSHUser: "tester",
		SSHAuth: "password", SSHPassword: "secret",
	})
	if err != nil {
		t.Fatalf("ensureTunnel: %v", err)
	}
	defer closeTunnel("__test_query__")

	dsn, err := rewriteMySQLNet("root:root@tcp(127.0.0.1:3306)/?timeout=5s&readTimeout=5s", netName)
	if err != nil {
		t.Fatalf("rewriteMySQLNet: %v", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var got int
	if err := db.QueryRow("SELECT 1 + 1").Scan(&got); err != nil {
		t.Fatalf("query through tunnel failed: %v", err)
	}
	if got != 2 {
		t.Fatalf("want 2, got %d", got)
	}
}

// TestSSHTunnelReconnect 验证: ssh client 断开后, 下一次拨号能自动重连并成功。
// 模拟跳板机空闲断线 / 网络抖动后的自愈。
func TestSSHTunnelReconnect(t *testing.T) {
	if err := PingDSN("mysql", "root:root@tcp(127.0.0.1:3306)/?timeout=3s"); err != nil {
		t.Skipf("本地 MySQL 不可用, 跳过: %v", err)
	}

	addr := startSSHServer(t)
	host, port, _ := net.SplitHostPort(addr)
	name := "__test_reconnect__"

	if _, err := ensureTunnel(name, &dao.DsnRecord{
		SSHHost: host, SSHPort: atoiSafe(port), SSHUser: "tester",
		SSHAuth: "password", SSHPassword: "secret",
	}); err != nil {
		t.Fatalf("ensureTunnel: %v", err)
	}
	defer closeTunnel(name)

	// 第一次拨号应成功
	c1, err := dialViaTunnel(name, "127.0.0.1:3306")
	if err != nil {
		t.Fatalf("first dial failed: %v", err)
	}
	c1.Close()

	// 强制关闭底层 ssh client, 模拟断线
	tunMu.Lock()
	tunnels[name].client.Close()
	tunMu.Unlock()

	// 直接拨号此时应失败
	if _, err := dialViaTunnel(name, "127.0.0.1:3306"); err == nil {
		t.Log("warning: dial after close unexpectedly succeeded (race)")
	}

	// 经由 reconnectTunnel 自愈后应再次成功 (这正是 RegisterDialContext 包裹的逻辑)
	if _, err := reconnectTunnel(name); err != nil {
		t.Fatalf("reconnect failed: %v", err)
	}
	c2, err := dialViaTunnel(name, "127.0.0.1:3306")
	if err != nil {
		t.Fatalf("dial after reconnect failed: %v", err)
	}
	c2.Close()
}

// TestSSHWrongPassword 验证错误密码会导致连接失败。
func TestSSHWrongPassword(t *testing.T) {
	addr := startSSHServer(t)
	host, port, _ := net.SplitHostPort(addr)

	rec := &dao.DsnRecord{
		Driver:      "mysql",
		DSN:         "root:root@tcp(127.0.0.1:3306)/",
		SSHEnabled:  true,
		SSHHost:     host,
		SSHPort:     atoiSafe(port),
		SSHUser:     "tester",
		SSHAuth:     "password",
		SSHPassword: "wrong",
	}
	if err := PingRecord(rec); err == nil {
		t.Fatal("expected auth failure with wrong password, got nil")
	}
}

// TestSSHKeyAuth 验证私钥认证路径: 生成密钥对, 服务端授权对应公钥, 客户端用 PEM 私钥连接。
func TestSSHKeyAuth(t *testing.T) {
	if err := PingDSN("mysql", "root:root@tcp(127.0.0.1:3306)/?timeout=3s"); err != nil {
		t.Skipf("本地 MySQL 不可用, 跳过: %v", err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("new pubkey: %v", err)
	}
	pemBlock, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal priv: %v", err)
	}
	pemBytes := encodePEM(pemBlock)

	addr := startSSHServer(t, sshPub)
	host, port, _ := net.SplitHostPort(addr)

	rec := &dao.DsnRecord{
		Driver:     "mysql",
		DSN:        "root:root@tcp(127.0.0.1:3306)/?timeout=5s",
		SSHEnabled: true,
		SSHHost:    host,
		SSHPort:    atoiSafe(port),
		SSHUser:    "tester",
		SSHAuth:    "key",
		SSHKey:     string(pemBytes),
	}
	if err := PingRecord(rec); err != nil {
		t.Fatalf("PingRecord with SSH key auth failed: %v", err)
	}
}

func encodePEM(b *pem.Block) []byte {
	return pem.EncodeToMemory(b)
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
