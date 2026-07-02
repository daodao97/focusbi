package datasource

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/pem"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xproxy/conf"
	"xproxy/dao"

	"github.com/daodao97/xgo/xdb"
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

func startCountingSSHServer(t *testing.T, authDelay time.Duration) (string, *atomic.Int32) {
	hostKey := genHostKey(t)
	var accepted atomic.Int32
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if authDelay > 0 {
				time.Sleep(authDelay)
			}
			if string(pass) == "secret" {
				return &ssh.Permissions{}, nil
			}
			return nil, io.EOF
		},
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
			accepted.Add(1)
			go handleSSHConn(conn, cfg)
		}
	}()
	return ln.Addr().String(), &accepted
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

func pingDSN(driver, dsn string) error {
	db, err := sql.Open(normalizeDriver(driver), dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := contextTimeout()
	defer cancel()
	return db.PingContext(ctx)
}

// TestSSHTunnelPing 验证: 经 SSH 跳板机转发到本地 MySQL 的 PingRecord 能成功。
// 需要本地 127.0.0.1:3306 有可用 MySQL (root:root). 不可用时跳过。
func TestSSHTunnelPing(t *testing.T) {
	// 先确认本地 MySQL 直连可用, 否则跳过 (隧道目标不存在无法验证)。
	if err := pingDSN("mysql", "root:root@tcp(127.0.0.1:3306)/?timeout=3s"); err != nil {
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
	if err := pingDSN("mysql", "root:root@tcp(127.0.0.1:3306)/?timeout=3s"); err != nil {
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
	if err := pingDSN("mysql", "root:root@tcp(127.0.0.1:3306)/?timeout=3s"); err != nil {
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
	c1, err := dialViaTunnelContext(context.Background(), name, "127.0.0.1:3306")
	if err != nil {
		t.Fatalf("first dial failed: %v", err)
	}
	c1.Close()

	// 强制关闭底层 ssh client, 模拟断线
	tunMu.Lock()
	tunnels[name].client.Close()
	tunMu.Unlock()

	// 直接拨号此时应失败
	if _, err := dialViaTunnelContext(context.Background(), name, "127.0.0.1:3306"); err == nil {
		t.Log("warning: dial after close unexpectedly succeeded (race)")
	}

	// 经由 reconnectTunnel 自愈后应再次成功 (这正是 RegisterDialContext 包裹的逻辑)
	if _, err := reconnectTunnel(name); err != nil {
		t.Fatalf("reconnect failed: %v", err)
	}
	c2, err := dialViaTunnelContext(context.Background(), name, "127.0.0.1:3306")
	if err != nil {
		t.Fatalf("dial after reconnect failed: %v", err)
	}
	c2.Close()
}

// TestEnsureTunnelConcurrentSingleBuild 验证同一数据源的 tunnel 冷启动会合并并发构建。
// 旧实现会让多个 goroutine 同时 dial SSH, 后完成者关闭先完成者, 把已开始的查询打断。
func TestEnsureTunnelConcurrentSingleBuild(t *testing.T) {
	addr, accepted := startCountingSSHServer(t, 40*time.Millisecond)
	host, port, _ := net.SplitHostPort(addr)
	name := "__test_singlebuild__"
	defer closeTunnel(name)

	rec := &dao.DsnRecord{
		SSHHost: host, SSHPort: atoiSafe(port), SSHUser: "tester",
		SSHAuth: "password", SSHPassword: "secret",
	}

	const n = 12
	start := make(chan struct{})
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := ensureTunnel(name, rec)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("ensureTunnel failed: %v", err)
		}
	}
	if got := accepted.Load(); got != 1 {
		t.Fatalf("并发冷启动建立了 %d 条 ssh 连接, want 1", got)
	}
}

func TestSSHDataSourceCapsDBOpenConnections(t *testing.T) {
	addr := startSSHServer(t)
	host, port, _ := net.SplitHostPort(addr)
	const name = "__test_ssh_pool_cap__"
	defer Invalidate(name)
	defer Invalidate("default")

	conf.ConfInstance = &conf.Conf{Database: []xdb.Config{{
		Name: "default", Driver: "sqlite", DSN: "file:sshpoolcap?mode=memory&cache=shared",
	}}}
	if err := xdb.Inits(conf.Get().Database); err != nil {
		t.Fatalf("xdb init: %v", err)
	}
	if _, err := Query("default", `CREATE TABLE IF NOT EXISTS dsn(id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT, driver TEXT, dsn TEXT, remark TEXT, ssh_enabled INTEGER DEFAULT 0,
		ssh_host TEXT, ssh_port INTEGER DEFAULT 22, ssh_user TEXT, ssh_auth TEXT,
		ssh_password TEXT, ssh_key TEXT, ssh_key_passphrase TEXT,
		created_at DATETIME, updated_at DATETIME)`); err != nil {
		t.Fatalf("create dsn table: %v", err)
	}
	if _, err := Query("default", `INSERT INTO dsn(name, driver, dsn, ssh_enabled, ssh_host, ssh_port, ssh_user, ssh_auth, ssh_password)
		VALUES(?, 'mysql', 'root:root@tcp(127.0.0.1:3306)/', 1, ?, ?, 'tester', 'password', 'secret')`,
		name, host, atoiSafe(port)); err != nil {
		t.Fatalf("insert dsn: %v", err)
	}
	dao.Dsn = xdb.New("dsn")

	db, err := get(name)
	if err != nil {
		t.Fatalf("get ssh datasource: %v", err)
	}
	if got := db.Stats().MaxOpenConnections; got != sshMaxConcurrentDials {
		t.Fatalf("ssh datasource max open conns = %d, want %d", got, sshMaxConcurrentDials)
	}
}

// TestSSHTunnelDialConcurrencyCapped 验证: 经隧道并发开多个转发通道时,
// "开通道"动作被 dialSem 限流到 sshMaxConcurrentDials, 不会突发打爆跳板机连接
// (回归 "unexpected packet in response to channel open: <nil>")。
//
// 测的是【客户端并发】: dialViaTunnel 的信号量决定同时有多少个 client.Dial
// 在向服务端发 channel-open。服务端在 Accept 之前故意延迟, 让客户端 Dial 堆积,
// 用并发的通道处理 goroutine 统计"已收到 open 但尚未 Accept"的在途峰值。
// 去掉信号量时峰值≈并发拨号数(12), 加了信号量则≤上限。
func TestSSHTunnelDialConcurrencyCapped(t *testing.T) {
	// echo 目标 (direct-tcpip 落地端), 无需 MySQL。
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("target listen: %v", err)
	}
	defer target.Close()
	go func() {
		for {
			c, err := target.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(c, c); _ = c.Close() }()
		}
	}()

	var (
		mu      sync.Mutex
		inFlnow int
		peak    int
	)
	hostKey := genHostKey(t)
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if string(pass) == "secret" {
				return &ssh.Permissions{}, nil
			}
			return nil, io.EOF
		},
	}
	cfg.AddHostKey(hostKey)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(nConn net.Conn) {
				sshConn, chans, reqs, err := ssh.NewServerConn(nConn, cfg)
				if err != nil {
					return
				}
				defer sshConn.Close()
				go ssh.DiscardRequests(reqs)
				// 读取循环不阻塞, 立刻把每个通道交给并发 goroutine 处理。
				for newCh := range chans {
					go func(nc ssh.NewChannel) {
						// "收到 open 但还没 Accept" 区间: 客户端 Dial 此刻正阻塞等确认。
						mu.Lock()
						inFlnow++
						if inFlnow > peak {
							peak = inFlnow
						}
						mu.Unlock()
						time.Sleep(60 * time.Millisecond) // 延迟 Accept, 让客户端拨号堆积
						mu.Lock()
						inFlnow--
						mu.Unlock()
						handleDirectTCPIP(nc) // 内部 Accept 并转发
					}(newCh)
				}
			}(conn)
		}
	}()

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	name := "__test_dialcap__"
	if _, err := ensureTunnel(name, &dao.DsnRecord{
		SSHHost: host, SSHPort: atoiSafe(port), SSHUser: "tester",
		SSHAuth: "password", SSHPassword: "secret",
	}); err != nil {
		t.Fatalf("ensureTunnel: %v", err)
	}
	defer closeTunnel(name)

	// 并发发起远多于上限的拨号 (模拟引擎 8 并发查询同时开通道)。
	const dials = 12
	var wg sync.WaitGroup
	for i := 0; i < dials; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := dialViaTunnelContext(context.Background(), name, target.Addr().String())
			if err == nil {
				_ = c.Close()
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	gotPeak := peak
	mu.Unlock()
	if gotPeak == 0 {
		t.Fatal("未观测到任何 channel-open, 测试无效")
	}
	if gotPeak > sshMaxConcurrentDials {
		t.Fatalf("同时在途 channel-open 峰值 %d 超过上限 %d, 限流未生效", gotPeak, sshMaxConcurrentDials)
	}
}

