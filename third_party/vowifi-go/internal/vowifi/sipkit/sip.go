// Package sipkit 提供 SIP (Session Initiation Protocol) 实现。
// 用于 IMS 注册、SMS over IP 信令，实现 RFC 3261 / RFC 2617 / 3GPP TS 24.229。
package sipkit

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/engine/logger"
)

// ---------- 类型定义 ----------

type Transport string

const (
	TransportUDP Transport = "udp"
	TransportTCP Transport = "tcp"
	TransportTLS Transport = "tls"
)

type Method string

const (
	MethodRegister  Method = "REGISTER"
	MethodInvite    Method = "INVITE"
	MethodAck       Method = "ACK"
	MethodBye       Method = "BYE"
	MethodCancel    Method = "CANCEL"
	MethodMessage   Method = "MESSAGE"
	MethodNotify    Method = "NOTIFY"
	MethodSubscribe Method = "SUBSCRIBE"
	MethodOptions   Method = "OPTIONS"
)

type ResponseCode int

const (
	StatusOK                ResponseCode = 200
	StatusAccepted          ResponseCode = 202
	StatusUnauthorized      ResponseCode = 401
	StatusProxyAuthRequired ResponseCode = 407
	StatusRequestTimeout    ResponseCode = 408
	StatusServerInternalErr ResponseCode = 500
	StatusServiceUnavailable ResponseCode = 503
)

type URI struct {
	Scheme   string
	User     string
	Host     string
	Port     int
	Params   string
	IsPhone  bool
}

func (u URI) String() string {
	var sb strings.Builder
	sb.WriteString(u.Scheme)
	sb.WriteString(":")
	if u.User != "" {
		sb.WriteString(u.User)
		sb.WriteString("@")
	}
	sb.WriteString(u.Host)
	if u.Port > 0 {
		sb.WriteString(fmt.Sprintf(":%d", u.Port))
	}
	if u.IsPhone {
		sb.WriteString(";user=phone")
	}
	if u.Params != "" {
		sb.WriteString(u.Params)
	}
	return sb.String()
}

type Header struct {
	Name  string
	Value string
}

type Request struct {
	Method  Method
	URI     string
	Headers []Header
	Body    []byte
}

type Response struct {
	StatusLine string
	Code       ResponseCode
	Reason     string
	Headers    []Header
	Body       []byte
}

type Dialog struct {
	CallID    string
	LocalTag  string
	RemoteTag string
	LocalSeq  uint32
	RemoteSeq uint32
	State     string
}

// ---------- 传输与客户端 ----------

type Client struct {
	mu        sync.Mutex
	transport Transport
	proxyAddr string
	conn      net.Conn
	udpConn   *net.UDPConn
	readBuf   bytes.Buffer
	dialogs    map[string]*Dialog
	cseq      uint32
	localAddr string
	remoteAddr string
}

func NewClient(transport Transport, proxyAddr string) *Client {
	return &Client{
		transport: transport,
		proxyAddr: proxyAddr,
		dialogs:   make(map[string]*Dialog),
		cseq:      1,
	}
}

func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connectLocked(ctx)
}

func (c *Client) connectLocked(ctx context.Context) error {
	if c.conn != nil || c.udpConn != nil {
		return nil
	}
	if c.transport == TransportUDP {
		addr, err := net.ResolveUDPAddr("udp", c.proxyAddr)
		if err != nil {
			return fmt.Errorf("sip resolve udp: %w", err)
		}
		conn, err := net.DialUDP("udp", nil, addr)
		if err != nil {
			return fmt.Errorf("sip connect udp: %w", err)
		}
		c.udpConn = conn
		c.remoteAddr = addr.String()
		return nil
	}
	dialer := net.Dialer{Timeout: 8 * time.Second}
	conn, err := dialer.DialContext(ctx, string(c.transport), c.proxyAddr)
	if err != nil {
		return fmt.Errorf("sip connect: %w", err)
	}
	c.conn = conn
	c.remoteAddr = conn.RemoteAddr().String()
	if la := conn.LocalAddr(); la != nil {
		c.localAddr = la.String()
	}
	return nil
}

func (c *Client) Send(ctx context.Context, req *Request) (*Response, error) {
	return c.sendInternal(ctx, req, true)
}

func (c *Client) SendRaw(ctx context.Context, req *Request) (*Response, error) {
	return c.sendInternal(ctx, req, false)
}

