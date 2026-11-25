"""Offline Unix-socket process fixture for opt-in launchd integration checks."""
import argparse
import fcntl
import http.server
import json
import os
from pathlib import Path
import socket
import threading
import uuid

parser = argparse.ArgumentParser()
parser.add_argument('command', choices=['serve'])
parser.add_argument('--socket', required=True)
parser.add_argument('--pause-for-setup', action='store_true')
parser.add_argument('--log-file', required=True)
args = parser.parse_args()
path = Path(args.socket)
path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
lock = (path.parent / (path.name + '.lock')).open('a')
fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
if path.exists():
    path.unlink()
instance = str(uuid.uuid4())


class Server(http.server.HTTPServer):
    address_family = socket.AF_UNIX


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.reply({'version': 1, 'instance_id': instance, 'setup_required': True,
                    'paused': True, 'requested_backend': 'macos'})

    def do_POST(self):
        if self.path != '/v1/shutdown':
            self.send_error(404)
            return
        self.reply({'version': 1, 'instance_id': instance, 'stopping': True})
        threading.Thread(target=self.server.shutdown).start()

    def reply(self, body):
        data = json.dumps(body).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)
        self.wfile.flush()

    def log_message(self, *_):
        pass


with Server(str(path), Handler) as server:
    os.chmod(path, 0o600)
    Path(args.log_file).parent.mkdir(parents=True, exist_ok=True)
    descriptor = os.open(args.log_file, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
    with os.fdopen(descriptor, 'w') as log:
        log.write('Offline launchd fixture ready\n')
    server.serve_forever(poll_interval=0.05)
path.unlink(missing_ok=True)
lock.close()
