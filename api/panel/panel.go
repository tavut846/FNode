package panel

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/go-resty/resty/v2"
	"github.com/tavut846/FNode/conf"
)

// Panel is the interface for different panel's api.

type Client struct {
	client           *resty.Client
	APIHost          string
	APISendIP        string
	Token            string
	NodeType         string
	NodeId           int
	MachineID        int
	nodeEtag         string
	userEtag         string
	responseBodyHash string
	hashLock         sync.Mutex
	UserList         *UserListBody
	AliveMap         *AliveMap
	// v2 selects the /api/v2/server/* endpoints (machine mode, or a server
	// token without NodeType). v1 UniProxy stays the default.
	v2 bool
}

// SupportedNodeTypes are the panel node types FNode can serve.
var SupportedNodeTypes = []string{"vmess", "vless", "trojan", "shadowsocks", "hysteria", "hysteria2", "tuic", "anytls"}

// NormalizeNodeType maps panel aliases to FNode node types. It returns ""
// for an unsupported type.
func NormalizeNodeType(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	if t == "v2ray" {
		t = "vmess"
	}
	for _, s := range SupportedNodeTypes {
		if s == t {
			return t
		}
	}
	return ""
}

func New(c *conf.ApiConfig) (*Client, error) {
	var client *resty.Client
	if c.APISendIP != "" {
		client = resty.NewWithLocalAddr(&net.TCPAddr{
			IP: net.ParseIP(c.APISendIP),
		})
	} else {
		client = resty.New()
	}
	client.SetRetryCount(3)
	if c.Timeout > 0 {
		client.SetTimeout(time.Duration(c.Timeout) * time.Second)
	} else {
		client.SetTimeout(5 * time.Second)
	}
	client.OnError(func(req *resty.Request, err error) {
		var v *resty.ResponseError
		if errors.As(err, &v) {
			// v.Response contains the last response from the server
			// v.Err contains the original error
			logrus.Error(v.Err)
		}
	})
	client.SetBaseURL(c.APIHost)
	client.SetHeader("User-Agent", UserAgent())

	token := c.Token()
	params := map[string]string{"token": token}
	nodeType := ""
	v2 := false
	switch {
	case c.IsMachine():
		// Machine mode: machine_id + machine token, node type comes from the config
		v2 = true
		params["machine_id"] = strconv.Itoa(c.MachineID)
		if c.NodeID > 0 {
			params["node_id"] = strconv.Itoa(c.NodeID)
		}
	case strings.TrimSpace(c.NodeType) == "":
		// Server token + NodeID without NodeType: v2 API, type detected from the config
		v2 = true
		params["node_id"] = strconv.Itoa(c.NodeID)
	default:
		nodeType = NormalizeNodeType(c.NodeType)
		if nodeType == "" {
			return nil, fmt.Errorf("unsupported Node type: %s", c.NodeType)
		}
		c.NodeType = nodeType
		params["node_type"] = nodeType
		params["node_id"] = strconv.Itoa(c.NodeID)
	}
	client.SetQueryParams(params)
	return &Client{
		client:    client,
		Token:     token,
		APIHost:   c.APIHost,
		APISendIP: c.APISendIP,
		NodeType:  nodeType,
		NodeId:    c.NodeID,
		MachineID: c.MachineID,
		UserList:  &UserListBody{},
		AliveMap:  &AliveMap{},
		v2:        v2,
	}, nil
}

// IsV2 reports whether the client uses the /api/v2/server endpoints.
func (c *Client) IsV2() bool { return c.v2 }

// apiPath returns the endpoint path for name (config, user, push, alive,
// alivelist, status) in the client's API version.
func (c *Client) apiPath(name string) string {
	if c.v2 {
		return "/api/v2/server/" + name
	}
	return "/api/v1/server/UniProxy/" + name
}

// ResetConfigCache forgets the last config ETag and body hash so the next
// GetNodeInfo returns the full config even when the panel has not changed it
// (used to rebuild a node after a failed reload).
func (c *Client) ResetConfigCache() {
	c.hashLock.Lock()
	c.responseBodyHash = ""
	c.hashLock.Unlock()
	c.nodeEtag = ""
}

// UseAutoType switches a v1 client (server token + NodeType) to the v2 API,
// which reads the node type from the panel's config. It resolves a NodeType
// in config.json that does not match the panel node. It reports whether the
// client changed.
func (c *Client) UseAutoType() bool {
	if c.v2 || c.MachineID > 0 {
		return false
	}
	c.v2 = true
	c.NodeType = ""
	c.client.QueryParam.Del("node_type")
	c.ResetConfigCache()
	return true
}