func (c *Client) sendInternal(ctx context.Context, req *Request, autoHeaders bool) (*Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.connectLocked(ctx); err != nil {
		return nil, err
	}

	if req.Headers == nil {
		req.Headers = make([]Header, 0, 10)
	}

	if autoHeaders {
		c.cseq++
		hasFrom := false
		hasTo := false
		hasCallID := false
		hasVia := false
		hasMaxFwd := false
		hasCSeq := false
		hasContentLength := false
		for _, h := range req.Headers {
			switch strings.ToLower(h.Name) {
			case "from":
				hasFrom = true
			case "to":
				hasTo = true
			case "call-id":
				hasCallID = true
			case "via":
				hasVia = true
			case "max-forwards":
				hasMaxFwd = true
			case "cseq":
				hasCSeq = true
			case "content-length":
				hasContentLength = true
			}
		}
		if !hasVia {
			branch := "z9hG4bK" + hexToken(12)
			via := fmt.Sprintf("SIP/2.0/%s %s;branch=%s;rport", strings.ToUpper(string(c.transport)), c.localAddrOrHost(), branch)
			req.Headers = append([]Header{{Name: "Via", Value: via}}, req.Headers...)
		}
		if !hasMaxFwd {
			req.Headers = append(req.Headers, Header{Name: "Max-Forwards", Value: "70"})
		}
		if !hasFrom {
			req.Headers = append(req.Headers, Header{Name: "From", Value: fmt.Sprintf("<sip:unknown@%s>;tag=%s", c.localAddrOrHost(), hexToken(8))})
		}
		if !hasTo {
			req.Headers = append(req.Headers, Header{Name: "To", Value: fmt.Sprintf("<%s>", req.URI)})
		}
		if !hasCallID {
			req.Headers = append(req.Headers, Header{Name: "Call-ID", Value: fmt.Sprintf("%s@%s", hexToken(16), c.localAddrOrHost())})
		}
		if !hasCSeq {
			req.Headers = append(req.Headers, Header{Name: "CSeq", Value: fmt.Sprintf("%d %s", c.cseq, req.Method)})
		}
		if !hasContentLength {
			if bodyLen := len(req.Body); bodyLen > 0 {
				req.Headers = append(req.Headers, Header{Name: "Content-Length", Value: strconv.Itoa(bodyLen)})
			} else {
				req.Headers = append(req.Headers, Header{Name: "Content-Length", Value: "0"})
			}
		}
	}

	raw := c.serializeRequest(req)
	logger.Debug("SIP request serialized", "method", string(req.Method), "size", len(raw))

	if err := c.writeRaw(raw); err != nil {
		return nil, fmt.Errorf("sip send: %w", err)
	}
	logger.Info("SIP request sent", "method", string(req.Method), "cseq", c.cseq)

	respRaw, err := c.readResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("sip read response: %w", err)
	}
	return ParseResponse(string(respRaw))
}

func (c *Client) writeRaw(data []byte) error {
	if c.udpConn != nil {
		_, err := c.udpConn.Write(data)
		return err
	}
	if c.conn != nil {
		_, err := c.conn.Write(data)
		return err
	}
	return errors.New("sip: not connected")
}

func (c *Client) readResponse(ctx context.Context) ([]byte, error) {
	buf := make([]byte, 65536)
	var readFunc func([]byte) (int, error)
	if c.udpConn != nil {
		readFunc = func(b []byte) (int, error) {
			c.udpConn.SetReadDeadline(time.Now().Add(8 * time.Second))
			n, _, err := c.udpConn.ReadFrom(b)
			return n, err
		}
	} else if c.conn != nil {
		readFunc = func(b []byte) (int, error) {
			c.conn.SetReadDeadline(time.Now().Add(8 * time.Second))
			return c.conn.Read(b)
		}
	} else {
		return nil, errors.New("sip: not connected")
	}

	var result bytes.Buffer
	for {
		n, err := readFunc(buf)
		if n > 0 {
			result.Write(buf[:n])
			if data := result.Bytes(); sipFrameComplete(data) {
				return data, nil
			}
		}
		if err != nil {
			if result.Len() > 0 {
				return result.Bytes(), nil
			}
			if errors.Is(err, io.EOF) {
				return result.Bytes(), nil
			}
			return nil, err
		}
		if result.Len() > 64*1024 {
			return nil, fmt.Errorf("sip response too large")
		}
	}
}

func (c *Client) LocalAddr() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.localAddr
}

func (c *Client) localAddrOrHost() string {
	if c.localAddr != "" {
		host, _, _ := net.SplitHostPort(c.localAddr)
		if host != "" {
			return host
		}
	}
	return "0.0.0.0"
}

