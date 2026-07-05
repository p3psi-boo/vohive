//go:build linux

// Package runtimecore 实现 VoWiFi 运行时核心协调器。
// 负责 orchestrating IKEv2 -> IPsec -> IMS 注册 -> 语音/短信服务。
package runtimecore

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/driver"
	"github.com/iniwex5/vowifi-go/engine/eap"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
	"github.com/iniwex5/vowifi-go/engine/logger"
	"github.com/iniwex5/vowifi-go/internal/vowifi/dns"
	"github.com/iniwex5/vowifi-go/internal/vowifi/epdg"
	"github.com/iniwex5/vowifi-go/runtimehost"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
)

type Phase string

const (
	PhaseInit      Phase = "init"
	PhaseIKE       Phase = "ike"
	PhaseIPsec     Phase = "ipsec"
	PhaseIMS       Phase = "ims"
	PhaseReady     Phase = "ready"
	PhaseFailed    Phase = "failed"
	PhaseStopped   Phase = "stopped"
)

type Core struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc

	deviceID  string
	traceID   string
	mcc       string
	mnc       string
	imsi      string
	epdgHost  string
	proxyAddr string
	phase     Phase
	state     runtimehost.State

	session *ikev2.Session
	childsa *ipsec.SA
	tun     *driver.TUN
	conn    net.Conn

	aka   eap.AKAProvider
	obs   []runtimehost.Observer
	store messaging.DeliveryStore
	disp  eventhost.Dispatcher

	encKey   []byte
	integKey []byte
}

type Config struct {
	DeviceID     string
	TraceID      string
	MCC          string
	MNC          string
	IMSI         string
	EPDGOverride string
	AKA          eap.AKAProvider
	Store        messaging.DeliveryStore
	Dispatch     eventhost.Dispatcher
	Proxy        string
}

func New(ctx context.Context, cfg *Config) *Core {
	ctx, cancel := context.WithCancel(ctx)
	return &Core{
		ctx:       ctx,
		cancel:    cancel,
		deviceID:  cfg.DeviceID,
		traceID:   cfg.TraceID,
		mcc:       cfg.MCC,
		mnc:       cfg.MNC,
		imsi:      cfg.IMSI,
		epdgHost:  cfg.EPDGOverride,
		proxyAddr: cfg.Proxy,
		aka:       cfg.AKA,
		store:     cfg.Store,
		disp:     cfg.Dispatch,
		phase:    PhaseInit,
	}
}

func (c *Core) Phase() Phase {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.phase
}

func (c *Core) State() runtimehost.State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Core) AddObserver(obs runtimehost.Observer) {
	c.mu.Lock()
	c.obs = append(c.obs, obs)
	c.mu.Unlock()
}

func (c *Core) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ikePhase(); err != nil {
		c.phase = PhaseFailed
		return fmt.Errorf("ike phase: %w", err)
	}
	if err := c.ipsecPhase(); err != nil {
		c.phase = PhaseFailed
		return fmt.Errorf("ipsec phase: %w", err)
	}
	c.phase = PhaseReady
	c.emitState()
	return nil
}

