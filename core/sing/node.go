package sing

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"encoding/json"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/common/format"
	"github.com/tavut846/FNode/conf"
	"github.com/sagernet/sing-box/option"
	logrus "github.com/sirupsen/logrus"
	F "github.com/sagernet/sing/common/format"
	"github.com/sagernet/sing/common/json/badoption"
)

type HttpNetworkConfig struct {
	Header struct {
		Type     string           `json:"type"`
		Request  *json.RawMessage `json:"request"`
		Response *json.RawMessage `json:"response"`
	} `json:"header"`
}

type HttpRequest struct {
	Version string   `json:"version"`
	Method  string   `json:"method"`
	Path    []string `json:"path"`
	Headers struct {
		Host []string `json:"Host"`
	} `json:"headers"`
}

type WsNetworkConfig struct {
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
}

type GrpcNetworkConfig struct {
	ServiceName string `json:"serviceName"`
}

type HttpupgradeNetworkConfig struct {
	Path string `json:"path"`
	Host string `json:"host"`
}

type XhttpNetworkConfig struct {
	Path  string          `json:"path"`
	Host  string          `json:"host"`
	Mode  string          `json:"mode"`
	Extra json.RawMessage `json:"extra"`
}

func (b *Sing) getInboundOptions(tag string, info *panel.NodeInfo, c *conf.Options) (option.Inbound, error) {
	return b.buildInboundOptions(tag, info, c, false)
}

