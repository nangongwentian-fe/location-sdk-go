import hashlib, json, os, pathlib, platform, subprocess, sys, tarfile
version, target = sys.argv[1:]
root = pathlib.Path('artifact').resolve()
root.mkdir(exist_ok=True)
subprocess.run(['gh','release','download','engine-v'+version,'--repo','nangongwentian-fe/location-sdk-go','--pattern','location-engine-'+version+'-'+target+'.tar.gz','--pattern','compatibility.json','--dir',str(root)],check=True)
compatibility=json.loads((root/'compatibility.json').read_text())
entry=compatibility['artifacts'][target]
archive=root/entry['file']
assert hashlib.sha256(archive.read_bytes()).hexdigest()==entry['archiveSHA256'], 'archive digest mismatch'
with tarfile.open(archive) as tar: tar.extractall(root,filter='data')
binary=root/('location-engine.exe' if target.startswith('windows-') else 'location-engine')
assert hashlib.sha256(binary.read_bytes()).hexdigest()==entry['sha256'], 'binary digest mismatch'
assert compatibility['version']==version and compatibility['protocolVersion']==1
binary.chmod(0o755)
subprocess.run(['go','run','./examples/smoke',str(binary)],check=True)
print('Runtime accepted on',platform.system(),platform.machine(),target)
