# Historical OMP Python-eval adapter definitions. Loading does not start a run.
import json, os, pathlib as pl, re, socket, subprocess as sp, sys, tempfile

pick = {'cobra': 'ExactArgs', 'ky': 'mergeHeaders', 'ripgrep': 'Replacer'}
commonN = 'Answer the following maintainer question with evidence from the frozen {c} repository. Choose your searches and what to read; you may search again or batch needed reads. Cite file and line(s). Keep the answer to the question asked. Use only {tools} as evidence; no other tools, files, web, tests or edits. Starting symbol: {n}.\n'
promB = {'cobra': 'When validating a command with ExactArgs, what happens if the argument count is wrong? Show one existing test that exercises this failure, including the concrete counts or expected error.', 'ky': 'How does Ky decide whether the user supplied Content-Type, and what header inputs does it consult for that decision? Cite the implementation, including the exact predicate.', 'ripgrep': 'When Standard builds its searcher sink with and without a path, how is its replacement helper initialized? Compare both construction sites and describe the initial internal state of the helper.'}
qsP = {('cobra', 'control'): ('ExactArgs', 'When validating a command with ExactArgs, what happens if the argument count is wrong? Show one existing test that exercises this failure, including the concrete counts or expected error.'), ('ky', 'control'): ('mergeHeaders', 'How does Ky decide whether the user supplied Content-Type, and what header inputs does it consult for that decision? Cite the implementation, including the exact predicate.'), ('ripgrep', 'control'): ('Replacer', 'When Standard builds its searcher sink with and without a path, how is its replacement helper initialized? Compare both construction sites and describe the initial internal state of the helper.'), ('cobra', 'typed'): ('MarkFlagRequired', 'Which non-test methods directly call the package-level MarkFlagRequired function (not the identically named Command method), and which flag set does each pass? Identify one production caller of the Command method instead and cite the evidence.'), ('ky', 'typed'): ('KyOptions.retry', 'Trace the retry property of KyOptions, not the retry helper or retry hook. Which init-hook options clone reads it, how is the property copied (including nested arrays), and which constant lists this option as a recognized key? Cite the code.'), ('ripgrep', 'typed'): ('line_buffer::Config', 'For the private line_buffer::Config, not the searcher or printer Config, which two structs store this exact type in a field, and which constructor explicitly calls its Default implementation? Give one default field value and cite the source.')}
runDirP = pl.Path(tempfile.mkdtemp(prefix="kv-pair-"))
procsP, labelsP, hP, workP = {}, {}, {}, {}
brokerSrc = "import json,os,socketserver,subprocess,sys\npath,corpus=sys.argv[1:];p=subprocess.Popen(['/tmp/kv-main','serve'],env={**os.environ,'HOME':f'/tmp/kivgraph-corpora/{corpus}/home','KIVGRAPH_TELEMETRY':'0'},stdin=subprocess.PIPE,stdout=subprocess.PIPE,text=True);seq=0\ndef rpc(method,params,notify=False):\n global seq\n seq+=1;req={'jsonrpc':'2.0','method':method,'params':params};req.update({} if notify else {'id':seq});p.stdin.write(json.dumps(req)+'\\n');p.stdin.flush()\n if notify:return\n while True:\n  line=p.stdout.readline()\n  if not line:raise EOFError('MCP closed before reply')\n  msg=json.loads(line)\n  if msg.get('id')==seq:return msg\nclass Handler(socketserver.StreamRequestHandler):\n def handle(self):\n  op,args=json.loads(self.rfile.readline());msg=rpc('tools/call',{'name':op,'arguments':args});res=msg.get('result',{});v=res['content'][0]['text'] if 'content' in res else str(msg);print('call',p.pid,seq,op,flush=True);self.wfile.write(json.dumps(v).encode()+b'\\n')\ntry:\n rpc('initialize',{'protocolVersion':'2025-06-18','capabilities':{},'clientInfo':{'name':'persistent-pair','version':'1'}});rpc('notifications/initialized',{},True)\n with socketserver.UnixStreamServer(path,Handler) as s:\n  print('ready',p.pid,flush=True);s.serve_forever()\nfinally:p.terminate();p.wait()\n"

def localFileN(c,path):
 root=(pl.Path('/tmp/kivgraph-corpora')/c/'src').resolve();p=(root/path).resolve();assert c in pick and p.is_relative_to(root);return p

