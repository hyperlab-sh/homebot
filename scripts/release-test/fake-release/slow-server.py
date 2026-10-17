"""A loopback server that accepts a request but does not answer promptly."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from time import sleep


class Slow(BaseHTTPRequestHandler):
    def do_GET(self):
        sleep(30)
        self.send_response(200)
        self.end_headers()


ThreadingHTTPServer(("127.0.0.1", 8027), Slow).serve_forever()
