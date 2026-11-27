import json, subprocess, sys, os, threading
from pathlib import Path
arm, log = sys.argv[1:]; allowed = {'find_references','find_symbol','get_source'}
p = subprocess.Popen([os.environ.get('KV_SERVER','/tmp/kv-main'),'serve'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,text=True,env={**os.environ,'HOME':'/tmp/kivgraph-corpora/ky/home','KIVGRAPH_TELEMETRY':'0'})
requests = {}
lock = threading.Lock()
def event(obj):
 with lock, open(log,'a') as f: print(json.dumps(obj),file=f)
def emit(msg):
 with lock: print(json.dumps(msg),flush=True)
def input_loop():
 for line in sys.stdin:
  msg=json.loads(line);method=msg.get('method');key=msg.get('id');params=msg.get('params',{});name=None
  if method=='tools/call':
   name=params['name'];args=params.get('arguments',{});event({'call':name,'args':args,'id':key,'pid':p.pid})
   def reject(text):
    event({'reject':text,'id':key})
    if key is not None:emit({'jsonrpc':'2.0','id':key,'result':{'isError':True,'content':[{'type':'text','text':text}]}})
   if arm=='generic' and name=='query':
    try:
     name=args['operation'];args=json.loads(args['arguments'])
     if not isinstance(args,dict):raise ValueError('arguments must be a JSON object')
    except (KeyError,ValueError,TypeError) as e: reject(str(e));continue
   if name not in allowed: reject('tool not allowed: %s'%name);continue
   msg['params']={**params,'name':name,'arguments':args}
  if key is not None:requests[key]=(method,name)
  p.stdin.write(json.dumps(msg)+'\n');p.stdin.flush()
 p.stdin.close()
threading.Thread(target=input_loop,daemon=True).start()
for line in p.stdout:
 msg=json.loads(line);rid=msg.get('id');method,name=requests.pop(rid,(None,None));r=msg.get('result',{})
 if method=='initialize':event({'initialize':p.pid,'id':rid})
 if method=='tools/list':
  tools=[t for t in r['tools'] if t['name'] in allowed];event({'tools':[t['name'] for t in tools],'id':rid});r['tools']=tools if arm=='native' else [{'name':'query','description':'Invoke find_symbol, find_references or get_source. Pass the operation and its JSON arguments. Search by name, narrow by repository, path and qualified_name; get_source accepts a symbols array of selectors.','inputSchema':{'type':'object','required':['operation','arguments'],'properties':{'operation':{'type':'string'},'arguments':{'type':'string'}}}}]
 if method=='tools/call':
  txt=r.get('content',[{}])[0].get('text','');event({'reply':name,'id':rid,'error':msg.get('error') or (txt if r.get('isError') else None),'preview':txt[:90]})
 emit(msg)
p.wait()
