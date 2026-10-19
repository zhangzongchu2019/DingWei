package migrate

import (
	"context"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

const hostedCatalogSQL = `SELECT jsonb_build_object(
 'columns',(SELECT jsonb_agg(jsonb_build_array(c.relname,a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull,pg_get_expr(d.adbin,d.adrelid)) ORDER BY c.relname,a.attnum) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_attribute a ON a.attrelid=c.oid LEFT JOIN pg_attrdef d ON d.adrelid=c.oid AND d.adnum=a.attnum WHERE n.nspname=current_schema() AND c.relname LIKE 'g4\_%' AND c.relkind='r' AND a.attnum>0 AND NOT a.attisdropped),
 'constraints',(SELECT jsonb_agg(jsonb_build_array(c.relname,k.conname,pg_get_constraintdef(k.oid)) ORDER BY c.relname,k.conname) FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname LIKE 'g4\_%'),
 'indexes',(SELECT jsonb_agg(jsonb_build_array(tablename,indexname,replace(indexdef,current_schema()||'.','')) ORDER BY tablename,indexname) FROM pg_indexes WHERE schemaname=current_schema() AND tablename LIKE 'g4\_%'))`

func TestR31HostedBaselineCatalogue(t *testing.T) {
	ctx := context.Background()
	p := testSchema(t, pgtest.DSN(t))
	baseline, err := embeddedFS.ReadFile("migrations/0200_hosted_r30_baseline.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Exec(ctx, testCtx(t), string(baseline)); err != nil {
		t.Fatal(err)
	}
	row, err := p.QueryRow(ctx, testCtx(t), hostedCatalogSQL)
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err = row.Scan(&raw); err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("testdata/r30-hosted-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err = json.Unmarshal(expected, &want); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	w, _ := json.Marshal(want)
	g, _ := json.Marshal(got)
	if string(w) != string(g) {
		t.Fatal("frozen R30 catalog drift")
	}
}

func TestR31HostedEmptyAndR30Adoption(t *testing.T) {
	dsn := pgtest.DSN(t)
	ctx := context.Background()
	for _, old := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "r30-direct-with-data"}[old], func(t *testing.T) {
			p := testSchema(t, dsn)
			var oid uint32
			if old {
				body, err := embeddedFS.ReadFile("migrations/0200_hosted_r30_baseline.sql")
				if err != nil {
					t.Fatal(err)
				}
				raw := strings.SplitN(string(body), "-- R30_FROZEN_DDL_START\n", 2)
				if len(raw) != 2 {
					t.Fatal("missing frozen baseline marker")
				}
				if _, err = p.Exec(ctx, testCtx(t), strings.SplitN(raw[1], "-- R30_FROZEN_DDL_END", 2)[0]); err != nil {
					t.Fatal(err)
				}
				if _, err = p.Exec(ctx, testCtx(t), `INSERT INTO g4_node(id,name)VALUES('nd_old','old');
   INSERT INTO g4_node_credential(node_id,secret_hash,issued_by)VALUES('nd_old','fixed-fixture-hash','fixture');
   INSERT INTO g4_account_pool(id,name,kind,cli,llm,secret_enc,key_version,owner_lc,created_by)VALUES('acct_old','old','token_file','codex','gpt',decode('0102','hex'),1,'','owner');
 INSERT INTO g4_account_grant(account_id,user_lc,granted_by)VALUES('acct_old','creator','fixture');
   INSERT INTO g4_hosted_seat(agent_id,node_id,cli,llm,cred_kind,account_ref,plan,created_by)VALUES('ag_old','nd_old','codex','gpt','token_file','acct_old','1c1.5g40','owner');`); err != nil {
					t.Fatal(err)
				}
				row, err := p.QueryRow(ctx, testCtx(t), `SELECT 'g4_node'::regclass::oid`)
				if err != nil {
					t.Fatal(err)
				}
				if err = row.Scan(&oid); err != nil {
					t.Fatal(err)
				}
			}
			first, err := Migrate(ctx, p, HubDomains())
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"0103_agent_last_source_r31.sql", "0200_hosted_r30_baseline.sql", "0201_hosted_node_access_r31.sql"} {
				found := false
				for _, applied := range first.Applied {
					if applied == name {
						found = true
					}
				}
				if !found {
					t.Fatalf("not applied: %s", name)
				}
			}
			if old {
				var gotOID uint32
				var scope, hash, plan, account, secret, owner string
				row, err := p.QueryRow(ctx, testCtx(t), `SELECT n.tableoid::oid,n.access_scope,c.secret_hash,s.plan,s.account_ref,encode(a.secret_enc,'hex'),a.owner_lc FROM g4_node n JOIN g4_node_credential c ON c.node_id=n.id JOIN g4_hosted_seat s ON s.node_id=n.id JOIN g4_account_pool a ON a.id=s.account_ref WHERE n.id='nd_old'`)
				if err != nil {
					t.Fatal(err)
				}
				if err = row.Scan(&gotOID, &scope, &hash, &plan, &account, &secret, &owner); err != nil {
					t.Fatal(err)
				}
				if gotOID != oid || scope != "admin_only" || hash != "fixed-fixture-hash" || plan != "1c1.5g40" || account != "acct_old" || secret != "0102" || owner != "" {
					t.Fatalf("adoption replaced table or changed existing data: oid=%v/%v scope=%s", oid, gotOID, scope)
				}
			}
			again, err := Migrate(ctx, p, HubDomains())
			if err != nil {
				t.Fatal(err)
			}
			if len(again.Applied) != 0 {
				t.Fatalf("replayed: %v", again.Applied)
			}
			var entries int
			row, err := p.QueryRow(ctx, testCtx(t), `SELECT count(*) FROM schema_migrations WHERE version IN ('0200_hosted_r30_baseline.sql','0201_hosted_node_access_r31.sql')`)
			if err != nil {
				t.Fatal(err)
			}
			if err = row.Scan(&entries); err != nil {
				t.Fatal(err)
			}
			if entries != 2 {
				t.Fatalf("ledger rows: %d", entries)
			}
		})
	}
}