func (c *Client) Register(ctx context.Context, impi, impu, domain, password string) (*Response, error) {
	req := &Request{
		Method: MethodRegister,
		URI:    fmt.Sprintf("sip:%s", domain),
		Headers: []Header{
			{Name: "From", Value: fmt.Sprintf("<%s>;tag=%s", impu, hexToken(8))},
			{Name: "To", Value: fmt.Sprintf("<%s>", impu)},
			{Name: "Contact", Value: fmt.Sprintf("<sip:%s@%s;transport=%s>", impi, c.localAddrOrHost(), string(c.transport))},
			{Name: "Expires", Value: "3600"},
			{Name: "Allow", Value: "INVITE,ACK,CANCEL,BYE,MESSAGE,NOTIFY,INFO,OPTIONS"},
		},
	}
	resp, err := c.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("sip register: %w", err)
	}
	if resp.Code == StatusUnauthorized || resp.Code == StatusProxyAuthRequired {
		challenge := ParseDigestChallenge(resp)
		if challenge == nil {
			return resp, nil
		}
		digestURI := fmt.Sprintf("sip:%s", domain)
		cnonce := hexToken(8)
		digestResp := ComputeDigestResponse(impi, domain, password, digestURI, "REGISTER", challenge.Nonce, challenge.Qop, cnonce)
		authHeader := buildAuthorizationHeader(challenge, impi, domain, digestURI, digestResp, cnonce)
		authReq := &Request{
			Method: MethodRegister,
			URI:    fmt.Sprintf("sip:%s", domain),
			Headers: []Header{
				{Name: "From", Value: fmt.Sprintf("<%s>;tag=%s", impu, hexToken(8))},
				{Name: "To", Value: fmt.Sprintf("<%s>", impu)},
				{Name: "Contact", Value: fmt.Sprintf("<sip:%s@%s;transport=%s>", impi, c.localAddrOrHost(), string(c.transport))},
				{Name: "Expires", Value: "3600"},
				{Name: authHeader.Name, Value: authHeader.Value},
				{Name: "Allow", Value: "INVITE,ACK,CANCEL,BYE,MESSAGE,NOTIFY,INFO,OPTIONS"},
			},
		}
		return c.Send(ctx, authReq)
	}
	return resp, nil
}

func (c *Client) SendMessage(ctx context.Context, from, to, body string) (*Response, error) {
	callID := fmt.Sprintf("%s@%s", hexToken(16), c.localAddrOrHost())
	req := &Request{
		Method: MethodMessage,
		URI:    to,
		Headers: []Header{
			{Name: "From", Value: fmt.Sprintf("<%s>;tag=%s", from, hexToken(8))},
			{Name: "To", Value: fmt.Sprintf("<%s>", to)},
			{Name: "Call-ID", Value: callID},
			{Name: "Content-Type", Value: "application/vnd.3gpp.sms"},
			{Name: "P-Access-Network-Info", Value: "3GPP-E-UTRAN;utran-cell-id-3gpp=00000000"},
		},
		Body: []byte(body),
	}
	return c.Send(ctx, req)
}

func (c *Client) serializeRequest(req *Request) []byte {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s %s SIP/2.0\r\n", req.Method, req.URI))
	for _, h := range req.Headers {
		sb.WriteString(fmt.Sprintf("%s: %s\r\n", h.Name, h.Value))
	}
	sb.WriteString("\r\n")
	if len(req.Body) > 0 {
		sb.Write(req.Body)
	}
	return []byte(sb.String())
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var errs []error
	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			errs = append(errs, err)
		}
		c.conn = nil
	}
	if c.udpConn != nil {
		if err := c.udpConn.Close(); err != nil {
			errs = append(errs, err)
		}
		c.udpConn = nil
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// ---------- SIP 响应解析 ----------

func ParseResponse(raw string) (*Response, error) {
	raw = strings.TrimSpace(raw)
	idx := strings.Index(raw, "\r\n\r\n")
	if idx < 0 {
		idx = strings.Index(raw, "\n\n")
	}
	if idx < 0 {
		return nil, fmt.Errorf("sip: no header/body separator")
	}
	headerPart := raw[:idx]
	bodyPart := ""
	if idx+4 <= len(raw) {
		bodyPart = raw[idx+4:]
	} else {
		bodyPart = raw[idx+2:]
	}
	bodyPart = strings.TrimSpace(bodyPart)

	lines := strings.Split(headerPart, "\r\n")
	if len(lines) <= 1 {
		lines = strings.Split(headerPart, "\n")
	}

	if len(lines) == 0 {
		return nil, fmt.Errorf("sip: empty response")
	}

	statusLine := strings.TrimSpace(lines[0])
	parts := strings.SplitN(statusLine, " ", 3)
	if len(parts) < 2 || parts[0] != "SIP/2.0" {
		return &Response{StatusLine: statusLine, Code: 0, Reason: statusLine}, nil
	}

	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("sip: invalid status code %q: %w", parts[1], err)
	}
	reason := ""
	if len(parts) >= 3 {
		reason = parts[2]
	}

	var headers []Header
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if colonIdx := strings.Index(line, ":"); colonIdx > 0 {
			name := strings.TrimSpace(line[:colonIdx])
			value := strings.TrimSpace(line[colonIdx+1:])
			headers = append(headers, Header{Name: name, Value: value})
		}
	}

	return &Response{
		StatusLine: statusLine,
		Code:       ResponseCode(code),
		Reason:     reason,
		Headers:    headers,
		Body:       []byte(bodyPart),
	}, nil
}

