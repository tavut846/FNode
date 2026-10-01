package panel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"encoding/json"
)

// Security type
const (
	None    = 0
	Tls     = 1
	Reality = 2
)

type NodeInfo struct {
	Id           int
	Type         string
	Security     int
	PushInterval time.Duration
	PullInterval time.Duration
	RawDNS       RawDNS
	Rules        Rules

	// origin
	VAllss      *VAllssNode
	Shadowsocks *ShadowsocksNode
	Trojan      *TrojanNode
	Tuic        *TuicNode
	AnyTls      *AnyTlsNode
	Hysteria    *HysteriaNode
	Hysteria2   *Hysteria2Node
	Common      *CommonNode
}

type CommonNode struct {
	Host       string      `json:"host"`
	ServerPort int         `json:"server_port"`
	ServerName string      `json:"server_name"`
	Routes     []Route     `json:"routes"`
	BaseConfig *BaseConfig `json:"base_config"`
}

type Route struct {
	Id          int         `json:"id"`
	Match       interface{} `json:"match"`
	Action      string      `json:"action"`
	ActionValue string      `json:"action_value"`
}
type BaseConfig struct {
	PushInterval any `json:"push_interval"`
	PullInterval any `json:"pull_interval"`
}

// VAllssNode is vmess and vless node info
type VAllssNode struct {
	CommonNode
	Tls                 int             `json:"tls"`
	TlsSettings         TlsSettings     `json:"tls_settings"`
	TlsSettingsBack     *TlsSettings    `json:"tlsSettings"`
	Network             string          `json:"network"`
	NetworkSettings     json.RawMessage `json:"network_settings"`
	NetworkSettingsBack json.RawMessage `json:"networkSettings"`
	Encryption          string          `json:"encryption"`
	EncryptionSettings  EncSettings     `json:"encryption_settings"`
	ServerName          string          `json:"server_name"`

	// vless only
	Flow          string        `json:"flow"`
	RealityConfig RealityConfig `json:"-"`
}

type TlsSettings struct {
	ServerName  string   `json:"server_name"`
	Dest        string   `json:"dest"`
	ServerPort  int      `json:"-"`
	ShortId     string   `json:"-"`
	ShortIds    []string `json:"-"`
	PrivateKey  string   `json:"private_key"`
	Mldsa65Seed string   `json:"mldsa65Seed"`
	Xver        uint64   `json:"-"`
}

