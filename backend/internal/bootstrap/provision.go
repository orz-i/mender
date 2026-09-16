package bootstrap

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/orz-i/mender/backend/internal/platform/configenv"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// All names are capabilities already implemented by the existing operator.
var deploymentGrants = map[string]bool{
	"runtime": true, "browser-session": true, "admission": true, "cancellation": true,
	"catalog-manager": true, "connection-manager": true, "publisher-manager": true,
	"governance-reviewer": true, "governance-policy-manager": true, "governance-execution-confirmer": true,
	"release-manager": true, "dangerous-operation-manager": true, "support-reader": true,
	"platform-admin-manager": true, "billing-manager": true, "payment-manager": true,
	"commerce-observer": true, "worker": true, "executor": true, "reconciler": true, "settlement": true,
}

type provisionRole struct {
	Name         string `json:"name"`
	Grant        string `json:"grant"`
	PasswordFile string `json:"password_file"`
}
type provisionPlan struct {
	Version  int             `json:"version"`
	Database string          `json:"database"`
	Instance string          `json:"instance"`
	Roles    []provisionRole `json:"roles"`
}

// RunProvision creates only explicitly inventoried least-privilege roles and
// invokes existing migration/grant contracts. It never seeds business facts,
// approves releases, grants Platform Staff, or overwrites an existing password.
func RunProvision(ctx context.Context, args []string, getenv func(string) string) error {
	if len(args) == 2 && args[0] == "local-pki" {
		return writeDeploymentPKI(args[1])
	}
	if len(args) != 3 || args[0] != "bootstrap" || args[1] != "--apply" {
		return errors.New("usage: provision local-pki NEW_DIRECTORY | bootstrap --apply PLAN_FILE")
	}
	get, err := configenv.Resolve(getenv)
	if err != nil {
		return err
	}
	f, err := os.Open(args[2])
	if err != nil {
		return errors.New("provision plan unavailable")
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 32769))
	decoder.DisallowUnknownFields()
	var plan provisionPlan
	if decoder.Decode(&plan) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("invalid provision plan")
	}
	valid := regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`)
	if plan.Version != 1 || !valid.MatchString(plan.Database) || !valid.MatchString(plan.Instance) || len(plan.Roles) == 0 || len(plan.Roles) > 32 {
		return errors.New("invalid provision identity or role count")
	}
	seen := map[string]bool{}
	passwords := map[string]string{}
	for _, role := range plan.Roles {
		if !regexp.MustCompile(`^[a-z][a-z0-9_]{1,62}$`).MatchString(role.Name) || !deploymentGrants[role.Grant] || seen[role.Name] || seen[role.Grant] {
			return errors.New("invalid or duplicate provision capability")
		}
		seen[role.Name] = true
		seen[role.Grant] = true
		info, e := os.Lstat(role.PasswordFile)
		if e != nil || !info.Mode().IsRegular() || info.Size() > 256 {
			return errors.New("role password file unavailable")
		}
		b, e := os.ReadFile(role.PasswordFile)
		if e != nil || !regexp.MustCompile(`^[a-f0-9]{64}$`).Match(b) {
			return errors.New("role password must be 32 random bytes encoded as lowercase hex")
		}
		passwords[role.Name] = string(b)
	}
	pool, err := database.Open(ctx, get("MENDER_ADMIN_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	var db string
	if err = pool.QueryRow(ctx, "SELECT current_database()").Scan(&db); err != nil || db != plan.Database {
		return errors.New("provision database identity mismatch")
	}
	if err = RunOperator(ctx, []string{"migrate"}, get, io.Discard, io.Discard); err != nil {
		return errors.New("provision migration failed; inspect operator migration separately")
	}
	for _, role := range plan.Roles {
		marker := "mender:deployment:v1:" + plan.Database + ":" + plan.Instance + ":" + role.Grant
		var exists bool
		if err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)", role.Name).Scan(&exists); err != nil {
			return errors.New("role lookup failed")
		}
		if exists {
			var safe bool
			err = pool.QueryRow(ctx, `SELECT rolcanlogin AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolbypassrls AND shobj_description(oid,'pg_authid')=$2 FROM pg_roles WHERE rolname=$1`, role.Name, marker).Scan(&safe)
			if err != nil || !safe {
				return errors.New("existing role is unmanaged or elevated; refusing to modify")
			}
			config, e := database.Config(get("MENDER_ADMIN_DATABASE_URL"))
			if e != nil {
				return e
			}
			config.ConnConfig.User = role.Name
			config.ConnConfig.Password = passwords[role.Name]
			conn, e := pgx.ConnectConfig(ctx, config.ConnConfig)
			if e != nil {
				return errors.New("existing role credential mismatch; no password was reset")
			}
			_ = conn.Close(ctx)
		} else {
			tx, e := pool.Begin(ctx)
			if e != nil {
				return errors.New("role transaction unavailable")
			}
			_, e = tx.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role.Name}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD '"+passwords[role.Name]+"'")
			if e == nil {
				_, e = tx.Exec(ctx, "COMMENT ON ROLE "+pgx.Identifier{role.Name}.Sanitize()+" IS '"+marker+"'")
			}
			if e != nil {
				_ = tx.Rollback(ctx)
				return errors.New("role creation failed (details redacted)")
			}
			if tx.Commit(ctx) != nil {
				return errors.New("role creation commit failed")
			}
		}
		if err = RunOperator(ctx, []string{"grant-" + role.Grant, "--role", role.Name}, get, io.Discard, io.Discard); err != nil {
			return fmt.Errorf("grant %s failed; no application startup authorized", role.Grant)
		}
	}
	fmt.Println("Provisioned explicit roles and current migrations; no users, business seeds, approvals or payments were created.")
	return nil
}

// Local PKI is for a private deployment database and smoke-test TLS endpoints.
// It never adds a CA to OS/browser trust or overwrites existing certificate files.
func writeDeploymentPKI(dir string) error {
	if err := os.Mkdir(dir, 0700); err != nil {
		return errors.New("PKI destination must be a new directory")
	}
	serial := func() *big.Int {
		v, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if e != nil {
			panic(e)
		}
		return v
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	ca := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: "Mender private deployment CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(1, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		return err
	}
	write := func(name string, b []byte) error { return os.WriteFile(filepath.Join(dir, name), b, 0600) }
	if err = write("ca.crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err != nil {
		return err
	}
	caKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err = write("ca.key", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: caKey})); err != nil {
		return err
	}
	for _, name := range []string{"database", "web"} {
		leafKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			return e
		}
		leaf := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name, "localhost", "console.localhost", "admin.localhost", "idp.localhost"}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(0, 3, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		cert, e := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
		if e != nil {
			return e
		}
		priv, e := x509.MarshalPKCS8PrivateKey(leafKey)
		if e != nil {
			return e
		}
		if e = write(name+".crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})); e != nil {
			return e
		}
		if e = write(name+".key", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: priv})); e != nil {
			return e
		}
	}
	fmt.Println("Private deployment PKI created; CA key is offline-only and no trust store was changed.")
	return nil
}