func ParseRequest(raw string) (*Request, error) {
	idx := strings.Index(raw, "\r\n\r\n")
	if idx < 0 {
		idx = strings.Index(raw, "\n\n")
	}
	if idx < 0 {
		return nil, fmt.Errorf("sip: no header/body separator")
	}
	headerPart := raw[:idx]
	bodyPart := raw[idx+2:]

	lines := strings.Split(headerPart, "\r\n")
	if len(lines) <= 1 {
		lines = strings.Split(headerPart, "\n")
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("sip: empty request")
	}

	requestLine := strings.TrimSpace(lines[0])
	reqParts := strings.SplitN(requestLine, " ", 3)
	if len(reqParts) < 3 || reqParts[2] != "SIP/2.0" {
		return nil, fmt.Errorf("sip: invalid request line: %q", requestLine)
	}

	var headers []Header
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if colonIdx := strings.Index(line, ":"); colonIdx > 0 {
			name := strings.TrimSpace(line[:colonIdx])
			value := strings.TrimSpace(line[colonIdx+1:])
			headers = append(headers, Header{Name: name, Value: value})
		}
	}

	return &Request{
		Method:  Method(reqParts[0]),
		URI:     reqParts[1],
		Headers: headers,
		Body:    []byte(bodyPart),
	}, nil
}

