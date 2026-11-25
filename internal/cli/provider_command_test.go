package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
	"github.com/vadim82/panora/internal/source"
)

func pythonFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Fatal("Python 3 is required for the protocol fixture")
	}
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "plugins", "python-local"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPythonPluginThroughService(t *testing.T) {
	root := t.TempDir()
	image := filepath.Join(root, "fixture.png")
	writeScanImage(t, image, 2, 2)
	backend := &fakeWallpaperBackend{}
	resolve := func(context.Context) (desktop.WallpaperBackend, error) { return backend, nil }
	options, socket := serviceFixture(t)
	options.Request.ConfigPath = writeNextConfig(t, root)
	provider, err := source.Load(pythonFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	options.Request.Source = provider
	launchService(t, options, socket, resolve)
	first := cliStatus(t, socket, "next")
	if first.Current.Path == image || filepath.Dir(first.Current.Path) != options.Request.CachePath || first.HistoryLength != 1 {
		t.Fatalf("service did not use Python source: %+v", first)
	}
	writeScanImage(t, image, 1, 1)
	reused := cliStatus(t, socket, "next")
	info, err := playback.ReadImageInfo(reused.Current.Path)
	if err != nil || info.Width != 2 || info.Height != 2 || backend.calls != 2 {
		t.Fatal("valid cached bytes were not preserved independently of changed original")
	}
	if err := os.Remove(reused.Current.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Request(context.Background(), socket, "next"); err == nil || backend.calls != 2 {
		t.Fatal("host trusted plugin over actual image dimensions after cache miss")
	}

}

func TestHangingPluginKeepsStatusResponsiveAndRecovers(t *testing.T) {
	options, socket := serviceFixture(t)
	dir := t.TempDir()
	data, err := os.ReadFile(filepath.Join(pythonFixture(t), "source.py"))
	if err != nil {
		t.Fatal(err)
	}
	// Block only discovery, then recover after the input revision changes.
	script := "import time\n" + string(data)
	script = replaceDiscoveryWithWait(script)
	scriptPath := filepath.Join(dir, "source.py")
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	provider := &source.Client{Manifest: source.Manifest{ID: "local", Protocol: source.Protocol, Command: []string{python, scriptPath}}, Directory: dir, Timeout: 300 * time.Millisecond}
	options.Request.Source = provider
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "fixture.png"), 2, 2)
	options.Request.ConfigPath = writeNextConfig(t, root)
	marker := filepath.Join(root, "block")
	if err := os.WriteFile(marker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	backend := &fakeWallpaperBackend{}
	launchService(t, options, socket, func(context.Context) (desktop.WallpaperBackend, error) { return backend, nil })
	deadline := time.Now().Add(2 * time.Second)
	refreshing := false
	for time.Now().Before(deadline) {
		before := time.Now()
		status, err := control.Request(context.Background(), socket, "")
		if err != nil || time.Since(before) > 200*time.Millisecond {
			t.Fatalf("status blocked: %v", err)
		}
		if status.Refreshing {
			refreshing = true
			if status.Busy {
				t.Fatal("discovery held playback action lock")
			}
			if _, err := control.Request(context.Background(), socket, "pause"); err != nil {
				t.Fatal(err)
			}
			if _, err := control.Request(context.Background(), socket, "next"); err == nil {
				t.Fatal("cold discovery reached desktop")
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !refreshing {
		t.Fatal("discovery never started")
	}
	awaitDiscovery(t, socket)

	if status := cliStatus(t, socket, "status"); status.HistoryLength != 0 || status.Uncertain || backend.calls != 0 {
		t.Fatalf("failed discovery changed desktop state: %+v", status)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	configData, err := os.ReadFile(options.Request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	configData = []byte(strings.Replace(string(configData), "max_depth = 0", "max_depth = 1", 1))
	if err := os.WriteFile(options.Request.ConfigPath, configData, 0600); err != nil {
		t.Fatal(err)
	}
	// The coordinator observes file edits independently of playback requests.
	time.Sleep(1100 * time.Millisecond)
	if recovered := cliStatus(t, socket, "next"); recovered.HistoryLength != 1 || filepath.Dir(recovered.Current.Path) != options.Request.CachePath {
		t.Fatal("source failed to recover")
	}
}

func replaceDiscoveryWithWait(script string) string {
	return strings.Replace(script, "elif operation == \"discover\":", "elif operation == \"discover\":\n    if (Path(params[\"config\"][\"root\"]) / \"block\").exists():\n        time.sleep(60)", 1)
}

// Python constructs requests independently, rather than serializing Go structs.
func TestPythonCallsBundledJSONRPCProvider(t *testing.T) {
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "fixture.png"), 2, 2)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	script := `import json,os,subprocess,sys
binary,root=sys.argv[1:]
config={'root':root,'max_depth':0}
def call(method,params,id='python-1'):
    request={'jsonrpc':'2.0','id':id,'method':method,'params':dict(protocol=2,**params)}
    p=subprocess.run([binary,'provider','local'],input=json.dumps(request)+'\n',text=True,capture_output=True,check=True,timeout=5)
    reply=json.loads(p.stdout)
    assert reply['jsonrpc']=='2.0' and reply['id']==id, reply
    return reply
def failure(method,params,kind):
    error=call(method,params)['error']
    assert error['code']==1000 and error['data']=={'kind':kind}, error
    assert error['message'] and root not in error['message'], error
assert call('describe',{})['result']['protocol']==2
assert call('validate',{'config':config})['result']=={}
discover={'config':config,'cursor':None,'limit':1}
page=call('discover',discover,9007199254740993)['result']
assert page['next_cursor'] is None and len(page['candidates'])==1
candidate=page['candidates'][0]
assert candidate['id']=='fixture.png'
resolve={'config':config,'id':candidate['id'],'ref':candidate['ref']}
resolved=call('resolve',resolve)['result']
assert resolved['content']['path']==root+'/fixture.png'
assert call('missing',{})['error']['code']==-32601
assert call('discover',{'config':config,'cursor':None,'limit':101})['error']['code']==-32602
for invalid in ({'root':'relative','max_depth':0}, {'root':root+'\0','max_depth':0},
                {'root':root,'max_depth':-1}, {'root':root,'max_depth':'bad'},
                {'root':root+'/fixture.png','max_depth':0}):
    failure('validate',{'config':invalid},'invalid_config')

aside=root+'-aside'
os.rename(root,aside)
try:
    failure('validate',{'config':config},'unavailable')
    failure('discover',discover,'unavailable')
    failure('resolve',resolve,'unavailable')
finally:
    os.rename(aside,root)
assert call('validate',{'config':config})['result']=={}
assert call('discover',discover)['result']==page
assert call('resolve',resolve)['result']==resolved

loop=root+'/loop'
os.symlink('loop',loop)
failure('validate',{'config':dict(config,root=loop)},'unavailable')
failure('validate',{'config':dict(config,root=root+'/fixture.png/child')},'unavailable')

parent=root+'/restricted'
child=parent+'/folder'
os.makedirs(child)
os.chmod(parent,0)
try:
    # Privileged users can bypass these mode bits.
    try:
        os.stat(child)
    except PermissionError:
        failure('validate',{'config':dict(config,root=child)},'unavailable')
finally:
    os.chmod(parent,0o700)
`
	if out, err := exec.Command(python, "-c", script, binary, root).CombinedOutput(); err != nil {
		t.Fatalf("independent Python caller: %v\n%s", err, out)
	}
}
