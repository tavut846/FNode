package limiter

import (
	"sync"
	"testing"
	"time"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/common/format"
	"github.com/tavut846/FNode/conf"
)

func TestCheckLimit_DynamicSpeedLimitExpiration_NotDeleted(t *testing.T) {
	tag := "node-test"
	uuid := "11111111-2222-3333-4444-555555555555"
	taguuid := format.UserTag(tag, uuid)

	users := []panel.UserInfo{
		{
			Id:          101,
			Uuid:        uuid,
			SpeedLimit:  0, // No static speed limit
			DeviceLimit: 0,
		},
	}

	l := AddLimiter(tag, &conf.LimitConfig{}, users, make(map[int]int))
	defer DeleteLimiter(tag)

	// Verify initial connection allowed
	_, reject := l.CheckLimit(taguuid, "1.2.3.4", true, true)
	if reject {
		t.Fatalf("expected initial connection to be allowed, got rejected")
	}

	// Apply dynamic speed limit that is expired (expire time in the past)
	err := l.UpdateDynamicSpeedLimit(tag, uuid, 10, time.Now().Add(-10*time.Second))
	if err != nil {
		t.Fatalf("UpdateDynamicSpeedLimit failed: %v", err)
	}

	// Next connection should clear the dynamic speed limit without deleting UserLimitInfo
	_, reject = l.CheckLimit(taguuid, "1.2.3.4", true, true)
	if reject {
		t.Fatalf("expected connection to be allowed after dynamic speed limit expired, got rejected")
	}

	// Verify UserLimitInfo still exists for user
	v, ok := l.UserLimitInfo.Load(taguuid)
	if !ok {
		t.Fatalf("UserLimitInfo was deleted after dynamic speed limit expired! User would be permanently blocked!")
	}
	u := v.(*UserLimitInfo)
	if u.DynamicSpeedLimit != 0 {
		t.Errorf("expected DynamicSpeedLimit to be 0, got %d", u.DynamicSpeedLimit)
	}
	if u.ExpireTime != 0 {
		t.Errorf("expected ExpireTime to be 0, got %d", u.ExpireTime)
	}

	// Another connection should still be allowed
	_, reject = l.CheckLimit(taguuid, "1.2.3.4", true, true)
	if reject {
		t.Fatalf("subsequent connection was rejected after dynamic limit cleared")
	}
}

func TestCheckLimit_ClientIPChange_DeviceLimitMigration(t *testing.T) {
	tag := "node-ipchange-test"
	uuid := "aaaa-bbbb-cccc-dddd"
	taguuid := format.UserTag(tag, uuid)

	users := []panel.UserInfo{
		{
			Id:          202,
			Uuid:        uuid,
			SpeedLimit:  0,
			DeviceLimit: 1, // Single device limit
		},
	}

	// Start with aliveIp = 0 (user not online yet)
	aliveMap := map[int]int{202: 0}
	l := AddLimiter(tag, &conf.LimitConfig{}, users, aliveMap)
	defer DeleteLimiter(tag)

	// Step 1: User connects on WiFi (IP_1)
	ip1 := "192.168.1.10"
	_, reject := l.CheckLimit(taguuid, ip1, true, true)
	if reject {
		t.Fatalf("initial connection from ip1 should be allowed, got rejected")
	}

	// Step 2: Xboard reports aliveIp = 1 after reporting ip1
	l.AliveList[202] = 1

	// Step 3: Client changes network to Cellular (IP_2)
	// Even though aliveMap reports 1 device, this is a local IP migration from ip1 -> ip2
	ip2 := "100.64.0.50"
	_, reject = l.CheckLimit(taguuid, ip2, true, true)
	if reject {
		t.Fatalf("ip change to ip2 was falsely rejected by device limit!")
	}

	// Verify ip2 is registered in UserOnlineIP and ip1 was replaced
	val, ok := l.UserOnlineIP.Load(taguuid)
	if !ok {
		t.Fatalf("UserOnlineIP missing for taguuid")
	}
	ipMap := val.(*sync.Map)

	if _, exists := ipMap.Load(ip2); !exists {
		t.Errorf("expected ip2 to be in active ipMap")
	}
	if _, exists := ipMap.Load(ip1); exists {
		t.Errorf("expected older ip1 to be pruned after ip2 migration")
	}
}

func TestCheckLimit_ClusterDeviceLimit_RejectsExcessFromOtherNodes(t *testing.T) {
	tag := "node-cluster-test"
	uuid := "user-cluster-1"
	taguuid := format.UserTag(tag, uuid)

	users := []panel.UserInfo{
		{
			Id:          303,
			Uuid:        uuid,
			SpeedLimit:  0,
			DeviceLimit: 1, // 1 device allowed
		},
	}

	// Panel reports aliveIp = 1 already active on another node (this node has 0 active devices)
	aliveMap := map[int]int{303: 1}
	l := AddLimiter(tag, &conf.LimitConfig{}, users, aliveMap)
	defer DeleteLimiter(tag)

	// Connecting from this node should be rejected because limit is already saturated on other nodes
	_, reject := l.CheckLimit(taguuid, "203.0.113.1", true, true)
	if !reject {
		t.Fatalf("expected connection to be rejected when device limit is already saturated on other nodes")
	}
}

func TestGetOnlineDevice_PrunesStaleIPs(t *testing.T) {
	tag := "node-prune-test"
	uuid := "user-prune-1"
	taguuid := format.UserTag(tag, uuid)

	users := []panel.UserInfo{
		{
			Id:          404,
			Uuid:        uuid,
			SpeedLimit:  0,
			DeviceLimit: 2,
		},
	}

	l := AddLimiter(tag, &conf.LimitConfig{}, users, make(map[int]int))
	defer DeleteLimiter(tag)

	// Add an active IP
	_, _ = l.CheckLimit(taguuid, "1.1.1.1", true, true)

	// Manually inject a stale IP with timestamp 100 seconds ago
	val, _ := l.UserOnlineIP.Load(taguuid)
	ipMap := val.(*sync.Map)
	ipMap.Store("2.2.2.2", &IPRecord{
		UID:      404,
		LastSeen: time.Now().Unix() - 100, // stale (>60s)
	})

	devices, err := l.GetOnlineDevice()
	if err != nil {
		t.Fatalf("GetOnlineDevice error: %v", err)
	}

	if devices == nil || len(*devices) != 1 {
		t.Fatalf("expected exactly 1 active online device, got %d", len(*devices))
	}

	if (*devices)[0].IP != "1.1.1.1" {
		t.Errorf("expected online device to be 1.1.1.1, got %s", (*devices)[0].IP)
	}

	// Verify 2.2.2.2 was pruned from map
	if _, exists := ipMap.Load("2.2.2.2"); exists {
		t.Errorf("expected stale IP 2.2.2.2 to be deleted from map")
	}
}