func GetHeaderValue(resp *Response, name string) string {
	if resp == nil {
		return ""
	}
	for _, h := range resp.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func GetAllHeaderValues(resp *Response, name string) []string {
	if resp == nil {
		return nil
	}
	var vals []string
	for _, h := range resp.Headers {
		if strings.EqualFold(h.Name, name) {
			vals = append(vals, h.Value)
		}
	}
	return vals
}

// ---------- Digest 认证 ----------

type DigestChallenge struct {
	HeaderKind string // "www-authenticate" or "proxy-authenticate"
	Realm      string
	Nonce      string
	Algorithm  string
	Qop        string
	Opaque     string
	Stale      bool

	Rand []byte // 前 16 字节 nonce (AKA challenge)
	Autn []byte // 后 16 字节 nonce (AKA challenge)

	SecurityServerHeaders []string
	SecurityServerOffers  []SecurityServerOffer
}

type SecurityServerOffer struct {
	Raw       string
	Alg       string
	Ealg      string
	Protocol  string
	Mode      string
	SPIC      uint32
	SPIS      uint32
	PortC     uint16
	PortS     uint16
	QMilli    uint16
}

func ParseDigestChallenge(resp *Response) *DigestChallenge {
	if resp == nil {
		return nil
	}
	challenge := &DigestChallenge{}

	for _, h := range resp.Headers {
		name := strings.ToLower(h.Name)
		switch name {
		case "www-authenticate":
			parseDigestParamsInto(h.Value, challenge, "www-authenticate")
			challenge.HeaderKind = "www-authenticate"
		case "proxy-authenticate":
			parseDigestParamsInto(h.Value, challenge, "proxy-authenticate")
			if challenge.HeaderKind == "" {
				challenge.HeaderKind = "proxy-authenticate"
			}
		case "security-server":
			challenge.SecurityServerHeaders = append(challenge.SecurityServerHeaders, h.Value)
			offers := parseSecurityServerOffers(h.Value)
			challenge.SecurityServerOffers = append(challenge.SecurityServerOffers, offers...)
		}
	}

	if challenge.HeaderKind == "" {
		return nil
	}
	if challenge.Algorithm == "" {
		challenge.Algorithm = "AKAv1-MD5"
	}

	nonceBytes := decodeDigestNonce(challenge.Nonce)
	if len(nonceBytes) >= 32 {
		challenge.Rand = nonceBytes[:16]
		challenge.Autn = nonceBytes[16:32]
	}
	return challenge
}

func parseDigestParamsInto(value string, ch *DigestChallenge, _ string) {
	value = strings.TrimSpace(value)
	if after, found := strings.CutPrefix(value, "Digest"); found {
		value = strings.TrimSpace(after)
	}
	params := splitDigestParamList(value)
	for _, param := range params {
		k, v, ok := strings.Cut(param, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(k))
		val := trimQuotes(strings.TrimSpace(v))
		switch key {
		case "realm":
			ch.Realm = val
		case "nonce":
			ch.Nonce = val
		case "algorithm":
			ch.Algorithm = val
		case "qop":
			ch.Qop = val
		case "opaque":
			ch.Opaque = val
		case "stale":
			ch.Stale = strings.EqualFold(val, "true")
		}
	}
}

func parseSecurityServerOffers(value string) []SecurityServerOffer {
	var offers []SecurityServerOffer
	values := splitHeaderValues(value)
	for _, v := range values {
		if offer := parseSecurityServerOffer(v); offer != nil {
			offers = append(offers, *offer)
		}
	}
	return offers
}

func parseSecurityServerOffer(v string) *SecurityServerOffer {
	v = strings.TrimSpace(v)
	parts := strings.Split(v, ";")
	if len(parts) < 6 {
		return nil
	}
	mechanism := strings.TrimSpace(parts[0])
	if !strings.EqualFold(mechanism, "ipsec-3gpp") {
		return nil
	}
	offer := &SecurityServerOffer{Raw: v}
	for _, part := range parts[1:] {
		part = strings.TrimSpace(part)
		k, val, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(k))
		value := trimQuotes(strings.TrimSpace(val))
		switch key {
		case "alg":
			offer.Alg = value
		case "ealg":
			offer.Ealg = value
		case "prot":
			offer.Protocol = value
		case "mod":
			offer.Mode = value
		case "spi-c":
			if spi, err := parseUint32(value); err == nil {
				offer.SPIC = spi
			}
		case "spi-s":
			if spi, err := parseUint32(value); err == nil {
				offer.SPIS = spi
			}
		case "port-c":
			if port, err := parseUint16(value); err == nil {
				offer.PortC = port
			}
		case "port-s":
			if port, err := parseUint16(value); err == nil {
				offer.PortS = port
			}
		case "q":
			offer.QMilli = parseQMilli(value)
		}
	}
	if offer.SPIC == 0 && offer.SPIS == 0 {
		return nil
	}
	return offer
}

func parseUint32(s string) (uint32, error) {
	v, err := strconv.ParseUint(s, 10, 32)
	return uint32(v), err
}

func parseUint16(s string) (uint16, error) {
	v, err := strconv.ParseUint(s, 10, 16)
	return uint16(v), err
}

func parseQMilli(s string) uint16 {
	if idx := strings.Index(s, "."); idx >= 0 {
		whole, _ := strconv.ParseUint(s[:idx], 10, 16)
		frac := s[idx+1:]
		if len(frac) > 3 {
			frac = frac[:3]
		}
		for len(frac) < 3 {
			frac += "0"
		}
		f, _ := strconv.ParseUint(frac, 10, 16)
		return uint16(whole*1000 + f)
	}
	v, _ := strconv.ParseUint(s, 10, 16)
	return uint16(v * 1000)
}

func ComputeDigestResponse(username, realm, password, digestURI, method, nonce, qop, cnonce string) string {
	return computeMD5Digest(username, realm, password, digestURI, method, nonce, qop, cnonce)
}

func ComputeAKAv1MD5Response(username, realm string, akaRes []byte, digestURI, method, nonce, qop, cnonce string) string {
	password := hex.EncodeToString(akaRes)
	return computeMD5Digest(username, realm, password, digestURI, method, nonce, qop, cnonce)
}

func ComputeAKAv2MD5Response(username, realm string, akaRes, ik, ck []byte, digestURI, method, nonce, qop, cnonce string) string {
	var keyBuf bytes.Buffer
	keyBuf.Write(akaRes)
	keyBuf.Write(ik)
	keyBuf.Write(ck)
	password := base64.StdEncoding.EncodeToString(hmacMD5(keyBuf.Bytes(), []byte("http-digest-akav2-password")))
	return computeMD5Digest(username, realm, password, digestURI, method, nonce, qop, cnonce)
}