// TestQueryDoesNotInvalidateSharedPool 是核心回归测试: Query 撞到坏连接 (EOF) 时
// 【不再关闭共享 cache 连接池】。旧版坏连接判断 -> Invalidate(name) 会
// db.Close()+delete 整个池, 并发下把正在用该池的其它查询一起打断 (unexpected EOF 雪崩)。
//
// 用一个自定义 database/sql 驱动确定性复刻: 对 "BADCONN" 查询返回 io.EOF
// (模拟旧版坏连接判断会命中的错误), 其余正常。并发执行, 断言:
//   - 共享池指针在并发期间保持不变 (池从未被 Invalidate 拆掉);
//   - 正常查询不被连累。
//
// 旧版代码下, EOF 查询会 Invalidate 整个池 -> 指针被换 -> 断言 1 失败。
func TestQueryDoesNotInvalidateSharedPool(t *testing.T) {
	const name = "probe"
	conf.ConfInstance = &conf.Conf{Database: []xdb.Config{{
		Name: "default", Driver: "sqlite", DSN: "file:dsnoinval?mode=memory&cache=shared",
	}}}
	if err := xdb.Inits(conf.Get().Database); err != nil {
		t.Fatalf("xdb init: %v", err)
	}
	if _, err := Query("default", `CREATE TABLE IF NOT EXISTS dsn(id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT, driver TEXT, dsn TEXT, remark TEXT, ssh_enabled INTEGER DEFAULT 0,
		ssh_host TEXT, ssh_port INTEGER DEFAULT 22, ssh_user TEXT, ssh_auth TEXT,
		ssh_password TEXT, ssh_key TEXT, ssh_key_passphrase TEXT,
		created_at DATETIME, updated_at DATETIME)`); err != nil {
		t.Fatalf("create dsn table: %v", err)
	}
	// probe 数据源用自定义 "eofdrv" 驱动 (normalizeDriver 原样透传未知驱动名)。
	if _, err := Query("default", `INSERT INTO dsn(name, driver, dsn) VALUES(?, 'eofdrv', 'whatever')`, name); err != nil {
		t.Fatalf("insert dsn: %v", err)
	}
	dao.Dsn = xdb.New("dsn")
	defer Invalidate(name)

	// 预热建池, 记录共享池指针。
	if _, err := Query(name, "SELECT 1"); err != nil {
		t.Fatalf("预热: %v", err)
	}
	mu.Lock()
	poolBefore := cache[name].db
	mu.Unlock()

	const n = 8
	var wg sync.WaitGroup
	okErrs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%4 == 0 {
				_, _ = Query(name, "BADCONN") // 返回 EOF, 模拟旧版坏连接判断会命中
				return
			}
			_, okErrs[idx] = Query(name, "SELECT 1") // 正常查询
		}(i)
	}
	wg.Wait()

	// 断言 1 (核心): 共享池指针未变 —— 旧版 Invalidate 会 Close+delete -> 指针被换。
	mu.Lock()
	poolAfter := cache[name].db
	mu.Unlock()
	if poolAfter != poolBefore {
		t.Fatalf("共享连接池在坏连接后被替换, 说明仍在 Invalidate 整个池 (回归未修复)")
	}
	for i, e := range okErrs {
		if e != nil {
			t.Errorf("正常并发查询 #%d 失败 (不应被连累): %v", i, e)
		}
	}
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
	if err := pingDSN("mysql", "root:root@tcp(127.0.0.1:3306)/?timeout=3s"); err != nil {
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
