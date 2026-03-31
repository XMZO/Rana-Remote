import http.server, json, socketserver
from urllib.parse import urlparse
INDEX = b"""<!doctype html><html><body><h1>Rana Remote</h1><form id='login-form'><input name='username'/><input name='password' type='password'/><button type='submit'>Login</button></form><script>document.getElementById('login-form').addEventListener('submit', async (e)=>{e.preventDefault(); const fd=new FormData(e.target); const resp=await fetch('/api/v1/auth/login',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({username:fd.get('username'),password:fd.get('password')})}); const data=await resp.json(); localStorage.setItem('rana.access', data.data.access_token); document.body.insertAdjacentHTML('beforeend','<div id=dashboard>dashboard</div>'); const audit=await fetch('/api/v1/audit-logs?download=1&format=json&access_token='+data.data.access_token); if(audit.ok){document.body.insertAdjacentHTML('beforeend','<div id=audit-export>audit-export-ok</div>')} });</script></body></html>"""
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        p=urlparse(self.path)
        if p.path=='/':
            self.send_response(200); self.send_header('Content-Type','text/html'); self.end_headers(); self.wfile.write(INDEX); return
        if p.path=='/api/v1/audit-logs':
            self.send_response(200); self.send_header('Content-Type','application/json'); self.send_header('X-Trace-Id','trace-e2e'); self.end_headers(); self.wfile.write(json.dumps([{'trace_id':'trace-e2e'}]).encode()); return
        self.send_response(404); self.end_headers()
    def do_POST(self):
        if self.path=='/api/v1/auth/login':
            l=int(self.headers.get('Content-Length','0')); self.rfile.read(l)
            self.send_response(200); self.send_header('Content-Type','application/json'); self.send_header('X-Trace-Id','trace-login'); self.end_headers(); self.wfile.write(json.dumps({'code':'ok','data':{'access_token':'token','refresh_token':'refresh'}}).encode()); return
        self.send_response(404); self.end_headers()
    def log_message(self, *args):
        pass
with socketserver.TCPServer(('127.0.0.1',18080), H) as s:
    s.serve_forever()