func computeMD5Digest(username, realm, password, digestURI, method, nonce, qop, cnonce string) string {
	a1 := md5Hex(username + ":" + realm + ":" + password)
	a2 := md5Hex(method + ":" + digestURI)
	var proofInput string
	if qop == "auth" {
		proofInput = fmt.Sprintf("%s:%s:00000001:%s:auth:%s", a1, nonce, cnonce, a2)
	} else {
		proofInput = fmt.Sprintf("%s:%s:%s", a1, nonce, a2)
	}
	return md5Hex(proofInput)
}

func buildAuthorizationHeader(ch *DigestChallenge, username, realm, uri, response, cnonce string) Header {
	name := "Authorization"
	if ch.HeaderKind == "proxy-authenticate" {
		name = "Proxy-Authorization"
	}
	sb := &strings.Builder{}
	sb.WriteString(fmt.Sprintf(`Digest username="%s",realm="%s",nonce="%s",uri="%s",response="%s",algorithm=%s`,
		quoteSipParam(username),
		quoteSipParam(realm),
		quoteSipParam(ch.Nonce),
		quoteSipParam(uri),
		response,
		ch.Algorithm,
	))
	if ch.Qop == "auth" {
		sb.WriteString(fmt.Sprintf(`,qop=auth,nc=00000001,cnonce="%s"`, cnonce))
	}
	if ch.Opaque != "" {
		sb.WriteString(fmt.Sprintf(`,opaque="%s"`, quoteSipParam(ch.Opaque)))
	}
	return Header{Name: name, Value: sb.String()}
}

// ---------- SIP 请求构建器 ----------

type RegisterRequestConfig struct {
	IMPI              string
	IMPU              string
	Domain            string
	Realm             string
	Proxy             string
	LocalAddr         string
	Transport         Transport
	UserAgent         string
	PLMN              string
	MCC               string
	MNC               string
	IncludeRouteHeader   bool
	IncludeSecurityClient bool
	SecurityMechanism  string
	SPIC              uint32
	SPIS              uint32
	PortC             uint16
	PortS             uint16
	IncludePANI       bool
}

func BuildRegisterRequest(cfg RegisterRequestConfig, cseq uint32, authorization, securityVerify string) string {
	branch := "z9hG4bK" + hexToken(12)
	fromTag := hexToken(8)
	callID := hexToken(16) + "@simadmin"
	viaHost := extractIPFromAddr(cfg.LocalAddr)

	var sb strings.Builder
	requestURI := fmt.Sprintf("sip:%s", cfg.Domain)
	sb.WriteString(fmt.Sprintf("REGISTER %s SIP/2.0\r\n", requestURI))
	sb.WriteString(fmt.Sprintf("Via: SIP/2.0/%s %s:%d;branch=%s;rport\r\n",
		strings.ToUpper(string(cfg.Transport)), viaHost, 5060, branch))
	sb.WriteString("Max-Forwards: 70\r\n")
	sb.WriteString(fmt.Sprintf("From: <%s>;tag=%s\r\n", cfg.IMPU, fromTag))
	sb.WriteString(fmt.Sprintf("To: <%s>\r\n", cfg.IMPU))
	sb.WriteString(fmt.Sprintf("Call-ID: %s\r\n", callID))
	sb.WriteString(fmt.Sprintf("CSeq: %d REGISTER\r\n", cseq))

	if authorization != "" {
		sb.WriteString(authorization)
		sb.WriteString("\r\n")
	}

	contactUser := extractUser(cfg.IMPI)
	sb.WriteString(fmt.Sprintf("Contact: <sip:%s@%s:%d;transport=%s>",
		contactUser, viaHost, 5060, string(cfg.Transport)))
	sb.WriteString(";+g.3gpp.accesstype=\"IEEE-802.11\"")
	sb.WriteString(";+g.3gpp.smsip")
	sb.WriteString(";expires=3600\r\n")

	sb.WriteString("Expires: 3600\r\n")

	if cfg.IncludeRouteHeader && cfg.Proxy != "" {
		sb.WriteString(fmt.Sprintf("Route: <sip:%s;lr>\r\n", cfg.Proxy))
	}

	sb.WriteString("Allow: INVITE,ACK,CANCEL,BYE,MESSAGE,NOTIFY,INFO,OPTIONS\r\n")
	sb.WriteString("Supported: sec-agree\r\n")

	sb.WriteString(fmt.Sprintf("P-Preferred-Identity: <%s>\r\n", cfg.IMPU))

	visitedNet := fmt.Sprintf("ims.mnc%03s.mcc%s.3gppnetwork.org", cfg.MNC, cfg.MCC)
	sb.WriteString(fmt.Sprintf("P-Visited-Network-ID: \"%s\"\r\n", visitedNet))

	if cfg.IncludePANI {
		sb.WriteString("P-Access-Network-Info: IEEE-802.11;i-wlan-node-id=000000000000\r\n")
	}
	sb.WriteString(fmt.Sprintf("Cellular-Network-Info: 3GPP-E-UTRAN-FDD;utran-cell-id-3gpp=%s0000000;cell-info-age=0\r\n", cfg.PLMN))

	if cfg.IncludeSecurityClient {
		sb.WriteString(BuildSecurityClientHeader(cfg.SecurityMechanism, cfg.SPIC, cfg.SPIS, cfg.PortC, cfg.PortS))
		sb.WriteString("\r\n")
	}
	if securityVerify != "" {
		sb.WriteString(fmt.Sprintf("Security-Verify: %s\r\n", securityVerify))
	}

	if cfg.UserAgent != "" {
		sb.WriteString(fmt.Sprintf("User-Agent: %s\r\n", cfg.UserAgent))
	}
	sb.WriteString("Content-Length: 0\r\n\r\n")
	return sb.String()
}

