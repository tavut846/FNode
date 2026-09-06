package panel

import (
	"encoding/json"
	"log"
	"testing"

	"github.com/tavut846/FNode/conf"
)

var client *Client

func init() {
	c, err := New(&conf.ApiConfig{
		APIHost:  "http://127.0.0.1",
		Key:      "token",
		NodeType: "V2ray",
		NodeID:   1,
	})
	if err != nil {
		log.Panic(err)
	}
	client = c
}

func TestClient_GetNodeInfo(t *testing.T) {
	log.Println(client.GetNodeInfo())
	log.Println(client.GetNodeInfo())
}

func TestClient_ReportUserTraffic(t *testing.T) {
	log.Println(client.ReportUserTraffic([]UserTraffic{
		{
			UID:      10372,
			Upload:   1000,
			Download: 1000,
		},
	}))
}

func TestTlsSettings_ShortIdVariants(t *testing.T) {
	// Single string
	var ts1 TlsSettings
	if err := json.Unmarshal([]byte(`{"server_name":"example.com","short_id":"abcd1234"}`), &ts1); err != nil {
		t.Fatalf("unmarshal ts1 error: %s", err)
	}
	ids := ts1.GetShortIds()
	if len(ids) != 1 || ids[0] != "abcd1234" {
		t.Errorf("expected [abcd1234], got %v", ids)
	}

	// Comma separated
	var ts2 TlsSettings
	if err := json.Unmarshal([]byte(`{"server_name":"example.com","short_id":"abcd, 1234"}`), &ts2); err != nil {
		t.Fatalf("unmarshal ts2 error: %s", err)
	}
	ids2 := ts2.GetShortIds()
	if len(ids2) != 2 || ids2[0] != "abcd" || ids2[1] != "1234" {
		t.Errorf("expected [abcd, 1234], got %v", ids2)
	}

	// JSON Array
	var ts3 TlsSettings
	if err := json.Unmarshal([]byte(`{"server_name":"example.com","short_id":["id1","id2","id3"]}`), &ts3); err != nil {
		t.Fatalf("unmarshal ts3 error: %s", err)
	}
	ids3 := ts3.GetShortIds()
	if len(ids3) != 3 || ids3[0] != "id1" || ids3[2] != "id3" {
		t.Errorf("expected 3 ids, got %v", ids3)
	}
}

func TestAnyTlsNode_PaddingSchemeVariants(t *testing.T) {
	// Multi-line string
	var at1 AnyTlsNode
	if err := json.Unmarshal([]byte(`{"padding_scheme":"stop=100-300\ncontinue=50-100"}`), &at1); err != nil {
		t.Fatalf("unmarshal at1 error: %s", err)
	}
	if len(at1.PaddingScheme) != 2 || at1.PaddingScheme[0] != "stop=100-300" {
		t.Errorf("expected 2 padding lines, got %v", at1.PaddingScheme)
	}

	// JSON Array
	var at2 AnyTlsNode
	if err := json.Unmarshal([]byte(`{"padding_scheme":["stop=100-300","continue=50-100"]}`), &at2); err != nil {
		t.Fatalf("unmarshal at2 error: %s", err)
	}
	if len(at2.PaddingScheme) != 2 || at2.PaddingScheme[1] != "continue=50-100" {
		t.Errorf("expected 2 padding lines, got %v", at2.PaddingScheme)
	}
}