func socks5UDPAssociate(proxyAddr, targetAddr string) (net.Conn, error) {
	tcpConn, err := (&net.Dialer{Timeout: 5 * time.Second}).Dial("tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("socks5 tcp connect: %w", err)
	}
	_, _ = tcpConn.Write([]byte{0x05, 0x01, 0x00})
	resp := make([]byte, 2)
	if _, err := io.ReadFull(tcpConn, resp); err != nil || resp[0] != 0x05 || resp[1] != 0x00 {
		tcpConn.Close()
		return nil, fmt.Errorf("socks5 handshake failed: %x", resp)
	}
	host, portStr, _ := net.SplitHostPort(targetAddr)
	port, _ := strconv.Atoi(portStr)
	ip := net.ParseIP(host).To4()
	if ip == nil {
		ips, lookupErr := net.LookupIP(host)
		if lookupErr != nil || len(ips) == 0 {
			tcpConn.Close()
			return nil, fmt.Errorf("socks5: cannot resolve %s: %w", host, lookupErr)
		}
		for _, resolved := range ips {
			ip = resolved.To4()
			if ip != nil {
				break
			}
		}
		if ip == nil {
			tcpConn.Close()
			return nil, fmt.Errorf("socks5: no IPv4 address for %s", host)
		}
	}
	req := []byte{0x05, 0x03, 0x00, 0x01}
	req = append(req, 0, 0, 0, 0)
	req = append(req, 0, 0)
	// 目标地址 (ePDG)
	req = append(req, ip...)
	req = append(req, byte(port>>8), byte(port&0xff))
	if _, err := tcpConn.Write(req); err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("socks5 udp associate: %w", err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(tcpConn, reply); err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("socks5 udp associate reply: %w", err)
	}
	if reply[1] != 0x00 {
		tcpConn.Close()
		return nil, fmt.Errorf("socks5 udp associate failed: code=%d", reply[1])
	}
	relayIP := net.IP(reply[4:8])
	relayPort := int(reply[8])<<8 | int(reply[9])
	logger.Info("socks5 UDP relay", "relay_addr", fmt.Sprintf("%s:%d", relayIP.String(), relayPort))
	relayAddr := &net.UDPAddr{IP: relayIP, Port: relayPort}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("socks5 udp listen: %w", err)
	}
	udpHeader := make([]byte, 10)
	udpHeader[0] = 0x00
	udpHeader[1] = 0x00
	udpHeader[2] = 0x00
	udpHeader[3] = 0x01
	copy(udpHeader[4:8], ip)
	udpHeader[8] = byte(port >> 8)
	udpHeader[9] = byte(port & 0xff)

	wrap := &socks5UDPConn{
		relay:     tcpConn,
		conn:      conn,
		header:    udpHeader,
		relayAddr: relayAddr,
		rawConn:   true,
	}
	go func() {
		defer tcpConn.Close()
		io.Copy(io.Discard, tcpConn)
	}()
	return wrap, nil
}

type socks5UDPConn struct {
	relay     net.Conn
	conn      *net.UDPConn
	header    []byte
	relayAddr *net.UDPAddr
	rawConn   bool
}

func (c *socks5UDPConn) Read(b []byte) (int, error) {
	n, _, err := c.conn.ReadFromUDP(b)
	if err != nil {
		return n, err
	}
	if n >= 10 && b[0] == 0x00 && b[1] == 0x00 && b[2] == 0x00 {
		copy(b, b[10:n])
		return n - 10, nil
	}
	return n, nil
}

func (c *socks5UDPConn) Write(b []byte) (int, error) {
	pkt := make([]byte, len(c.header)+len(b))
	copy(pkt, c.header)
	copy(pkt[len(c.header):], b)
	_, err := c.conn.WriteToUDP(pkt, c.relayAddr)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *socks5UDPConn) Close() error {
	c.conn.Close()
	if c.relay != nil {
		c.relay.Close()
	}
	return nil
}

func (c *socks5UDPConn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *socks5UDPConn) RemoteAddr() net.Addr                { return c.relayAddr }
func (c *socks5UDPConn) SetDeadline(t time.Time) error       { return c.conn.SetDeadline(t) }
func (c *socks5UDPConn) SetReadDeadline(t time.Time) error   { return c.conn.SetReadDeadline(t) }
func (c *socks5UDPConn) SetWriteDeadline(t time.Time) error  { return c.conn.SetWriteDeadline(t) }

