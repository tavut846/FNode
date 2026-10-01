package conf

import (
	"os"
	"testing"
)

func TestConf_LoadFromPath(t *testing.T) {
	c := New()
	t.Log(c.LoadFromPath("../example/config.json"), c.NodeConfig)
}

func TestConf_Watch(t *testing.T) {
	c := New()
	tmpFile, err := os.CreateTemp("", "conf_watch_*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	_, _ = tmpFile.WriteString("{}")
	_ = tmpFile.Close()

	err = c.Watch(tmpFile.Name(), "", "", func() {})
	if err != nil {
		t.Fatal(err)
	}
}
