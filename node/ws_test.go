package node

import (
	"strings"
	"testing"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
	vCore "github.com/tavut846/FNode/core"
	"github.com/tavut846/FNode/limiter"
)

func TestApplyUserDelta(t *testing.T) {
	current := []panel.UserInfo{{Id: 1, Uuid: "u1"}, {Id: 2, Uuid: "u2", SpeedLimit: 5}}

	added := applyUserDelta(current, "add", []panel.UserInfo{{Id: 2, Uuid: "u2", SpeedLimit: 50}, {Id: 3, Uuid: "u3"}})
	if len(added) != 3 {
		t.Fatalf("expected 3 users, got %+v", added)
	}
	for _, u := range added {
		if u.Id == 2 && u.SpeedLimit != 50 {
			t.Errorf("add must update an existing user: %+v", u)
		}
	}
	deleted, newUsers := compareUserList(current, added)
	if len(newUsers) != 2 || len(deleted) != 1 {
		t.Errorf("diff after add: deleted %v added %v", deleted, newUsers)
	}

	removed := applyUserDelta(added, "remove", []panel.UserInfo{{Id: 1}, {Id: 99}})
	if len(removed) != 2 {
		t.Fatalf("expected 2 users after remove, got %+v", removed)
	}
	for _, u := range removed {
		if u.Id == 1 {
			t.Error("user 1 must be removed")
		}
	}
	if got := applyUserDelta(current, "remove", []panel.UserInfo{{Id: 1}, {Id: 2}}); len(got) != 0 {
		t.Errorf("removing everyone must leave an empty list, got %+v", got)
	}
}

func TestDevicesToAlive(t *testing.T) {
	alive := devicesToAlive(map[int][]string{5: {"1.1.1.1", "2.2.2.2"}, 6: {"3.3.3.3"}})
	if alive[5] != 2 || alive[6] != 1 || len(alive) != 2 {
		t.Errorf("unexpected alive map %v", alive)
	}
}

type fakeCore struct {
	added, deleted []panel.UserInfo
	routing        int
	delNode        int
	addNode        int
}

func (f *fakeCore) Start() error { return nil }
func (f *fakeCore) Close() error { return nil }
func (f *fakeCore) AddNode(string, *panel.NodeInfo, *conf.Options) error {
	f.addNode++
	return nil
}
func (f *fakeCore) DelNode(string) error {
	f.delNode++
	return nil
}
func (f *fakeCore) AddUsers(p *vCore.AddUsersParams) (int, error) {
	f.added = append(f.added, p.Users...)
	return len(p.Users), nil
}
func (f *fakeCore) GetUserTrafficSlice(string, bool) ([]panel.UserTraffic, error) { return nil, nil }
func (f *fakeCore) DelUsers(users []panel.UserInfo, _ string, _ *panel.NodeInfo) error {
	f.deleted = append(f.deleted, users...)
	return nil
}
func (f *fakeCore) Protocols() []string { return nil }
func (f *fakeCore) Type() string        { return "fake" }
func (f *fakeCore) UpdateNodeRouting(string, *panel.NodeInfo) error {
	f.routing++
	return nil
}

const wsTestConfig = `{"server_port":443,"network":"tcp","tls":0,"base_config":{"push_interval":60,"pull_interval":60},
"routes":[{"id":1,"match":["netflix.com"],"action":"proxy","action_value":"us"}],
"custom_outbounds":[{"tag":"us","protocol":"socks","settings":{"server":"1.2.3.4","server_port":1080}}]}`

func TestController_HandleWSEvents(t *testing.T) {
	apiClient, err := panel.New(&conf.ApiConfig{APIHost: "http://127.0.0.1:1", NodeID: 7, NodeType: "vless", Key: "k"})
	if err != nil {
		t.Fatal(err)
	}
	tag := "ws-event-test"
	users := []panel.UserInfo{{Id: 1, Uuid: "u1"}}
	lim := limiter.AddLimiter(tag, &conf.LimitConfig{}, users, map[int]int{})
	defer limiter.DeleteLimiter(tag)
	info, err := apiClient.ParseNodeConfig([]byte(wsTestConfig))
	if err != nil || info == nil {
		t.Fatalf("parse: %v", err)
	}
	core := &fakeCore{}
	c := &Controller{server: core, apiClient: apiClient, tag: tag, limiter: lim, userList: users, info: info,
		Options: &conf.Options{}}

	event := func(raw string) *panel.WSEvent {
		t.Helper()
		_, ev, err := panel.ParseWSMessage([]byte(raw))
		if err != nil || ev == nil {
			t.Fatalf("parse %s: %v", raw, err)
		}
		return ev
	}

	c.handleWSEvent(event(`{"event":"sync.user.delta","data":{"action":"add","users":[{"id":2,"uuid":"u2"}]}}`))
	if len(core.added) != 1 || core.added[0].Uuid != "u2" || len(c.userList) != 2 {
		t.Fatalf("delta add: core %+v list %+v", core.added, c.userList)
	}
	c.handleWSEvent(event(`{"event":"sync.user.delta","data":{"action":"remove","users":[{"id":1}]}}`))
	if len(core.deleted) != 1 || core.deleted[0].Uuid != "u1" || len(c.userList) != 1 {
		t.Fatalf("delta remove: core %+v list %+v", core.deleted, c.userList)
	}

	c.handleWSEvent(event(`{"event":"sync.devices","data":{"users":{"2":["9.9.9.9"]}}}`))
	if c.limiter.AliveList[2] != 1 {
		t.Errorf("devices: %v", c.limiter.AliveList)
	}

	// An outbound-only change is applied in place, without rebuilding the node
	changed := strings.Replace(wsTestConfig, `"server_port":1080`, `"server_port":2080`, 1)
	c.handleWSEvent(event(`{"event":"sync.config","data":{"config":` + changed + `}}`))
	if core.routing != 1 || core.delNode != 0 {
		t.Errorf("routing-only change: routing=%d delNode=%d", core.routing, core.delNode)
	}
	// The same config pushed again is a no-op
	c.handleWSEvent(event(`{"event":"sync.config","data":{"config":` + changed + `}}`))
	if core.routing != 1 {
		t.Errorf("unchanged config must not re-apply, routing=%d", core.routing)
	}

	// Events for another node are ignored
	c.handleWSEvent(event(`{"event":"sync.user.delta","data":{"node_id":8,"action":"remove","users":[{"id":2}]}}`))
	if len(c.userList) != 1 {
		t.Errorf("event for another node must be ignored")
	}
}