func BuildSecurityClientHeader(mechanism string, spiC, spiS uint32, portC, portS uint16) string {
	parts := strings.Split(mechanism, "/")
	alg := "hmac-sha-1-96"
	ealg := "aes-cbc"
	protocol := "esp"
	mode := "trans"
	if len(parts) >= 4 {
		alg = parts[0]
		ealg = parts[1]
		protocol = parts[2]
		mode = parts[3]
	}
	return fmt.Sprintf("Security-Client: ipsec-3gpp; alg=%s; ealg=%s; prot=%s; mod=%s; spi-c=%d; spi-s=%d; port-c=%d; port-s=%d",
		alg, ealg, protocol, mode, spiC, spiS, portC, portS)
}

func BuildMessageRequestIms(from, to, route, userAgent, pani string, body []byte) string {
	branch := "z9hG4bK" + hexToken(12)
	callID := hexToken(16) + "@simadmin"
	fromTag := hexToken(8)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("MESSAGE %s SIP/2.0\r\n", to))
	host := extractIPFromAddr(route)
	if host == "" {
		host = "127.0.0.1"
	}
	sb.WriteString(fmt.Sprintf("Via: SIP/2.0/TCP %s:%d;branch=%s;rport\r\n", host, 5060, branch))
	sb.WriteString("Max-Forwards: 70\r\n")
	if route != "" {
		sb.WriteString(fmt.Sprintf("Route: <sip:%s;lr>\r\n", route))
	}
	sb.WriteString(fmt.Sprintf("From: <%s>;tag=%s\r\n", from, fromTag))
	sb.WriteString(fmt.Sprintf("To: <%s>\r\n", to))
	sb.WriteString(fmt.Sprintf("Call-ID: %s\r\n", callID))
	sb.WriteString(fmt.Sprintf("CSeq: 1 MESSAGE\r\n"))
	sb.WriteString(fmt.Sprintf("P-Preferred-Identity: <%s>\r\n", from))
	if pani != "" {
		sb.WriteString(fmt.Sprintf("P-Access-Network-Info: %s\r\n", pani))
	}
	sb.WriteString("Accept-Contact: *;+g.3gpp.smsip\r\n")
	if userAgent != "" {
		sb.WriteString(fmt.Sprintf("User-Agent: %s\r\n", userAgent))
	}
	sb.WriteString("Content-Type: application/vnd.3gpp.sms\r\n")
	sb.WriteString(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body)))
	sb.Write(body)
	return sb.String()
}

func BuildSipOKResponse(inRequest string) string {
	req, err := ParseRequest(inRequest)
	if err != nil {
		return "SIP/2.0 200 OK\r\nContent-Length: 0\r\n\r\n"
	}
	var sb strings.Builder
	sb.WriteString("SIP/2.0 200 OK\r\n")
	for _, h := range req.Headers {
		name := strings.ToLower(h.Name)
		switch name {
		case "via", "from", "call-id", "cseq":
			out := h.Value
			if name == "to" && !strings.Contains(strings.ToLower(h.Value), ";tag=") {
				out += ";tag=" + hexToken(8)
			}
			sb.WriteString(fmt.Sprintf("%s: %s\r\n", h.Name, out))
		}
	}
	sb.WriteString("Content-Length: 0\r\n\r\n")
	return sb.String()
}

// ---------- 工具函数 ----------

