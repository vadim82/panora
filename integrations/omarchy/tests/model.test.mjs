import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
const source=readFileSync(new URL('../io.github.vadim82.panora/Model.js',import.meta.url),'utf8').replace(/^\.pragma library\n/,'');
const model=vm.createContext({});vm.runInContext(source,model);
assert.equal(model.executablePath('/trusted path/panora'), '/trusted path/panora');
for (const path of ['panora', './panora', '/', '/trusted/panora\0ignored', '/trusted/panora\nignored', '/' + 'a'.repeat(4096), 42, undefined]) {
    assert.equal(model.executablePath(path), '');
}
const snapshot={version:1,connected:true,instance_id:'one',paused:true,busy:false,uncertain:false,setup_required:false,can_previous:true,warning:false,image:{id:'1',history_id:'history-one',title:'<img src="https://tracker.invalid">',origin:'local',preview_state:'not_requested'}};
assert.equal(model.parse(JSON.stringify(snapshot)).image.title,snapshot.image.title);
assert.equal(model.allowed(snapshot,'next'),true);assert.equal(model.allowed(snapshot,'pause'),false);assert.equal(model.allowed(snapshot,'resume'),true);
assert.equal(model.allowed({...snapshot,uncertain:true},'resume'),false);assert.equal(model.allowed({...snapshot,uncertain:true},'next'),true);
for(const key of ['busy','setup_required'])assert.equal(model.allowed({...snapshot,[key]:true},'next'),false);
assert.equal(model.allowed({...snapshot,connected:false},'next'),false);
for(const raw of ['{', 'x'.repeat(3*1024*1024+1),JSON.stringify({...snapshot,version:2}),JSON.stringify({...snapshot,image:{...snapshot.image,id:'../private'}}),JSON.stringify({...snapshot,image:{...snapshot.image,preview_state:'available',data_uri:'https://tracker.invalid/a.png'}})])assert.equal(model.parse(raw),null);
assert.equal(model.parse(JSON.stringify({...snapshot,image:{...snapshot.image,preview_state:'available',data_uri:'data:image/png;base64,YQ=='}})).image.data_uri,'data:image/png;base64,YQ==');
assert.notEqual(model.imageKey(snapshot),model.imageKey({...snapshot,instance_id:'two'}));
assert.equal(model.actionSucceeded(JSON.stringify({version:1,instance_id:'one',paused:true,busy:false,uncertain:false}),'one'),true);
assert.equal(model.actionSucceeded('{"version":1,"instance_id":"two"}','one'),false);assert.equal(model.actionSucceeded('{','one'),false);
assert.equal(model.allowed({...snapshot,busy:true,setup_required:true},'stop'),true);
assert.equal(model.allowed({...snapshot,connected:false},'stop'),false);
assert.equal(model.actionSucceeded('{"version":1,"instance_id":"one","stopping":true}','one','stop'),true);
assert.equal(model.actionSucceeded('{"version":1,"instance_id":"other","stopping":true}','one','stop'),false);
assert.equal(model.actionSucceeded('{"version":1,"instance_id":"one","stopping":false}','one','stop'),false);
console.log('Shell model: action gates, unsupported/malformed/oversized payloads, safe text and identity checks passed.');