func TestR31HostedPartialShapeRefusesAdoption(t *testing.T) {
	ctx := context.Background()
	p := testSchema(t, pgtest.DSN(t))
	if _, err := p.Exec(ctx, testCtx(t), `CREATE TABLE g4_node(id text PRIMARY KEY,name text)`); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(ctx, p, HubDomains()); err == nil || !strings.Contains(err.Error(), "baseline shape mismatch") {
		t.Fatalf("partial schema accepted: %v", err)
	}
	var count int
	row, err := p.QueryRow(ctx, testCtx(t), `SELECT count(*) FROM schema_migrations WHERE version='0200_hosted_r30_baseline.sql'`)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed adoption recorded as success")
	}
}

// Evaluate declarative Go string constants without importing hosted back into
// its migration dependency. This keeps the source-vs-ledger shape comparison
// independent of the frozen SQL files and avoids an import cycle in this test.
func hostedSourceDDL(t *testing.T) string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "../../hosted/schema.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	defs := map[string]ast.Expr{}
	for _, decl := range f.Decls {
		if g, ok := decl.(*ast.GenDecl); ok && g.Tok == token.CONST {
			for _, spec := range g.Specs {
				v := spec.(*ast.ValueSpec)
				for i, n := range v.Names {
					defs[n.Name] = v.Values[i]
				}
			}
		}
	}
	var eval func(ast.Expr) string
	eval = func(e ast.Expr) string {
		switch x := e.(type) {
		case *ast.BasicLit:
			s, err := strconv.Unquote(x.Value)
			if err != nil {
				t.Fatal(err)
			}
			return s
		case *ast.Ident:
			return eval(defs[x.Name])
		case *ast.BinaryExpr:
			if x.Op == token.ADD {
				return eval(x.X) + eval(x.Y)
			}
		}
		t.Fatalf("unsupported schema constant %T", e)
		return ""
	}
	return eval(defs["SchemaDDL"])
}

func r31HostedTableNames() []string {
	return []string{"g4_quota_notice", "g4_node", "g4_node_credential", "g4_account_pool", "g4_account_grant", "g4_hosted_seat", "g4_seat_event", "g4_lifecycle_job", "g4_quota", "g4_team_key", "g4_audit", "g4_backup", "g4_account_login", "g4_node_access_grant"}
}