// buildInboundOptions is getInboundOptions; usersLocked says the caller
// already holds b.users.mapLock (taking it again would deadlock).
func (b *Sing) buildInboundOptions(tag string, info *panel.NodeInfo, c *conf.Options, usersLocked bool) (option.Inbound, error) {
	addr, err := netip.ParseAddr(c.ListenIP)
	if err != nil {
		return option.Inbound{}, fmt.Errorf("the listen ip not vail")
	}
	listen := option.ListenOptions{
		Listen:     (*badoption.Addr)(&addr),
		ListenPort: uint16(info.Common.ServerPort),
	}
	if c.SingOptions != nil {
		listen.TCPFastOpen = c.SingOptions.TCPFastOpen
	}
	applyTCPKeepAlive(&listen, c)
	var localMultiplex *conf.MultiplexConfig
	if c.SingOptions != nil {
		localMultiplex = c.SingOptions.Multiplex
	}
	multiplex := resolveMultiplex(tag, info, localMultiplex)
	var tls option.InboundTLSOptions
	switch info.Security {
	case panel.Tls:
		if c.CertConfig == nil {
			return option.Inbound{}, fmt.Errorf("the CertConfig is not vail")
		}
		switch c.CertConfig.CertMode {
		case "none", "":
			break // disable
		default:
			tls.Enabled = true
			tls.CertificatePath = c.CertConfig.CertFile
			tls.KeyPath = c.CertConfig.KeyFile
			if info.Common != nil && info.Common.ServerName != "" {
				tls.ServerName = info.Common.ServerName
			} else if c.CertConfig.CertDomain != "" {
				tls.ServerName = c.CertConfig.CertDomain
			}
		}
	case panel.Reality:
		tlsSettings := info.GetTlsSettings()
		if tlsSettings == nil {
			return option.Inbound{}, fmt.Errorf("reality tls settings missing for tag %s", tag)
		}
		if strings.TrimSpace(tlsSettings.PrivateKey) == "" {
			return option.Inbound{}, fmt.Errorf("reality tls requires private_key for tag %s", tag)
		}
		privateKey, err := NormalizeRealityPrivateKey(tlsSettings.PrivateKey)
		if err != nil {
			return option.Inbound{}, fmt.Errorf("reality private_key for tag %s: %w", tag, err)
		}
		shortIDs, err := ValidateRealityShortIDs(tlsSettings.GetShortIds())
		if err != nil {
			return option.Inbound{}, fmt.Errorf("reality short_id for tag %s: %w", tag, err)
		}

		serverName := strings.TrimSpace(tlsSettings.ServerName)
		if serverName == "" && info.Common != nil && info.Common.ServerName != "" {
			serverName = strings.TrimSpace(info.Common.ServerName)
		}
		if serverName == "" && c.CertConfig != nil && c.CertConfig.CertDomain != "" {
			serverName = strings.TrimSpace(c.CertConfig.CertDomain)
		}

		dest := strings.TrimSpace(tlsSettings.Dest)
		if serverName == "" && dest == "" {
			return option.Inbound{}, fmt.Errorf("reality tls requires server_name or dest for tag %s", tag)
		}

		destHost := dest
		if destHost == "" {
			destHost = serverName
		}

		destPort := tlsSettings.ServerPort
		if parts := strings.SplitN(destHost, ":", 2); len(parts) == 2 {
			destHost = parts[0]
			if p, err := strconv.Atoi(parts[1]); err == nil && p > 0 {
				destPort = p
			}
		}
		if destPort <= 0 {
			destPort = 443
		}

		if serverName == "" {
			serverName = destHost
		}

		// In sing-box reality inbound, tls.ServerName MUST match the client's SNI:
		// reality_server.go: tlsConfig.ServerNames = map[string]bool{options.ServerName: true}
		tls.Enabled = true
		tls.ServerName = serverName

		var mtd time.Duration
		if info.VAllss != nil && info.VAllss.RealityConfig.MaxTimeDiff != "" {
			mtd, _ = time.ParseDuration(info.VAllss.RealityConfig.MaxTimeDiff)
		}

		tls.Reality = &option.InboundRealityOptions{
			Enabled:    true,
			ShortID:    shortIDs,
			PrivateKey: privateKey,
			Handshake: option.InboundRealityHandshakeOptions{
				ServerOptions: option.ServerOptions{
					Server:     destHost,
					ServerPort: uint16(destPort),
				},
			},
			MaxTimeDifference: badoption.Duration(mtd),
		}
	}
	in := option.Inbound{
		Tag: tag,
	}
	switch info.Type {
	case "vmess", "vless":
		n := info.VAllss
		t := option.V2RayTransportOptions{
			Type: n.Network,
		}
		switch n.Network {
		case "tcp":
			if len(n.NetworkSettings) != 0 {
				network := HttpNetworkConfig{}
				err := json.Unmarshal(n.NetworkSettings, &network)
				if err != nil {
					return option.Inbound{}, fmt.Errorf("decode NetworkSettings error: %s", err)
				}
				//Todo fix http options
				if network.Header.Type == "http" {
					t.Type = network.Header.Type
					var request HttpRequest
					if network.Header.Request != nil {
						err = json.Unmarshal(*network.Header.Request, &request)
						if err != nil {
							return option.Inbound{}, fmt.Errorf("decode HttpRequest error: %s", err)
						}
						t.HTTPOptions.Host = request.Headers.Host
						t.HTTPOptions.Path = request.Path[0]
						t.HTTPOptions.Method = request.Method
					}
				} else {
					t.Type = ""
				}
			} else {
				t.Type = ""
			}
		case "ws":
			var (
				path    string
				ed      int
				headers map[string]badoption.Listable[string]
			)
			if len(n.NetworkSettings) != 0 {
				network := WsNetworkConfig{}
				err := json.Unmarshal(n.NetworkSettings, &network)
				if err != nil {
					return option.Inbound{}, fmt.Errorf("decode NetworkSettings error: %s", err)
				}
				var u *url.URL
				u, err = url.Parse(network.Path)
				if err != nil {
					return option.Inbound{}, fmt.Errorf("parse path error: %s", err)
				}
				path = u.Path
				ed, _ = strconv.Atoi(u.Query().Get("ed"))
				headers = make(map[string]badoption.Listable[string], len(network.Headers))
				for k, v := range network.Headers {
					headers[k] = badoption.Listable[string]{
						v,
					}
				}
			}
			t.WebsocketOptions = option.V2RayWebsocketOptions{
				Path:                path,
				EarlyDataHeaderName: "Sec-WebSocket-Protocol",
				MaxEarlyData:        uint32(ed),
				Headers:             headers,
			}
		case "grpc":
			network := GrpcNetworkConfig{}
			if len(n.NetworkSettings) != 0 {
				err := json.Unmarshal(n.NetworkSettings, &network)
				if err != nil {
					return option.Inbound{}, fmt.Errorf("decode NetworkSettings error: %s", err)
				}
			}
			t.GRPCOptions = option.V2RayGRPCOptions{
				ServiceName: network.ServiceName,
			}
		case "httpupgrade":
			network := HttpupgradeNetworkConfig{}
			if len(n.NetworkSettings) != 0 {
				err := json.Unmarshal(n.NetworkSettings, &network)
				if err != nil {
					return option.Inbound{}, fmt.Errorf("decode NetworkSettings error: %s", err)
				}
			}
			t.HTTPUpgradeOptions = option.V2RayHTTPUpgradeOptions{
				Path: network.Path,
				Host: network.Host,
			}
		case "xhttp":
			network := XhttpNetworkConfig{}
			if len(n.NetworkSettings) != 0 {
				err := json.Unmarshal(n.NetworkSettings, &network)
				if err != nil {
					return option.Inbound{}, fmt.Errorf("decode NetworkSettings error: %s", err)
				}
			}
			t.Type = "httpupgrade"
			t.HTTPUpgradeOptions = option.V2RayHTTPUpgradeOptions{
				Path: network.Path,
				Host: network.Host,
			}
		}
		if info.Type == "vless" {
			in.Type = "vless"
			in.Options = &option.VLESSInboundOptions{
				ListenOptions: listen,
				InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
					TLS: &tls,
				},
				Transport: &t,
				Multiplex: multiplex,
			}
		} else {
			in.Type = "vmess"
			in.Options = &option.VMessInboundOptions{
				ListenOptions: listen,
				InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
					TLS: &tls,
				},
				Transport: &t,
				Multiplex: multiplex,
			}
		}
	case "shadowsocks":
		in.Type = "shadowsocks"
		n := info.Shadowsocks
		var keyLength int
		switch n.Cipher {
		case "2022-blake3-aes-128-gcm":
			keyLength = 16
		case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
			keyLength = 32
		default:
			keyLength = 16
		}
		ssoption := &option.ShadowsocksInboundOptions{
			ListenOptions: listen,
			Method:        n.Cipher,
			Multiplex:     multiplex,
		}
		p := make([]byte, keyLength)
		_, _ = rand.Read(p)
		randomPasswd := string(p)
		if strings.Contains(n.Cipher, "2022") {
			ssoption.Password = n.ServerKey
			randomPasswd = base64.StdEncoding.EncodeToString([]byte(randomPasswd))
		}
		ssoption.Users = []option.ShadowsocksUser{{
			Password: randomPasswd,
		}}
		in.Options = ssoption
	case "trojan":
		n := info.Trojan
		t := option.V2RayTransportOptions{
			Type: n.Network,
		}
		switch n.Network {
		case "tcp":
			t.Type = ""
		case "ws":
			var (
				path    string
				ed      int
				headers map[string]badoption.Listable[string]
			)
			if len(n.NetworkSettings) != 0 {
				network := WsNetworkConfig{}
				err := json.Unmarshal(n.NetworkSettings, &network)
				if err != nil {
					return option.Inbound{}, fmt.Errorf("decode NetworkSettings error: %s", err)
				}
				var u *url.URL
				u, err = url.Parse(network.Path)
				if err != nil {
					return option.Inbound{}, fmt.Errorf("parse path error: %s", err)
				}
				path = u.Path
				ed, _ = strconv.Atoi(u.Query().Get("ed"))
				headers = make(map[string]badoption.Listable[string], len(network.Headers))
				for k, v := range network.Headers {
					headers[k] = badoption.Listable[string]{
						v,
					}
				}
			}
			t.WebsocketOptions = option.V2RayWebsocketOptions{
				Path:                path,
				EarlyDataHeaderName: "Sec-WebSocket-Protocol",
				MaxEarlyData:        uint32(ed),
				Headers:             headers,
			}
		case "grpc":
			network := GrpcNetworkConfig{}
			if len(n.NetworkSettings) != 0 {
				err := json.Unmarshal(n.NetworkSettings, &network)
				if err != nil {
					return option.Inbound{}, fmt.Errorf("decode NetworkSettings error: %s", err)
				}
			}
			t.GRPCOptions = option.V2RayGRPCOptions{
				ServiceName: network.ServiceName,
			}
		case "httpupgrade":
			network := HttpupgradeNetworkConfig{}
			if len(n.NetworkSettings) != 0 {
				err := json.Unmarshal(n.NetworkSettings, &network)
				if err != nil {
					return option.Inbound{}, fmt.Errorf("decode NetworkSettings error: %s", err)
				}
			}
			t.HTTPUpgradeOptions = option.V2RayHTTPUpgradeOptions{
				Path: network.Path,
				Host: network.Host,
			}
		case "xhttp":
			network := XhttpNetworkConfig{}
			if len(n.NetworkSettings) != 0 {
				err := json.Unmarshal(n.NetworkSettings, &network)
				if err != nil {
					return option.Inbound{}, fmt.Errorf("decode NetworkSettings error: %s", err)
				}
			}
			t.Type = "httpupgrade"
			t.HTTPUpgradeOptions = option.V2RayHTTPUpgradeOptions{
				Path: network.Path,
				Host: network.Host,
			}
		default:
			t.Type = ""
		}
		in.Type = "trojan"
		trojanoption := &option.TrojanInboundOptions{
			ListenOptions: listen,
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
				TLS: &tls,
			},
			Transport: &t,
			Multiplex: multiplex,
		}
		if c.SingOptions != nil && c.SingOptions.FallBackConfigs != nil {
			// fallback handling
			fallback := c.SingOptions.FallBackConfigs.FallBack
			fallbackPort, err := strconv.Atoi(fallback.ServerPort)
			if err == nil {
				trojanoption.Fallback = &option.ServerOptions{
					Server:     fallback.Server,
					ServerPort: uint16(fallbackPort),
				}
			}
			fallbackForALPNMap := c.SingOptions.FallBackConfigs.FallBackForALPN
			fallbackForALPN := make(map[string]*option.ServerOptions, len(fallbackForALPNMap))
			if err := processFallback(c, fallbackForALPN); err == nil {
				trojanoption.FallbackForALPN = fallbackForALPN
			}
		}
		in.Options = trojanoption
	case "tuic":
		in.Type = "tuic"
		tls.ALPN = append(tls.ALPN, "h3")
		in.Options = &option.TUICInboundOptions{
			ListenOptions:     listen,
			CongestionControl: info.Tuic.CongestionControl,
			ZeroRTTHandshake:  info.Tuic.ZeroRTTHandshake,
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
				TLS: &tls,
			},
		}
	case "anytls":
		in.Type = "anytls"
		anytlsOpt := &option.AnyTLSInboundOptions{
			ListenOptions: listen,
			PaddingScheme: info.AnyTls.PaddingScheme,
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
				TLS: &tls,
			},
		}
		if b != nil {
			if !usersLocked {
				b.users.mapLock.RLock()
			}
			if uList, ok := b.inboundUsers[tag]; ok && len(uList) > 0 {
				anytlsOpt.Users = make([]option.AnyTLSUser, len(uList))
				for i, u := range uList {
					anytlsOpt.Users[i] = option.AnyTLSUser{
						Name:     format.UserEmail(tag, u.Uuid),
						Password: u.Uuid,
					}
				}
			}
			if !usersLocked {
				b.users.mapLock.RUnlock()
			}
		}
		in.Options = anytlsOpt
	case "hysteria":
		in.Type = "hysteria"
		tls.ALPN = append(tls.ALPN, "h3")
		in.Options = &option.HysteriaInboundOptions{
			ListenOptions: listen,
			UpMbps:        info.Hysteria.UpMbps,
			DownMbps:      info.Hysteria.DownMbps,
			Obfs:          info.Hysteria.Obfs,
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
				TLS: &tls,
			},
		}
	case "hysteria2":
		in.Type = "hysteria2"
		tls.ALPN = append(tls.ALPN, "h3")
		var obfs *option.Hysteria2Obfs
		obfsType := strings.ToLower(info.Hysteria2.ObfsType)
		if obfsType != "" && obfsType != "plain" {
			if info.Hysteria2.ObfsPassword != "" {
				obfs = &option.Hysteria2Obfs{
					Type:     obfsType,
					Password: info.Hysteria2.ObfsPassword,
				}
			} else {
				obfs = &option.Hysteria2Obfs{
					Type:     "salamander",
					Password: info.Hysteria2.ObfsType,
				}
			}
		}
		var masquerade *option.Hysteria2Masquerade
		target := info.Hysteria2.Masquerade
		if target == "" && c.Masquerade != "" {
			target = c.Masquerade
		}
		if target != "" {
			if strings.HasPrefix(target, "file://") {
				masquerade = &option.Hysteria2Masquerade{
					Type: "file",
					FileOptions: option.Hysteria2MasqueradeFile{
						Directory: strings.TrimPrefix(target, "file://"),
					},
				}
			} else if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
				masquerade = &option.Hysteria2Masquerade{
					Type: "proxy",
					ProxyOptions: option.Hysteria2MasqueradeProxy{
						URL:         target,
						RewriteHost: true,
					},
				}
			} else {
				masquerade = &option.Hysteria2Masquerade{
					Type: "string",
					StringOptions: option.Hysteria2MasqueradeString{
						StatusCode: 200,
						Content:    target,
					},
				}
			}
		}

		// Transport & Performance enhancements (Option A):
		// 1. Configure UDP packet fragmentation based on local config with panel override
		var udpFragment *bool
		var hy2Cfg *conf.Hysteria2Config
		if c.Hysteria2Config != nil {
			hy2Cfg = c.Hysteria2Config
		} else if c.SingOptions != nil && c.SingOptions.Hysteria2Config != nil {
			hy2Cfg = c.SingOptions.Hysteria2Config
		}
		// 2. UDP socket reuse is opt-in (Hysteria2Config.ReuseAddr)
		listen.ReuseAddr = hy2Cfg != nil && hy2Cfg.ReuseAddr

		if hy2Cfg != nil && hy2Cfg.UDPFragment != nil {
			udpFragment = hy2Cfg.UDPFragment
		}
		if info.Hysteria2.UDPFragment {
			frag := true
			udpFragment = &frag
		}

		brutalDebug := info.Hysteria2.BrutalDebug
		if hy2Cfg != nil {
			if !brutalDebug {
				brutalDebug = hy2Cfg.BrutalDebug
			}
			if hy2Cfg.UDPTimeout != "" {
				if d, err := time.ParseDuration(hy2Cfg.UDPTimeout); err == nil {
					listen.UDPTimeout = option.UDPTimeoutCompat(badoption.Duration(d))
				}
			}
		}
		if udpFragment != nil {
			listen.UDPFragment = udpFragment
		}

		bw := resolveHysteria2Bandwidth(info.Hysteria2, hy2Cfg)
		if bw.Reason != "" {
			logrus.WithFields(logrus.Fields{"tag": tag, "up_mbps": bw.UpMbps, "down_mbps": bw.DownMbps, "ignore_client_bandwidth": bw.Ignore}).
				Info("Hysteria2 Brutal rate limited: " + bw.Reason)
		}
		in.Options = &option.Hysteria2InboundOptions{
			ListenOptions:         listen,
			UpMbps:                bw.UpMbps,
			DownMbps:              bw.DownMbps,
			IgnoreClientBandwidth: bw.Ignore,
			Obfs:                  obfs,
			Masquerade:            masquerade,
			BrutalDebug:           brutalDebug,
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
				TLS: &tls,
			},
		}
	}
	return in, nil
}

