package bootstrap

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestDeploymentPKIIsExplicitAndNeverOverwrites(t *testing.T){
	dir:=filepath.Join(t.TempDir(),"pki")
	if err:=RunProvision(context.Background(),[]string{"local-pki",dir},func(string)string{return ""});err!=nil{t.Fatal(err)}
	b,err:=os.ReadFile(filepath.Join(dir,"ca.crt"));if err!=nil{t.Fatal(err)};block,_:=pem.Decode(b);ca,err:=x509.ParseCertificate(block.Bytes);if err!=nil||!ca.IsCA{t.Fatal("invalid CA",err)}
	b,err=os.ReadFile(filepath.Join(dir,"database.crt"));if err!=nil{t.Fatal(err)};block,_=pem.Decode(b);cert,err:=x509.ParseCertificate(block.Bytes);if err!=nil{t.Fatal(err)}
	pool:=x509.NewCertPool();pool.AddCert(ca);if _,err=cert.Verify(x509.VerifyOptions{DNSName:"database",Roots:pool});err!=nil{t.Fatal(err)}
	if _,err=cert.Verify(x509.VerifyOptions{DNSName:"other-database",Roots:pool});err==nil{t.Fatal("wrong TLS host trusted")}
	if err=RunProvision(context.Background(),[]string{"local-pki",dir},func(string)string{return ""});err==nil{t.Fatal("existing PKI overwritten")}
	if err=RunProvision(context.Background(),[]string{"bootstrap","plan.json"},func(string)string{return ""});err==nil{t.Fatal("implicit apply accepted")}
}