@tool
def text_search(corpus:str,pattern:str,path:str='.',context:int=0)->str:
 """Search a corpus with rg -n; pattern is a regex; optional relative path and number of surrounding lines."""
 p=localFileN(corpus,path);v=sp.run(['/opt/homebrew/bin/rg','--no-heading','--with-filename','--color','never','-n','-C',str(context),'--',pattern,path],cwd=localFileN(corpus,'.'),capture_output=True,text=True);return v.stdout or v.stderr or 'No matches.'

@tool
async def text_read(corpus:str,ranges:list[str])->str:
 """Read one or more file ranges with the host reader. Each item is relative/path:start-end."""
 out=[]
 for r in ranges:
  p,span=r.rsplit(':',1);assert re.fullmatch(r'\d+-\d+',span);v=await tool.read(path=f'{localFileN(corpus,p)}:{span}');out.append(v['text'] if isinstance(v,dict) else str(v))
 return '\n'.join(out)

def isolatedRPC(c,t,a):
 p=sp.Popen(['/tmp/kv-main','serve'],env={**os.environ,'HOME':f'/tmp/kivgraph-corpora/{c}/home','KIVGRAPH_TELEMETRY':'0'},stdin=sp.PIPE,stdout=sp.PIPE,stderr=sp.DEVNULL,text=True)
 try:
  def request(m,params,i):
   req={'jsonrpc':'2.0','method':m,'params':params};req.update({'id':i} if i else {});p.stdin.write(json.dumps(req)+'\n');p.stdin.flush();return json.loads(p.stdout.readline()) if i else None
  request('initialize',{'protocolVersion':'2025-06-18','capabilities':{},'clientInfo':{'name':'paired','version':'1'}},1);request('notifications/initialized',{},0);v=request('tools/call',{'name':t,'arguments':a},2);return v['result']['content'][0]['text'] if 'result' in v else str(v['error'])
 finally:p.terminate();p.wait()

@tool
def graph_query(corpus:str,operation:str,arguments:str)->str:
 """Invoke find_symbol or find_references with {\"name\":\"...\"}, or get_source with {\"symbols\":[{\"repository\":...,\"path\":...,\"qualified_name\":...},...]}. corpus must be cobra, ky or ripgrep. arguments is JSON."""
 if corpus not in pick or operation not in ('find_symbol','find_references','get_source'):return 'invalid corpus or operation'
 return isolatedRPC(corpus,operation,json.loads(arguments))

def brokerP(label,c):
 path=str(runDirP/label);p=sp.Popen([sys.executable,'-u','-c',brokerSrc,path,c],stdout=sp.PIPE,stderr=sp.DEVNULL,text=True);init=p.stdout.readline().strip();assert init.startswith('ready '),init;procsP[label]=p;labelsP[label]=int(init.split()[1]);return path

@tool
def persistent_graph(session:str,operation:str,arguments:str)->str:
 """Use your assigned session ID. Operations: find_symbol or find_references with {\"name\":\"...\"}, or get_source with {\"symbols\":[{\"repository\":...,\"path\":...,\"qualified_name\":...},...]}. Narrow a selector by repository, path and qualified_name. Arguments is JSON."""
 if operation not in ('find_symbol','find_references','get_source') or not re.fullmatch('[A-Za-z0-9]+',session):return 'invalid operation or session'
 path=str(runDirP/session)
 if not os.path.exists(path):return 'use assigned session ID'
 with socket.socket(socket.AF_UNIX,socket.SOCK_STREAM) as s:
  s.settimeout(30);s.connect(path);s.sendall((json.dumps([operation,json.loads(arguments)])+'\n').encode());return json.loads(s.makefile('rb').readline())

def runCleanN(i):
 keys=[(c,a) for c in pick for a in ('graph','grep')];hs={}
 for c,a in (keys if i%2 else keys[::-1]):
  ts=['graph_query'] if a=='graph' else ['text_search','text_read'];hs[c,a]=agent('The corpus argument is exactly '+c+'. '+commonN.format(c=c,tools=', '.join(ts),n=pick[c])+promB[c],agent='scout',tools=ts,label=f'Pair{i}{c}{a}')
 return hs

def launchP(rep,kind):
 keys=[(c,a) for c in pick for a in ('graph','grep')];keys=keys[::-1] if rep==2 else keys
 for c,a in keys:
  n,q=qsP[c,kind];label=f'Stay{rep}{kind}{c}{a}';ts=['persistent_graph'] if a=='graph' else ['text_search','text_read'];info='The corpus argument is exactly '+c+'. '
  if a=='graph':brokerP(label,c);info=f'Your graph session ID is {label}, repository {c}. '
  prompt=info+commonN.format(c=c,tools=', '.join(ts),n=n)+q;workP[label]=prompt;hP[rep,kind,c,a]=agent(prompt,agent='scout',tools=ts,label=label)