func (c *Core) ikePhase() error {
	c.phase = PhaseIKE
	dnsResolver := dns.NewResolver()
	sel := epdg.NewSelector(dnsResolver)
	ep, err := sel.Select(c.ctx, c.mcc, c.mnc, c.epdgHost)
	if err != nil {
		return fmt.Errorf("epdg select: %w", err)
	}
	if ep == nil || strings.TrimSpace(ep.Host) == "" {
		return fmt.Errorf("epdg: no host resolved for MCC=%s MNC=%s", c.mcc, c.mnc)
	}

	dnsStatus, err := sel.Resolve(c.ctx, ep)
	if err != nil {
		logger.Warn("epdg DNS resolve warning", "host", ep.Host, "err", err)
	}
	if dnsStatus != nil && len(dnsStatus.Addresses) > 0 {
		logger.Info("epdg DNS resolved", "host", ep.Host, "ips", dnsStatus.Addresses)
	}

	ports := []int{ikev2.DefaultPort, ikev2.NatTPort}
	var conn net.Conn
	var lastErr error

	tryConnect := func(ip string, port int) bool {
		addr := net.JoinHostPort(ip, fmt.Sprintf("%d", port))
		conn, lastErr = (&net.Dialer{Timeout: 5 * time.Second}).DialContext(c.ctx, "udp", addr)
		return lastErr == nil
	}

	// 先用 DNS 解析到的 IP
	if dnsStatus != nil && len(dnsStatus.Addresses) > 0 {
		for _, ip := range dnsStatus.Addresses {
			for _, port := range ports {
				logger.Info("epdg trying", "ip", ip.String(), "port", port)
				if tryConnect(ip.String(), port) {
					goto connected
				}
			}
		}
	}
	// fallback: 直接用 FQDN
	for _, port := range ports {
		logger.Info("epdg trying", "fqdn", ep.Host, "port", port)
		if tryConnect(ep.Host, port) {
			goto connected
		}
	}
	return fmt.Errorf("epdg connect: all ports/IPs failed: %w", lastErr)

connected:
	if conn == nil {
		return fmt.Errorf("epdg connect: all ports failed: %w", lastErr)
	}
	c.conn = conn
	cfg := ikev2.DefaultConfig()
	cfg.LocalIdentity = fmt.Sprintf("0%s@nai.epc.mnc%s.mcc%s.3gppnetwork.org", c.imsi, fmt.Sprintf("%03s", c.mnc), c.mcc)
	sess := ikev2.NewSession(cfg, conn, c.aka)
	ikesa, err := sess.Negotiate(c.ctx)
	if err != nil {
		return fmt.Errorf("ike negotiate: %w", err)
	}
	c.session = sess
	c.encKey = ikesa.Keys.SK_ei[:16]
	c.integKey = ikesa.Keys.SK_ai[:16]
	logger.Info("IKEv2 SA established", "device", c.deviceID, "epdg", ep.Host)
	return nil
}

func (c *Core) ipsecPhase() error {
	c.phase = PhaseIPsec
	if c.session == nil {
		return fmt.Errorf("no IKE session")
	}
	ts := []ikev2.TrafficSelector{
		{TSType: 7, IPProto: 0, StartAddr: []byte{0, 0, 0, 0}, EndAddr: []byte{255, 255, 255, 255}},
	}
	childsa, err := c.session.CreateChildSA(c.ctx, ts)
	if err != nil {
		return fmt.Errorf("create child sa: %w", err)
	}
	c.childsa = ipsec.NewSA(childsa.SPI, c.encKey, c.integKey)
	tun, err := driver.New(fmt.Sprintf("vowifi-%s", c.deviceID), 1500)
	if err != nil {
		return fmt.Errorf("create tun: %w", err)
	}
	c.tun = tun
	logger.Info("IPsec ESP Child SA created", "device", c.deviceID, "spi", childsa.SPI)
	return nil
}

func (c *Core) HandleIPPacket(packet []byte) error {
	if c.childsa == nil {
		return fmt.Errorf("no child SA")
	}
	seq, _ := c.childsa.AllocateSequence()
	enc, err := ipsec.Encrypt(c.childsa.SPI, seq, packet, 4, c.encKey, c.integKey)
	if err != nil {
		return fmt.Errorf("esp encrypt: %w", err)
	}
	if c.conn != nil {
		_, err = c.conn.Write(enc)
	}
	return err
}

func (c *Core) ReceiveESP(raw []byte) ([]byte, error) {
	if c.childsa == nil {
		return nil, fmt.Errorf("no child SA")
	}
	inner, _, err := ipsec.Decrypt(raw, c.encKey, c.integKey)
	if err != nil {
		return nil, fmt.Errorf("esp decrypt: %w", err)
	}
	return inner, nil
}

func (c *Core) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.phase = PhaseStopped
	if c.tun != nil {
		c.tun.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
	c.cancel()
	c.emitState()
	return nil
}

func (c *Core) emitState() {
	st := runtimehost.State{
		Phase:    runtimehost.Phase(string(c.phase)),
		DeviceID: c.deviceID,
	}
	c.state = st
	for _, obs := range c.obs {
		obs.OnRuntimeHostEvent(c.ctx, runtimehost.Event{
			Kind:     runtimehost.EventKindStateChange,
			State:    st,
			DeviceID: c.deviceID,
			Time:     time.Now(),
		})
	}
}

func (c *Core) RegenerateChildSA() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	encKey, _ := crypto.GenerateRandom(16)
	integKey, _ := crypto.GenerateRandom(16)
	spiData, _ := crypto.GenerateRandom(4)
	spi := binary.BigEndian.Uint32(spiData)
	if spi == 0 {
		spi = 1
	}
	c.encKey = encKey
	c.integKey = integKey
	c.childsa = ipsec.NewSA(spi, encKey, integKey)
	logger.Info("Child SA regenerated", "device", c.deviceID, "spi", spi)
	return nil
}
