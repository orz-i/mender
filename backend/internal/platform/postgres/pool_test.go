package postgres

import (
	"strings"
	"testing"
)

func TestDatabaseTLSAndRedactedConfigErrors(t *testing.T) {
	for _, dsn := range []string{"", "invalid://secret-must-not-escape", "postgres://u:p@db.example.test/mender?sslmode=disable", "postgres://u:p@db.example.test/mender?sslmode=require", "host=db.example.test user=u password=secret-must-not-escape dbname=mender sslmode=prefer"} {
		if _, err := Config(dsn); err == nil || strings.Contains(err.Error(), "secret-must-not-escape") {
			t.Fatal("unsafe configuration accepted or leaked", err)
		}
	}
	for _, dsn := range []string{"postgres://u:p@127.0.0.1/mender?sslmode=disable", "postgres://u:p@localhost/mender?sslmode=disable", "postgres://u:p@db.example.test/mender?sslmode=verify-full"} {
		cfg, err := Config(dsn)
		if err != nil || cfg.MaxConns != 4 || cfg.ConnConfig.ConnectTimeout == 0 {
			t.Fatal(err)
		}
	}
}
