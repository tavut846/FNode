package panel

import "testing"

func TestParseWSMessageUpdate(t *testing.T) {
	name, ev, err := ParseWSMessage([]byte(`{"event":"custom_node.update","data":{"version":" 0.0.30-pre "}}`))
	if err != nil || name != WSEventUpdate || ev == nil || ev.UpdateVersion != "0.0.30-pre" {
		t.Fatalf("got %q %+v %v", name, ev, err)
	}
	if _, _, err = ParseWSMessage([]byte(`{"event":"custom_node.update","data":[]}`)); err == nil {
		t.Fatal("empty payload must fail")
	}
}
