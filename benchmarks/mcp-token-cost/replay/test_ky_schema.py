import json, os, subprocess, sys, tempfile, unittest
from pathlib import Path

FAKE = '''import sys, json
for line in sys.stdin:
    m = json.loads(line)
    print(json.dumps({"jsonrpc": "2.0", "id": m["id"], "result": {"content": [{"type": "text", "text": json.dumps(m["params"])}]}}), flush=True)
'''

def call(i, name, args):
    return {'jsonrpc': '2.0', 'id': i, 'method': 'tools/call', 'params': {'name': name, 'arguments': args}}

class KySchemaProxy(unittest.TestCase):
    def test_rejects_are_request_local(self):
        with tempfile.TemporaryDirectory() as d:
            fake, log = Path(d, 'fake.py'), Path(d, 'log.jsonl')
            fake.write_text(FAKE)
            sh = Path(d, 'kv'); sh.write_text('#!/bin/sh\nexec %s %s\n' % (sys.executable, fake)); sh.chmod(0o755)
            reqs = [
                call(1, 'query', {'operation': 'rm', 'arguments': '{}'}),
                call(2, 'query', {'operation': 'find_symbol', 'arguments': '{bad'}),
                call(3, 'query', {'operation': 'find_symbol', 'arguments': 5}),
                call(4, 'query', {'operation': 'find_symbol', 'arguments': '{"name":"x"}'}),
            ]
            out = subprocess.run([sys.executable, str(Path(__file__).with_name('ky_schema.py')), 'generic', str(log)],
                input=''.join(json.dumps(r) + '\n' for r in reqs), capture_output=True, text=True, timeout=30,
                env={**os.environ, 'KV_SERVER': str(sh)})
            replies = {m['id']: m for m in map(json.loads, out.stdout.splitlines())}
            self.assertEqual(sorted(replies), [1, 2, 3, 4], out.stderr)
            for i in (1, 2, 3):
                self.assertTrue(replies[i]['result']['isError'])
            self.assertEqual(json.loads(replies[4]['result']['content'][0]['text']),
                             {'name': 'find_symbol', 'arguments': {'name': 'x'}})
            events = [json.loads(l) for l in log.read_text().splitlines()]
            self.assertEqual(sorted(e['id'] for e in events if 'reject' in e), [1, 2, 3])
            self.assertEqual(sorted(e['id'] for e in events if 'call' in e), [1, 2, 3, 4])
            self.assertEqual([e['id'] for e in events if e.get('reply') == 'find_symbol'], [4])

if __name__ == '__main__':
    unittest.main()
