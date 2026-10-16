package node

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/common/format"
	"github.com/tavut846/FNode/conf"
	vCore "github.com/tavut846/FNode/core"
	"github.com/tavut846/FNode/limiter"
)

func TestCompareUserList(t *testing.T) {
	oldUsers := []panel.UserInfo{
		{Id: 1, Uuid: "uuid-1", SpeedLimit: 0},
		{Id: 2, Uuid: "uuid-2", SpeedLimit: 100},
	}
	newUsers := []panel.UserInfo{
		{Id: 2, Uuid: "uuid-2", SpeedLimit: 100}, // unchanged
		{Id: 3, Uuid: "uuid-3", SpeedLimit: 50},  // added
	}

	deleted, added := compareUserList(oldUsers, newUsers)

	if len(deleted) != 1 || deleted[0].Id != 1 {
		t.Fatalf("expected deleted user 1, got %+v", deleted)
	}
	if len(added) != 1 || added[0].Id != 3 {
		t.Fatalf("expected added user 3, got %+v", added)
	}
}

func TestReportUserTrafficTask_ClearsOfflineUsers(t *testing.T) {
	var reportedAlivePayload map[int][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/server/UniProxy/alive":
			_ = json.NewDecoder(r.Body).Decode(&reportedAlivePayload)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":true}`))
		case "/api/v1/server/UniProxy/push":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":true}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	apiClient, err := panel.New(&conf.ApiConfig{
		APIHost:  server.URL,
		NodeID:   1,
		NodeType: "vless",
		Key:      "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create api client: %v", err)
	}

	tag := "test-offline-clear"
	uuid := "user-offline-1"
	taguuid := format.UserTag(tag, uuid)
	users := []panel.UserInfo{
		{Id: 10, Uuid: uuid, DeviceLimit: 1},
	}

	lim := limiter.AddLimiter(tag, &conf.LimitConfig{}, users, make(map[int]int))
	defer limiter.DeleteLimiter(tag)

	// Simulate user was previously reported online
	ctrl := &Controller{
		tag:              tag,
		apiClient:        apiClient,
		limiter:          lim,
		lastReportedUIDs: map[int]struct{}{10: {}}, // UID 10 was reported online previously
		Options: &conf.Options{
			DeviceOnlineMinTraffic: 0,
		},
	}

	// Currently no active devices for user 10 (user went offline)
	err = ctrl.reportUserTrafficTask()
	if err != nil {
		t.Fatalf("reportUserTrafficTask failed: %v", err)
	}

	// Verify that reportedAlivePayload contains {10: []} so Xboard clears the device immediately
	if ips, ok := reportedAlivePayload[10]; !ok {
		t.Fatalf("expected UID 10 to be included in payload to clear offline device, got %+v", reportedAlivePayload)
	} else if len(ips) != 0 {
		t.Fatalf("expected empty IP slice for UID 10, got %+v", ips)
	}

	// Verify lastReportedUIDs was cleared
	if _, ok := ctrl.lastReportedUIDs[10]; ok {
		t.Fatalf("expected UID 10 to be removed from lastReportedUIDs")
	}

	// Now simulate user connects from a new IP
	_, _ = lim.CheckLimit(taguuid, "10.0.0.1", true, true)
	err = ctrl.reportUserTrafficTask()
	if err != nil {
		t.Fatalf("reportUserTrafficTask failed: %v", err)
	}

	if ips, ok := reportedAlivePayload[10]; !ok || len(ips) != 1 || ips[0] != "10.0.0.1" {
		t.Fatalf("expected UID 10 to report [10.0.0.1], got %+v", reportedAlivePayload)
	}
	if _, ok := ctrl.lastReportedUIDs[10]; !ok {
		t.Fatalf("expected UID 10 to be stored in lastReportedUIDs")
	}
}

type mockTrafficCore struct {
	trafficSlice []panel.UserTraffic
}

func (m *mockTrafficCore) Start() error                                                   { return nil }
func (m *mockTrafficCore) Close() error                                                   { return nil }
func (m *mockTrafficCore) AddNode(tag string, info *panel.NodeInfo, c *conf.Options) error { return nil }
func (m *mockTrafficCore) DelNode(tag string) error                                       { return nil }
func (m *mockTrafficCore) AddUsers(p *vCore.AddUsersParams) (int, error)                   { return len(p.Users), nil }
func (m *mockTrafficCore) DelUsers(u []panel.UserInfo, t string, i *panel.NodeInfo) error { return nil }
func (m *mockTrafficCore) Protocols() []string                                            { return []string{"vless"} }
func (m *mockTrafficCore) Type() string                                                   { return "mock" }
func (m *mockTrafficCore) GetUserTrafficSlice(tag string, reset bool) ([]panel.UserTraffic, error) {
	return m.trafficSlice, nil
}

func TestReportUserTrafficTask_PushTrafficToManager(t *testing.T) {
	var receivedPushPayload map[string][]int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/server/UniProxy/push":
			_ = json.NewDecoder(r.Body).Decode(&receivedPushPayload)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":true}`))
		case "/api/v1/server/UniProxy/alive":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":true}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	apiClient, err := panel.New(&conf.ApiConfig{
		APIHost:  server.URL,
		NodeID:   1,
		NodeType: "vless",
		Key:      "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create api client: %v", err)
	}

	tag := "test-traffic-push"
	users := []panel.UserInfo{{Id: 99, Uuid: "uuid-99"}}
	lim := limiter.AddLimiter(tag, &conf.LimitConfig{}, users, make(map[int]int))
	defer limiter.DeleteLimiter(tag)

	mockServer := &mockTrafficCore{
		trafficSlice: []panel.UserTraffic{
			{UID: 99, Upload: 5242880, Download: 10485760}, // 5MB up, 10MB down
		},
	}

	ctrl := &Controller{
		tag:       tag,
		apiClient: apiClient,
		server:    mockServer,
		limiter:   lim,
		Options: &conf.Options{
			DeviceOnlineMinTraffic: 0,
		},
	}

	err = ctrl.reportUserTrafficTask()
	if err != nil {
		t.Fatalf("reportUserTrafficTask failed: %v", err)
	}

	// Verify push payload received by panel
	if payload, ok := receivedPushPayload["99"]; !ok {
		t.Fatalf("expected UID 99 in push payload, got %+v", receivedPushPayload)
	} else if len(payload) != 2 || payload[0] != 5242880 || payload[1] != 10485760 {
		t.Fatalf("unexpected traffic values in payload for UID 99: %+v", payload)
	}
}