func (t *TlsSettings) UnmarshalJSON(data []byte) error {
	type Alias TlsSettings
	aux := struct {
		*Alias
		RawShortID    json.RawMessage `json:"short_id"`
		RawServerPort json.RawMessage `json:"server_port"`
		RawXver       json.RawMessage `json:"xver"`
	}{
		Alias: (*Alias)(t),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Flexible server_port (integer 443, string "443", or omitted)
	if len(aux.RawServerPort) > 0 {
		var portInt int
		if err := json.Unmarshal(aux.RawServerPort, &portInt); err == nil {
			t.ServerPort = portInt
		} else {
			var portStr string
			if err := json.Unmarshal(aux.RawServerPort, &portStr); err == nil {
				t.ServerPort, _ = strconv.Atoi(strings.TrimSpace(portStr))
			}
		}
	}

	// Flexible xver (integer 0, string "0", or omitted)
	if len(aux.RawXver) > 0 {
		var xverInt uint64
		if err := json.Unmarshal(aux.RawXver, &xverInt); err == nil {
			t.Xver = xverInt
		} else {
			var xverStr string
			if err := json.Unmarshal(aux.RawXver, &xverStr); err == nil {
				t.Xver, _ = strconv.ParseUint(strings.TrimSpace(xverStr), 10, 64)
			}
		}
	}

	// Flexible short_id (single string, comma-separated, or JSON array)
	if len(aux.RawShortID) > 0 {
		var single string
		if err := json.Unmarshal(aux.RawShortID, &single); err == nil {
			trimmed := strings.TrimSpace(single)
			t.ShortId = trimmed
			if trimmed != "" {
				if strings.Contains(trimmed, ",") {
					for _, s := range strings.Split(trimmed, ",") {
						s = strings.TrimSpace(s)
						if s != "" {
							t.ShortIds = append(t.ShortIds, s)
						}
					}
				} else {
					t.ShortIds = []string{trimmed}
				}
			}
		} else {
			var list []string
			if err := json.Unmarshal(aux.RawShortID, &list); err == nil {
				for _, item := range list {
					item = strings.TrimSpace(item)
					if item != "" {
						t.ShortIds = append(t.ShortIds, item)
					}
				}
				if len(t.ShortIds) > 0 {
					t.ShortId = t.ShortIds[0]
				}
			}
		}
	}
	return nil
}

func (t *TlsSettings) GetShortIds() []string {
	if len(t.ShortIds) > 0 {
		return t.ShortIds
	}
	if t.ShortId != "" {
		return []string{t.ShortId}
	}
	return nil
}

func (n *NodeInfo) GetTlsSettings() *TlsSettings {
	if n.VAllss != nil {
		return &n.VAllss.TlsSettings
	}
	if n.Trojan != nil {
		return &n.Trojan.TlsSettings
	}
	return nil
}

type EncSettings struct {
	Mode          string `json:"mode"`
	Ticket        string `json:"ticket"`
	ServerPadding string `json:"server_padding"`
	PrivateKey    string `json:"private_key"`
}

type RealityConfig struct {
	Xver         uint64 `json:"Xver"`
	MinClientVer string `json:"MinClientVer"`
	MaxClientVer string `json:"MaxClientVer"`
	MaxTimeDiff  string `json:"MaxTimeDiff"`
}

type ShadowsocksNode struct {
	CommonNode
	Cipher    string `json:"cipher"`
	ServerKey string `json:"server_key"`
}

type TrojanNode struct {
	CommonNode
	Tls                 int             `json:"tls"`
	TlsSettings         TlsSettings     `json:"tls_settings"`
	TlsSettingsBack     *TlsSettings    `json:"tlsSettings"`
	Network             string          `json:"network"`
	NetworkSettings     json.RawMessage `json:"networkSettings"`
	NetworkSettingsBack json.RawMessage `json:"network_settings"`
}

type TuicNode struct {
	CommonNode
	CongestionControl string `json:"congestion_control"`
	ZeroRTTHandshake  bool   `json:"zero_rtt_handshake"`
}

type AnyTlsNode struct {
	CommonNode
	PaddingScheme []string `json:"padding_scheme,omitempty"`
}

func (a *AnyTlsNode) UnmarshalJSON(data []byte) error {
	type Alias AnyTlsNode
	aux := struct {
		*Alias
		RawPadding json.RawMessage `json:"padding_scheme"`
	}{
		Alias: (*Alias)(a),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if len(aux.RawPadding) > 0 {
		var single string
		if err := json.Unmarshal(aux.RawPadding, &single); err == nil {
			if single != "" {
				for _, line := range strings.Split(single, "\n") {
					line = strings.TrimSpace(line)
					if line != "" {
						a.PaddingScheme = append(a.PaddingScheme, line)
					}
				}
			}
		} else {
			var list []string
			if err := json.Unmarshal(aux.RawPadding, &list); err == nil {
				a.PaddingScheme = list
			}
		}
	}
	return nil
}

type HysteriaNode struct {
	CommonNode
	UpMbps   int    `json:"up_mbps"`
	DownMbps int    `json:"down_mbps"`
	Obfs     string `json:"obfs"`
}

type Hysteria2Node struct {
	CommonNode
	Ignore_Client_Bandwidth bool   `json:"ignore_client_bandwidth"`
	UpMbps                  int    `json:"up_mbps"`
	DownMbps                int    `json:"down_mbps"`
	ObfsType                string `json:"obfs"`
	ObfsPassword            string `json:"obfs-password"`
	Masquerade              string `json:"masquerade"`
	UDPFragment             bool   `json:"udp_fragment"`
}

type RawDNS struct {
	DNSMap  map[string]map[string]interface{}
	DNSJson []byte
}

type Rules struct {
	Regexp   []string
	Protocol []string
}

func (c *Client) GetNodeInfo() (node *NodeInfo, err error) {
	const path = "/api/v1/server/UniProxy/config"
	r, err := c.client.
		R().
		SetHeader("If-None-Match", c.nodeEtag).
		ForceContentType("application/json").
		Get(path)

	if err != nil {
		return nil, c.checkResponse(r, path, err)
	}
	if r == nil {
		return nil, fmt.Errorf("received nil response")
	}
	if r.RawBody() != nil {
		defer r.RawBody().Close()
	}
	if r.StatusCode() == 304 {
		return nil, nil
	}
	if err = c.checkResponse(r, path, err); err != nil {
		return nil, err
	}
	hash := sha256.Sum256(r.Body())
	newBodyHash := hex.EncodeToString(hash[:])
	if c.responseBodyHash == newBodyHash {
		return nil, nil
	}
	c.responseBodyHash = newBodyHash
	c.nodeEtag = r.Header().Get("ETag")
	node = &NodeInfo{
		Id:   c.NodeId,
		Type: c.NodeType,
		RawDNS: RawDNS{
			DNSMap:  make(map[string]map[string]interface{}),
			DNSJson: []byte(""),
		},
	}
	// parse protocol params
	var cm *CommonNode
	switch c.NodeType {
	case "vmess", "vless":
		rsp := &VAllssNode{}
		err = json.Unmarshal(r.Body(), rsp)
		if err != nil {
			return nil, fmt.Errorf("decode v2ray params error: %s", err)
		}
		if len(rsp.NetworkSettingsBack) > 0 {
			rsp.NetworkSettings = rsp.NetworkSettingsBack
			rsp.NetworkSettingsBack = nil
		}
		if rsp.TlsSettingsBack != nil {
			rsp.TlsSettings = *rsp.TlsSettingsBack
			rsp.TlsSettingsBack = nil
		}
		cm = &rsp.CommonNode
		node.VAllss = rsp
		node.Security = node.VAllss.Tls
	case "shadowsocks":
		rsp := &ShadowsocksNode{}
		err = json.Unmarshal(r.Body(), rsp)
		if err != nil {
			return nil, fmt.Errorf("decode shadowsocks params error: %s", err)
		}
		cm = &rsp.CommonNode
		node.Shadowsocks = rsp
		node.Security = None
	case "trojan":
		rsp := &TrojanNode{}
		err = json.Unmarshal(r.Body(), rsp)
		if err != nil {
			return nil, fmt.Errorf("decode trojan params error: %s", err)
		}
		if len(rsp.NetworkSettingsBack) > 0 {
			rsp.NetworkSettings = rsp.NetworkSettingsBack
			rsp.NetworkSettingsBack = nil
		}
		if rsp.TlsSettingsBack != nil {
			rsp.TlsSettings = *rsp.TlsSettingsBack
			rsp.TlsSettingsBack = nil
		}
		cm = &rsp.CommonNode
		node.Trojan = rsp
		if rsp.Tls == Reality {
			node.Security = Reality
		} else if rsp.Tls == None {
			node.Security = None
		} else {
			node.Security = Tls
		}
	case "tuic":
		rsp := &TuicNode{}
		err = json.Unmarshal(r.Body(), rsp)
		if err != nil {
			return nil, fmt.Errorf("decode tuic params error: %s", err)
		}
		cm = &rsp.CommonNode
		node.Tuic = rsp
		node.Security = Tls
	case "anytls":
		rsp := &AnyTlsNode{}
		err = json.Unmarshal(r.Body(), rsp)
		if err != nil {
			return nil, fmt.Errorf("decode anytls params error: %s", err)
		}
		cm = &rsp.CommonNode
		node.AnyTls = rsp
		node.Security = Tls
	case "hysteria":
		rsp := &HysteriaNode{}
		err = json.Unmarshal(r.Body(), rsp)
		if err != nil {
			return nil, fmt.Errorf("decode hysteria params error: %s", err)
		}
		cm = &rsp.CommonNode
		node.Hysteria = rsp
		node.Security = Tls
	case "hysteria2":
		rsp := &Hysteria2Node{}
		err = json.Unmarshal(r.Body(), rsp)
		if err != nil {
			return nil, fmt.Errorf("decode hysteria2 params error: %s", err)
		}
		cm = &rsp.CommonNode
		node.Hysteria2 = rsp
		node.Security = Tls
	}

	// parse rules and dns
	for i := range cm.Routes {
		var matchs []string
		if _, ok := cm.Routes[i].Match.(string); ok {
			matchs = strings.Split(cm.Routes[i].Match.(string), ",")
		} else if _, ok = cm.Routes[i].Match.([]string); ok {
			matchs = cm.Routes[i].Match.([]string)
		} else {
			temp := cm.Routes[i].Match.([]interface{})
			matchs = make([]string, len(temp))
			for i := range temp {
				matchs[i] = temp[i].(string)
			}
		}
		switch cm.Routes[i].Action {
		case "block":
			for _, v := range matchs {
				if strings.HasPrefix(v, "protocol:") {
					// protocol
					node.Rules.Protocol = append(node.Rules.Protocol, strings.TrimPrefix(v, "protocol:"))
				} else {
					// domain
					node.Rules.Regexp = append(node.Rules.Regexp, strings.TrimPrefix(v, "regexp:"))
				}
			}
		case "dns":
			var domains []string
			domains = append(domains, matchs...)
			if matchs[0] != "main" {
				node.RawDNS.DNSMap[strconv.Itoa(i)] = map[string]interface{}{
					"address": cm.Routes[i].ActionValue,
					"domains": domains,
				}
			} else {
				dns := []byte(strings.Join(matchs[1:], ""))
				node.RawDNS.DNSJson = dns
			}
		}
	}

	// set interval
	node.PushInterval = intervalToTime(cm.BaseConfig.PushInterval)
	node.PullInterval = intervalToTime(cm.BaseConfig.PullInterval)

	node.Common = cm
	// clear
	cm.Routes = nil
	cm.BaseConfig = nil

	return node, nil
}

func intervalToTime(i interface{}) time.Duration {
	switch reflect.TypeOf(i).Kind() {
	case reflect.Int:
		return time.Duration(i.(int)) * time.Second
	case reflect.String:
		i, _ := strconv.Atoi(i.(string))
		return time.Duration(i) * time.Second
	case reflect.Float64:
		return time.Duration(i.(float64)) * time.Second
	default:
		return time.Duration(reflect.ValueOf(i).Int()) * time.Second
	}
}