func sipFrameComplete(data []byte) bool {
	idx := bytes.Index(data, []byte("\r\n\r\n"))
	if idx < 0 {
		idx = bytes.Index(data, []byte("\n\n"))
	}
	if idx < 0 {
		return false
	}
	headerPart := string(data[:idx])
	cl := findContentLength(headerPart)
	if cl < 0 {
		return true
	}
	bodyStart := idx + 4
	return len(data) >= bodyStart+cl
}

func findContentLength(headers string) int {
	for _, line := range strings.Split(headers, "\n") {
		line = strings.TrimRight(line, "\r")
		if after, found := strings.CutPrefix(strings.ToLower(line), "content-length:"); found {
			if v, err := strconv.Atoi(strings.TrimSpace(after)); err == nil {
				return v
			}
		}
	}
	return -1
}

func splitHeaderValues(value string) []string {
	var values []string
	var current strings.Builder
	inQuote := false
	for _, ch := range value {
		switch {
		case ch == '"':
			inQuote = !inQuote
			current.WriteRune(ch)
		case ch == ',' && !inQuote:
			item := strings.TrimSpace(current.String())
			if item != "" {
				values = append(values, item)
			}
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}
	item := strings.TrimSpace(current.String())
	if item != "" {
		values = append(values, item)
	}
	return values
}

func splitDigestParamList(value string) []string {
	var items []string
	var current strings.Builder
	inQuote := false
	for _, ch := range value {
		switch {
		case ch == '\\' && inQuote:
			current.WriteRune(ch)
		case ch == '"':
			inQuote = !inQuote
			current.WriteRune(ch)
		case ch == ',' && !inQuote:
			item := strings.TrimSpace(current.String())
			if item != "" {
				items = append(items, item)
			}
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}
	item := strings.TrimSpace(current.String())
	if item != "" {
		items = append(items, item)
	}
	return items
}

func trimQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func md5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", h[:])
}

func hmacMD5(key, data []byte) []byte {
	const blockSize = 64
	var normKey [blockSize]byte
	if len(key) > blockSize {
		h := md5.Sum(key)
		copy(normKey[:], h[:])
	} else {
		copy(normKey[:], key)
	}

	var ipad, opad [blockSize]byte
	for i := 0; i < blockSize; i++ {
		ipad[i] = normKey[i] ^ 0x36
		opad[i] = normKey[i] ^ 0x5C
	}

	inner := md5.New()
	inner.Write(ipad[:])
	inner.Write(data)
	innerHash := inner.Sum(nil)

	outer := md5.New()
	outer.Write(opad[:])
	outer.Write(innerHash)
	return outer.Sum(nil)
}

func decodeDigestNonce(value string) []byte {
	value = strings.TrimSpace(value)
	if len(value)%2 == 0 && isHexString(value) {
		b, err := hex.DecodeString(value)
		if err == nil {
			return b
		}
	}
	b, err := base64.StdEncoding.DecodeString(value)
	if err == nil && len(b) > 0 {
		return b
	}
	b, err = base64.RawStdEncoding.DecodeString(value)
	if err == nil && len(b) > 0 {
		return b
	}
	if padded := padBase64(value); padded != value {
		b, err = base64.StdEncoding.DecodeString(padded)
		if err == nil && len(b) > 0 {
			return b
		}
	}
	return nil
}

func padBase64(s string) string {
	for len(s)%4 != 0 {
		s += "="
	}
	return s
}

func isHexString(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func hexToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "deadbeef"[:min(n*2, 8)]
	}
	return hex.EncodeToString(b)
}

func randomUint32NonZero() (uint32, error) {
	for i := 0; i < 10; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(1<<32-1))
		if err != nil {
			return 0, err
		}
		v := uint32(n.Uint64())
		if v != 0 {
			return v, nil
		}
	}
	return 1, nil
}

func quoteSipParam(value string) string {
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	return strings.ReplaceAll(escaped, "\"", "\\\"")
}

func extractIPFromAddr(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "" {
		if ip := net.ParseIP(host); ip != nil {
			if ip4 := ip.To4(); ip4 != nil {
				return ip4.String()
			}
			return "[" + ip.String() + "]"
		}
		return host
	}
	if ip := net.ParseIP(addr); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return ip4.String()
		}
		return "[" + ip.String() + "]"
	}
	return addr
}

func extractUser(impi string) string {
	if idx := strings.Index(impi, "@"); idx > 0 {
		return impi[:idx]
	}
	return impi
}

func extractHost(addr string) string {
	if idx := strings.Index(addr, "@"); idx > 0 {
		return addr[idx+1:]
	}
	return addr
}
