package sing

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
	"github.com/tavut846/FNode/core"
)

func portOpen(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// A really started box: nodes are added and removed at runtime (the sing-box
// managers carry FNode's Remove()/start-after-boot patch) and the port follows.
func TestLiveBox_AddDelReAddNodeAndUsers(t *testing.T) {
	b := newTestSing(t, nil)
	if err := b.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer b.Close()

	port := freePort(t)
	tag := "[https://panel.example]-vless:7"
	info := nodeFor("vless")
	info.Common.ServerPort = port
	info.VAllss.CommonNode.ServerPort = port
	opts := &conf.Options{ListenIP: "127.0.0.1", CertConfig: conf.NewCertConfig()}

	for round := 1; round <= 2; round++ {
		if err := b.AddNode(tag, info, opts); err != nil {
			t.Fatalf("round %d add: %v", round, err)
		}
		if !portOpen(port) {
			t.Fatalf("round %d: node port %d is not listening after AddNode on a started box", round, port)
		}
		users := []panel.UserInfo{{Id: 1, Uuid: "11c9573c-abcd-1234-5678-0123456789ab"}, {Id: 2, Uuid: "21c9573c-abcd-1234-5678-0123456789ab"}}
		if n, err := b.AddUsers(&core.AddUsersParams{Tag: tag, Users: users, NodeInfo: info}); err != nil || n != 2 {
			t.Fatalf("round %d add users: %d, %v", round, n, err)
		}
		// a delta: drop one user, the node keeps listening
		if err := b.DelUsers(users[:1], tag, info); err != nil {
			t.Fatalf("round %d del users: %v", round, err)
		}
		if !portOpen(port) {
			t.Fatalf("round %d: node stopped listening after a user delta", round)
		}
		if err := b.DelNode(tag); err != nil {
			t.Fatalf("round %d del: %v", round, err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for portOpen(port) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if portOpen(port) {
			t.Fatalf("round %d: port %d still open after DelNode", round, port)
		}
	}
}