func (b *Sing) AddNode(tag string, info *panel.NodeInfo, config *conf.Options) error {
	b.nodeReportMinTrafficBytes[tag] = config.ReportMinTraffic * 1024
	b.users.mapLock.Lock()
	b.inboundInfo[tag] = info
	b.inboundConfig[tag] = config
	if _, ok := b.inboundUsers[tag]; !ok {
		b.inboundUsers[tag] = make([]panel.UserInfo, 0)
	}
	b.users.mapLock.Unlock()
	c, err := b.getInboundOptions(tag, info, config)
	if err != nil {
		return err
	}
	in := b.box.Inbound()
	err = in.Create(
		b.ctx,
		b.box.Router(),
		b.logFactory.NewLogger(F.ToString("inbound/", c.Type, "[", tag, "]")),
		tag,
		c.Type,
		c.Options,
	)

	if err != nil {
		return fmt.Errorf("add inbound error: %s", err)
	}
	logrus.WithFields(logrus.Fields{"tag": tag, "type": c.Type, "port": info.Common.ServerPort}).Info("Inbound created")
	b.users.mapLock.Lock()
	b.inboundOptions[tag] = c
	b.users.mapLock.Unlock()
	if err := b.applyPanelState(true); err != nil {
		logrus.WithFields(logrus.Fields{"tag": tag, "err": err}).Error("Apply panel outbounds and routes failed")
	}
	return nil
}

