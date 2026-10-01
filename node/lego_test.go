package node

import (
	"log"
	"os"
	"testing"

	"github.com/tavut846/FNode/conf"
)

var l *Lego

func getTestLego() *Lego {
	if l != nil {
		return l
	}
	token := os.Getenv("CF_DNS_API_TOKEN")
	if token == "" {
		return nil
	}
	var err error
	l, err = NewLego(&conf.CertConfig{
		CertMode:   "dns",
		Email:      "test@test.com",
		CertDomain: "test.test.com",
		Provider:   "cloudflare",
		DNSEnv: map[string]string{
			"CF_DNS_API_TOKEN": token,
		},
		CertFile: "./cert/1.pem",
		KeyFile:  "./cert/1.key",
	})
	if err != nil {
		log.Println(err)
		return nil
	}
	return l
}

func TestLego_CreateCertByDns(t *testing.T) {
	legoClient := getTestLego()
	if legoClient == nil {
		t.Skip("skipping ACME DNS test: CF_DNS_API_TOKEN environment variable not set")
	}
	err := legoClient.CreateCert()
	if err != nil {
		t.Error(err)
	}
}

func TestLego_RenewCert(t *testing.T) {
	legoClient := getTestLego()
	if legoClient == nil {
		t.Skip("skipping ACME DNS renew test: CF_DNS_API_TOKEN environment variable not set")
	}
	log.Println(legoClient.RenewCert())
}
