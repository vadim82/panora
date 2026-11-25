package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pythonClient(t *testing.T, script string) *Client {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3 is required for the cross-language contract fixture")
	}
	path := filepath.Join(t.TempDir(), "plugin.py")
	if err := os.WriteFile(path, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	return &Client{Manifest: Manifest{ID: "local", Protocol: Protocol, Command: []string{python, path}}, Timeout: 2 * time.Second}
}

func TestClientRejectsBrokenProtocolAndBoundsOutput(t *testing.T) {
	for _, test := range []struct{ name, response string }{
		{"malformed", "print('not JSON')"},
		{"extra line", "print(json.dumps(ok)); print('{}')"},
		{"wrong version", "ok['jsonrpc']='1.0'; print(json.dumps(ok))"},
		{"legacy reply", "ok={'protocol':1,'id':r['id'],'result':{}}; print(json.dumps(ok))"},
		{"missing error code", "ok.pop('result'); ok['error']={'message':'failed'}; print(json.dumps(ok))"},
		{"null error code", "ok.pop('result'); ok['error']={'code':None,'message':'failed'}; print(json.dumps(ok))"},
		{"wrong ID", "ok['id']='other'; print(json.dumps(ok))"},
		{"both results", "ok['error']={'code':1000,'message':'failed','data':{'kind':'unavailable'}}; print(json.dumps(ok))"},
		{"null result", "ok['result']=None; print(json.dumps(ok))"},
		{"unknown error", "ok.pop('result'); ok['error']={'code':1000,'message':'failed','data':{'kind':'surprise'}}; print(json.dumps(ok))"},
		{"no newline", "sys.stdout.write(json.dumps(ok))"},
		{"crash after response", "print(json.dumps(ok)); sys.exit(7)"},
		{"oversized", "sys.stdout.write('x'*(2*1024*1024)); sys.stdout.flush()"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := pythonClient(t, "import json,sys\nr=json.load(sys.stdin)\nok={'jsonrpc':'2.0','id':r['id'],'result':{'protocol':2,'id':'local','media_kinds':['image']}}\n"+test.response+"\n")
			if _, err := client.Describe(context.Background()); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

func TestClientTypedErrorAndPrivateEnvironment(t *testing.T) {
	t.Setenv("PANORA_TEST_SECRET", "do-not-inherit-or-log")
	client := pythonClient(t, `import json,os,sys
r=json.load(sys.stdin)
assert 'PANORA_TEST_SECRET' not in os.environ
print(json.dumps({'jsonrpc':'2.0','id':r['id'],'error':{'code':1000,'message':'folder is unavailable','data':{'kind':'unavailable'}}}))
`)
	_, err := client.Describe(context.Background())
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "unavailable" {
		t.Fatalf("got %v", err)
	}
	client = pythonClient(t, "import sys\nsys.stderr.write('do-not-inherit-or-log'*10000)\nsys.exit(9)\n")
	_, err = client.Describe(context.Background())
	if err == nil || strings.Contains(err.Error(), "do-not-inherit-or-log") {
		t.Fatalf("diagnostic leaked: %v", err)
	}
}

func TestClientDeadlineCancellationAndManifest(t *testing.T) {
	client := pythonClient(t, "import time\ntime.sleep(60)\n")
	client.Timeout = 100 * time.Millisecond
	started := time.Now()
	_, err := client.Describe(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 2*time.Second {
		t.Fatalf("timeout failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Describe(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	dir := t.TempDir()
	manifest := Manifest{ID: "local", Name: "Fixture", Version: "0.1.0", Protocol: Protocol, Command: []string{"./fixture", "literal $(argument)"}}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil || loaded.Manifest.Command[0] != filepath.Join(dir, "fixture") || loaded.Manifest.Command[1] != "literal $(argument)" {
		t.Fatalf("manifest: %+v %v", loaded, err)
	}
	if _, err := Load("relative"); err == nil {
		t.Fatal("accepted relative registration")
	}
}

func TestClientRejectsIncompatibleManifestAndDescription(t *testing.T) {
	dir := t.TempDir()
	original := []byte(`{"id":"local","name":"Old provider","version":"1","protocol":1,"command":["./never-started"]}`)
	path := filepath.Join(dir, "plugin.json")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "unsupported plugin protocol 1; expected 2") {
		t.Fatalf("old manifest: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(original) {
		t.Fatal("manifest changed during rejection")
	}
	for _, change := range []string{"d['protocol']=1", "d['requires']=['video']", "d['id']='other'", "d['operations']=['describe']", "d['media_kinds']=['scene']"} {
		client := pythonClient(t, `import json,sys
r=json.load(sys.stdin)
d={'protocol':2,'id':'local','media_kinds':['image'],'operations':['describe','validate','discover','resolve']}
`+change+`
print(json.dumps({'jsonrpc':'2.0','id':r['id'],'result':d}))
`)
		if _, err := client.Describe(context.Background()); err == nil {
			t.Fatalf("incompatible description accepted: %s", change)
		}
	}
}

func TestClientRPCFailuresAndRetryValidation(t *testing.T) {
	for _, code := range []int{-32700, -32600, -32601, -32602, -32603, -32001} {
		script := fmt.Sprintf("import json,sys\nr=json.load(sys.stdin)\nprint(json.dumps({'jsonrpc':'2.0','id':r['id'],'error':{'code':%d,'message':'private details','data':{'secret':'private token'}}}))\n", code)
		client := pythonClient(t, script)
		_, err := client.Describe(context.Background())
		var rpc *RPCError
		if !errors.As(err, &rpc) || rpc.Code != code || strings.Contains(err.Error(), "private") {
			t.Fatalf("RPC error lost or leaked: %v", err)
		}
	}
	for _, data := range []string{`{'kind':'rate_limited','retry_after_seconds':-1}`, `{'kind':'unavailable','retry_after_seconds':86401}`, `{'kind':'not_found','retry_after_seconds':1}`} {
		client := pythonClient(t, "import json,sys\nr=json.load(sys.stdin)\nprint(json.dumps({'jsonrpc':'2.0','id':r['id'],'error':{'code':1000,'message':'failed','data':"+data+"}}))\n")
		_, err := client.Describe(context.Background())
		var typed *Error
		if err == nil || errors.As(err, &typed) {
			t.Fatalf("invalid retry hint accepted: %v", err)
		}
	}
}
