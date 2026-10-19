package sing

import (
	"testing"
	"time"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
	"github.com/tavut846/FNode/core"
)

func anytlsNode(port int) *panel.NodeInfo {
	common := &panel.CommonNode{ServerPort: port}
	return &panel.NodeInfo{
		Id:       468,
		Type:     "anytls",
		Security: panel.Tls,
		Common:   common,
		AnyTls:   &panel.AnyTlsNode{CommonNode: *common},
	}
}

// Adding or removing anytls users updates the inbound in place: it must not
// recreate it (which drops every connection and logs a second "started").
func TestSing_AnyTLSUsersUpdateInPlace(t *testing.T) {
	b := newTestSing(t, nil)
	tag := "[https://panel.example]-anytls:468"
	info := anytlsNode(freePort(t))
	opts := &conf.Options{ListenIP: "127.0.0.1", CertConfig: &conf.CertConfig{CertMode: "none"}}

	if err := b.AddNode(tag, info, opts); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	first, found := b.box.Inbound().Get(tag)
	if !found {
		t.Fatal("AddNode did not create the anytls inbound")
	}

	run := func(name string, f func() error) {
		t.Helper()
		done := make(chan error, 1)
		go func() { done <- f() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s deadlocked", name)
		}
		if in, ok := b.box.Inbound().Get(tag); !ok || in != first {
			t.Fatalf("%s recreated the anytls inbound", name)
		}
	}
	u1 := panel.UserInfo{Id: 1, Uuid: "11111111-1111-1111-1111-111111111111"}
	u2 := panel.UserInfo{Id: 2, Uuid: "22222222-2222-2222-2222-222222222222"}
	run("AddUsers", func() error {
		_, err := b.AddUsers(&core.AddUsersParams{Tag: tag, Users: []panel.UserInfo{u1, u2}, NodeInfo: info})
		return err
	})
	run("DelUsers", func() error { return b.DelUsers([]panel.UserInfo{u1}, tag, info) })

	if err := b.DelNode(tag); err != nil {
		t.Fatalf("DelNode: %v", err)
	}
}