func (b *Sing) DelNode(tag string) error {
	b.users.mapLock.Lock()
	delete(b.inboundInfo, tag)
	delete(b.inboundUsers, tag)
	delete(b.inboundConfig, tag)
	delete(b.inboundOptions, tag)
	b.users.mapLock.Unlock()
	in := b.box.Inbound()
	err := in.Remove(tag)
	if err != nil {
		return fmt.Errorf("delete inbound error: %s", err)
	}
	// Keep this node's panel outbounds until the next AddNode: a config
	// reload removes and re-adds the node, and the outbounds usually survive.
	if err := b.applyPanelState(false); err != nil {
		logrus.WithFields(logrus.Fields{"tag": tag, "err": err}).Error("Apply panel outbounds and routes failed")
	}
	return nil
}

// UpdateNodeRouting applies a config whose only changes are routing fields
// (routes, custom_outbounds, custom_routes, custom_route_rules). The inbound
// and its users keep running; outbounds and rules are reconciled in place.
func (b *Sing) UpdateNodeRouting(tag string, info *panel.NodeInfo) error {
	b.users.mapLock.Lock()
	if _, ok := b.inboundInfo[tag]; !ok {
		b.users.mapLock.Unlock()
		return fmt.Errorf("node %s not found", tag)
	}
	b.inboundInfo[tag] = info
	b.users.mapLock.Unlock()
	return b.applyPanelState(true)
}
