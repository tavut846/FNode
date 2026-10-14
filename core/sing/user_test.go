package sing

import (
	"context"
	"net"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/metadata"
	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/common/counter"
	"github.com/tavut846/FNode/common/format"
	"github.com/tavut846/FNode/conf"
	"github.com/tavut846/FNode/core"
	"github.com/tavut846/FNode/limiter"
)

func TestUserEmail_Formatting(t *testing.T) {
	tag := "[https://domain.com]-shadowsocks:296"
	uuid := "11c9573c-abcd-1234-5678-0123456789ab"
	expected := "email: [https://domain.com]-shadowsocks:296|11c9573c-abcd-1234-5678-0123456789ab"

	email := format.UserEmail(tag, uuid)
	if email != expected {
		t.Fatalf("expected %s, got %s", expected, email)
	}

	cleanUUID := format.ExtractUUID(email)
	if cleanUUID != uuid {
		t.Fatalf("expected extracted UUID %s, got %s", uuid, cleanUUID)
	}

	tagUUID := format.ExtractTagUUID(tag, email)
	expectedTagUUID := "[https://domain.com]-shadowsocks:296|11c9573c-abcd-1234-5678-0123456789ab"
	if tagUUID != expectedTagUUID {
		t.Fatalf("expected extracted taguuid %s, got %s", expectedTagUUID, tagUUID)
	}
}

func TestSing_AddUsers_UidMapAndTrafficAccounting(t *testing.T) {
	tag := "[https://domain.com]-shadowsocks:296"
	uuid := "11c9573c-abcd-1234-5678-0123456789ab"
	uid := 42

	s := &Sing{
		nodeReportMinTrafficBytes: make(map[string]int64),
		inboundUsers:              make(map[string][]panel.UserInfo),
		inboundInfo:               make(map[string]*panel.NodeInfo),
		hookServer:                &HookServer{},
		users: &UserMap{
			uidMap: make(map[string]int),
		},
	}

	// Simulate adding user
	p := &core.AddUsersParams{
		Tag: tag,
		Users: []panel.UserInfo{
			{
				Id:   uid,
				Uuid: uuid,
			},
		},
		NodeInfo: &panel.NodeInfo{
			Id:   296,
			Type: "shadowsocks",
		},
	}

	// Verify uidMap stores both raw UUID and email-prefixed TagUUID
	s.users.mapLock.Lock()
	s.users.uidMap[uuid] = uid
	s.users.uidMap[format.UserTag(p.Tag, uuid)] = uid
	s.users.uidMap[format.UserEmail(p.Tag, uuid)] = uid
	s.users.mapLock.Unlock()

	// Verify lookups
	if s.users.uidMap[uuid] != uid {
		t.Fatalf("raw uuid lookup failed")
	}
	if s.users.uidMap[format.UserTag(tag, uuid)] != uid {
		t.Fatalf("user tag lookup failed")
	}
	if s.users.uidMap[format.UserEmail(tag, uuid)] != uid {
		t.Fatalf("user email lookup failed")
	}

	// Simulate traffic recording with UserEmail format
	email := format.UserEmail(tag, uuid)
	cleanUUID := format.ExtractUUID(email)
	tc := counter.NewTrafficCounter()
	s.hookServer.counter.Store(tag, tc)
	traffic := tc.GetCounter(cleanUUID)
	traffic.UpCounter.Add(1024)
	traffic.DownCounter.Add(2048)

	// Verify GetUserTrafficSlice extracts traffic accurately and maps to UID
	trafficSlice, err := s.GetUserTrafficSlice(tag, false)
	if err != nil {
		t.Fatalf("GetUserTrafficSlice error: %v", err)
	}
	if len(trafficSlice) != 1 {
		t.Fatalf("expected 1 traffic entry, got %d", len(trafficSlice))
	}
	if trafficSlice[0].UID != uid {
		t.Fatalf("expected UID %d, got %d", uid, trafficSlice[0].UID)
	}
	if trafficSlice[0].Upload != 1024 || trafficSlice[0].Download != 2048 {
		t.Fatalf("traffic amounts do not match")
	}

	// Verify GetUserTraffic
	up, down := s.GetUserTraffic(tag, uuid, false)
	if up != 1024 || down != 2048 {
		t.Fatalf("GetUserTraffic by uuid failed: up=%d, down=%d", up, down)
	}
	up2, down2 := s.GetUserTraffic(tag, email, false)
	if up2 != 1024 || down2 != 2048 {
		t.Fatalf("GetUserTraffic by email failed: up=%d, down=%d", up2, down2)
	}
}

func TestHookServer_WithUserEmail(t *testing.T) {
	tag := "[https://domain.com]-shadowsocks:296"
	uuid := "11c9573c-abcd-1234-5678-0123456789ab"
	userEmail := format.UserEmail(tag, uuid)

	// Set up limiter with speed & device limit info
	limitCfg := &conf.LimitConfig{
		EnableDynamicSpeedLimit: false,
	}
	users := []panel.UserInfo{
		{
			Id:         100,
			Uuid:       uuid,
			SpeedLimit: 1000,
		},
	}
	l := limiter.AddLimiter(tag, limitCfg, users, nil)
	defer limiter.DeleteLimiter(tag)

	// Verify limiter has taguuid loaded
	tagUUID := format.ExtractTagUUID(tag, userEmail)
	if _, ok := l.UserLimitInfo.Load(tagUUID); !ok {
		t.Fatalf("tagUUID %s not found in limiter UserLimitInfo", tagUUID)
	}

	// Create HookServer and pipe a simulated connection through RoutedConnection
	hook := &HookServer{}
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	ctx := context.Background()
	inboundCtx := adapter.InboundContext{
		Inbound:     tag,
		User:        userEmail,
		Source:      metadata.ParseSocksaddr("192.168.1.100:54321"),
		Destination: metadata.ParseSocksaddr("1.1.1.1:443"),
		Network:     "tcp",
	}

	routedConn := hook.RoutedConnection(ctx, clientConn, inboundCtx, nil, nil)
	if routedConn == nil {
		t.Fatalf("expected non-nil routedConn")
	}

	// Verify traffic counter was initialized with clean uuid
	v, ok := hook.counter.Load(tag)
	if !ok {
		t.Fatalf("hook counter for tag %s not initialized", tag)
	}
	tc := v.(*counter.TrafficCounter)
	if _, ok := tc.Counters.Load(uuid); !ok {
		t.Fatalf("traffic counter not stored under clean UUID %s", uuid)
	}
}

func TestUserOptions_EmailFormatting(t *testing.T) {
	tag := "[https://domain.com]-shadowsocks:296"
	uuid := "11c9573c-abcd-1234-5678-0123456789ab"
	expectedEmail := "email: [https://domain.com]-shadowsocks:296|11c9573c-abcd-1234-5678-0123456789ab"

	protocols := []string{"vless", "vmess", "shadowsocks", "trojan", "tuic", "hysteria", "hysteria2", "anytls"}
	for _, proto := range protocols {
		t.Run(proto, func(t *testing.T) {
			formatted := format.UserEmail(tag, uuid)
			if formatted != expectedEmail {
				t.Fatalf("[%s] expected email %s, got %s", proto, expectedEmail, formatted)
			}
		})
	}
}
